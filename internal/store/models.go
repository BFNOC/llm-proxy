package store

import "time"

type UpstreamProvider struct {
	ID        int64
	Name      string
	BaseURL   string
	APIKey    string // encrypted at rest
	Priority  int
	Enabled   bool   // persisted; disabled upstreams are skipped by the prober
	Healthy   bool   // runtime only, not persisted
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DownstreamKey struct {
	ID        int64
	KeyHash   string
	KeyPrefix string
	Name      string
	RPMLimit  int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RequestLog struct {
	ID              int64
	DownstreamKeyID int64
	UpstreamName    string
	ClientIP        string
	ProviderStyle   string
	Path            string
	StatusCode      int
	LatencyMs       int64
	CreatedAt       time.Time
}

// ModelWhitelistEntry is a glob pattern for filtering /v1/models responses.
// If the whitelist is non-empty, only models matching at least one pattern are
// returned. Patterns support * wildcards (e.g. "claude-sonnet*"); patterns
// without wildcards match as substrings.
type ModelWhitelistEntry struct {
	ID        int64
	Pattern   string
	CreatedAt time.Time
}
