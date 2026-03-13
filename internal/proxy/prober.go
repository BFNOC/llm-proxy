package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/Instawork/llm-proxy/internal/store"
)

// UpstreamProber periodically probes all configured upstreams and updates the
// DynamicProxy to point at the highest-priority healthy one.
type UpstreamProber struct {
	store     *store.Store
	proxy     *DynamicProxy
	interval  time.Duration
	timeout   time.Duration
	currentID int64
	mu        sync.Mutex
	client    *http.Client
}

// NewUpstreamProber creates a prober that checks upstreams on the given
// interval and uses timeout for each individual probe request.
func NewUpstreamProber(s *store.Store, p *DynamicProxy, interval, timeout time.Duration) *UpstreamProber {
	return &UpstreamProber{
		store:    s,
		proxy:    p,
		interval: interval,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
}

// Start runs the probe loop until ctx is cancelled. It performs an initial
// probe immediately before entering the tick loop, so the proxy is usable as
// soon as Start returns (assuming it is called in a goroutine).
func (p *UpstreamProber) Start(ctx context.Context) {
	p.probeOnce()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeOnce()
		}
	}
}

// probeOnce evaluates all upstreams and potentially switches the active one.
// It holds the mutex for the entire evaluation to prevent concurrent switches.
func (p *UpstreamProber) probeOnce() {
	p.mu.Lock()
	defer p.mu.Unlock()

	upstreams, err := p.store.ListUpstreams()
	if err != nil {
		slog.Error("prober: failed to list upstreams", "error", err)
		return
	}

	if len(upstreams) == 0 {
		slog.Warn("prober: no upstreams configured")
		return
	}

	// Sort by priority ascending (lower value = higher preference).
	sort.Slice(upstreams, func(i, j int) bool {
		return upstreams[i].Priority < upstreams[j].Priority
	})

	// If the current active upstream is still healthy, leave it in place to
	// avoid unnecessary failovers.
	if p.currentID > 0 {
		for _, u := range upstreams {
			if u.ID == p.currentID {
				if p.probeUpstream(u.BaseURL) {
					return // current is healthy, no action needed
				}
				slog.Warn("prober: current upstream unhealthy, switching",
					"id", u.ID, "name", u.Name)
				break
			}
		}
	}

	// Walk the sorted list and activate the first reachable upstream.
	for _, u := range upstreams {
		if !p.probeUpstream(u.BaseURL) {
			continue
		}
		parsed, err := url.Parse(u.BaseURL)
		if err != nil {
			slog.Error("prober: invalid upstream URL", "url", u.BaseURL, "error", err)
			continue
		}
		p.proxy.SetActiveUpstream(parsed, u.APIKey)
		p.currentID = u.ID
		slog.Info("prober: activated upstream",
			"id", u.ID, "name", u.Name, "url", u.BaseURL)
		return
	}

	// No upstream is reachable. Keep the last active one rather than clearing
	// it, so transient network blips don't result in a 503 storm.
	slog.Error("prober: all upstreams unhealthy, keeping last active")
}

// probeUpstream issues a HEAD request to baseURL/v1/models and returns true
// when the server is reachable (any HTTP status below 500 counts, including
// 401 which still means the server is up).
func (p *UpstreamProber) probeUpstream(baseURL string) bool {
	resp, err := p.client.Head(baseURL + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// GetCurrentID returns the ID of the upstream that is currently active.
// Returns 0 if no upstream has been selected yet.
func (p *UpstreamProber) GetCurrentID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentID
}
