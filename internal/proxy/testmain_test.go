package proxy

import (
	"os"
	"testing"
)

// TestMain disables DNS rebinding SSRF protection for the proxy test suite.
// Test servers created by httptest.NewServer listen on 127.0.0.1, which would
// be rejected by safeDialContext. Individual tests for safeDialContext enable
// protection explicitly where needed.
func TestMain(m *testing.M) {
	SSRFProtection = false
	os.Exit(m.Run())
}
