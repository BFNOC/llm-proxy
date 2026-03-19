package geoip

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// New — graceful degradation
// ---------------------------------------------------------------------------

func TestNew_FileNotFound(t *testing.T) {
	g := New("/nonexistent/path/GeoLite2-City.mmdb")
	assert.Nil(t, g, "should return nil when mmdb file does not exist")
}

func TestNew_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.mmdb")
	require.NoError(t, os.WriteFile(path, []byte("not-a-valid-mmdb"), 0o644))

	g := New(path)
	assert.Nil(t, g, "should return nil when mmdb file is corrupted")
}

func TestNew_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "noperm.mmdb")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o000))
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	g := New(path)
	assert.Nil(t, g, "should return nil when mmdb file is not readable")
}

func TestNew_ValidMMDB(t *testing.T) {
	mmdbPath := findMMDB(t)
	if mmdbPath == "" {
		t.Skip("GeoLite2-City.mmdb not found, skipping live test")
	}

	g := New(mmdbPath)
	require.NotNil(t, g, "should load a valid mmdb file")
	defer g.Close()
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

func TestLookup_NilReceiver(t *testing.T) {
	var g *GeoIP
	assert.Equal(t, "", g.Lookup("1.2.3.4"), "nil receiver should return empty string")
}

func TestLookup_EmptyIP(t *testing.T) {
	mmdbPath := findMMDB(t)
	if mmdbPath == "" {
		t.Skip("mmdb not found")
	}
	g := New(mmdbPath)
	require.NotNil(t, g)
	defer g.Close()

	assert.Equal(t, "", g.Lookup(""), "empty IP should return empty string")
}

func TestLookup_InvalidIP(t *testing.T) {
	mmdbPath := findMMDB(t)
	if mmdbPath == "" {
		t.Skip("mmdb not found")
	}
	g := New(mmdbPath)
	require.NotNil(t, g)
	defer g.Close()

	assert.Equal(t, "", g.Lookup("not-an-ip"), "invalid IP should return empty string")
}

func TestLookup_KnownIPs(t *testing.T) {
	mmdbPath := findMMDB(t)
	if mmdbPath == "" {
		t.Skip("mmdb not found")
	}
	g := New(mmdbPath)
	require.NotNil(t, g)
	defer g.Close()

	// Google DNS — should be US
	regionUS := g.Lookup("8.8.8.8")
	assert.NotEmpty(t, regionUS)
	t.Logf("8.8.8.8 → %s", regionUS)

	// AWS Tokyo — should be Japan (was incorrectly Singapore in ip2region)
	regionJP := g.Lookup("3.112.240.98")
	assert.Contains(t, regionJP, "日本", "3.112.240.98 (AWS Tokyo) should be Japan")
	t.Logf("3.112.240.98 → %s", regionJP)

	// Chinese DNS
	regionCN := g.Lookup("114.114.114.114")
	assert.Contains(t, regionCN, "中国", "114.114.114.114 should be China")
	t.Logf("114.114.114.114 → %s", regionCN)

	// Loopback — should return empty (no record)
	regionLo := g.Lookup("127.0.0.1")
	t.Logf("127.0.0.1 → %q", regionLo)
}

func TestLookup_ConcurrentSafety(t *testing.T) {
	mmdbPath := findMMDB(t)
	if mmdbPath == "" {
		t.Skip("mmdb not found")
	}
	g := New(mmdbPath)
	require.NotNil(t, g)
	defer g.Close()

	ips := []string{"1.2.3.4", "8.8.8.8", "114.114.114.114", "3.112.240.98", "127.0.0.1"}
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(ip string) {
			defer func() { done <- struct{}{} }()
			_ = g.Lookup(ip)
		}(ips[i%len(ips)])
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose_NilReceiver(t *testing.T) {
	var g *GeoIP
	g.Close() // should not panic
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func findMMDB(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../data/GeoLite2-City.mmdb",
		"../../tmp/ip-test/data/GeoLite2-City.mmdb",
		"data/GeoLite2-City.mmdb",
		os.Getenv("GEOIP_DB_PATH"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}
