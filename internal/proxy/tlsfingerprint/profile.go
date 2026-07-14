package tlsfingerprint

// NodeClaudeCodeProfile 返回一个与 sub2api 所用 Claude Code
//（Node.js 24.x）默认值匹配的 TLS ClientHello 配置。空切片会回退到
// buildClientHelloSpecFromProfile 中的内置默认值。
func NodeClaudeCodeProfile() *Profile {
	return &Profile{
		Name: "claude-code-nodejs-24",
		// CipherSuites/Curves 等留空 → 使用 dialer.go 的默认值（Node 24.x）
		// 仅使用 ALPN http/1.1 —— 匹配 Claude Code Node 客户端指纹。
		ALPNProtocols: []string{"http/1.1"},
		EnableGREASE:  false,
	}
}
