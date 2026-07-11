package tlsfingerprint

// NodeClaudeCodeProfile returns a TLS ClientHello profile matching Claude Code
// (Node.js 24.x) defaults used by sub2api. Empty slices fall back to built-in
// defaults in buildClientHelloSpecFromProfile.
func NodeClaudeCodeProfile() *Profile {
	return &Profile{
		Name: "claude-code-nodejs-24",
		// CipherSuites/Curves/etc. empty → dialer.go defaults (Node 24.x)
		// ALPN http/1.1 only — matches Claude Code Node client fingerprint.
		ALPNProtocols: []string{"http/1.1"},
		EnableGREASE:  false,
	}
}
