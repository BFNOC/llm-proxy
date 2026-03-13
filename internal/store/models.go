package store

import "time"

type UpstreamProvider struct {
	ID        int64
	Name      string
	BaseURL   string
	APIKey    string // encrypted at rest
	Priority  int
	Healthy   bool // runtime only, not persisted
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
	ProviderStyle   string
	Path            string
	StatusCode      int
	LatencyMs       int64
	CreatedAt       time.Time
}
