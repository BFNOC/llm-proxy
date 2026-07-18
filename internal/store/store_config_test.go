package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SetSetting / GetSetting
// ---------------------------------------------------------------------------

func TestSetting_SetAndGet(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("my_key", "my_value")
	require.NoError(t, err)

	val, err := s.GetSetting("my_key", "default")
	require.NoError(t, err)
	assert.Equal(t, "my_value", val)
}

func TestSetting_GetMissing_ReturnsDefault(t *testing.T) {
	s := newTestStore(t)

	val, err := s.GetSetting("nonexistent", "fallback")
	require.NoError(t, err)
	assert.Equal(t, "fallback", val)
}

func TestSetting_Upsert(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("threshold", "5")
	require.NoError(t, err)

	err = s.SetSetting("threshold", "10")
	require.NoError(t, err)

	val, err := s.GetSetting("threshold", "0")
	require.NoError(t, err)
	assert.Equal(t, "10", val)
}

func TestSetting_EmptyValue(t *testing.T) {
	s := newTestStore(t)

	err := s.SetSetting("empty", "")
	require.NoError(t, err)

	val, err := s.GetSetting("empty", "default")
	require.NoError(t, err)
	assert.Equal(t, "", val, "empty string should be stored, not treated as missing")
}

func TestSetting_MultipleKeys(t *testing.T) {
	s := newTestStore(t)

	require.NoError(t, s.SetSetting("a", "1"))
	require.NoError(t, s.SetSetting("b", "2"))
	require.NoError(t, s.SetSetting("c", "3"))

	v1, _ := s.GetSetting("a", "")
	v2, _ := s.GetSetting("b", "")
	v3, _ := s.GetSetting("c", "")
	assert.Equal(t, "1", v1)
	assert.Equal(t, "2", v2)
	assert.Equal(t, "3", v3)
}

// ---------------------------------------------------------------------------
// Config Export / Import（配置导出/导入）
// ---------------------------------------------------------------------------

func TestExportConfig(t *testing.T) {
	s := newTestStore(t)

	// 创建上游（含 API Key）
	up, err := s.CreateUpstream("export-up", "https://e.example.com", []string{"secret-key-123"}, 5, "", "", "", "备注", false, false, 0)
	require.NoError(t, err)
	require.NoError(t, s.SetUpstreamModelPatterns(up.ID, []string{"gpt-*"}))

	// 创建下游 Key
	_, _, err = s.CreateKey("export-key", 60)
	require.NoError(t, err)

	// 创建白名单
	_, err = s.AddModelWhitelist("claude-*")
	require.NoError(t, err)

	// 创建设置
	require.NoError(t, s.SetSetting("auto_disable_threshold", "5"))

	// 导出
	cfg, err := s.ExportConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "1", cfg.Version)
	assert.False(t, cfg.ExportedAt.IsZero())

	// 验证上游导出（不含 API Key）
	require.Len(t, cfg.Upstreams, 1)
	assert.Equal(t, "export-up", cfg.Upstreams[0].Name)
	assert.Equal(t, "https://e.example.com", cfg.Upstreams[0].BaseURL)
	assert.Equal(t, 5, cfg.Upstreams[0].Priority)
	assert.Equal(t, "备注", cfg.Upstreams[0].Remark)
	assert.Equal(t, []string{"gpt-*"}, cfg.Upstreams[0].ModelPatterns)

	// 验证下游 Key 导出（不含哈希和明文）
	require.Len(t, cfg.Keys, 1)
	assert.Equal(t, "export-key", cfg.Keys[0].Name)
	assert.Equal(t, 60, cfg.Keys[0].RPMLimit)

	// 验证白名单
	require.Len(t, cfg.Whitelist, 1)
	assert.Equal(t, "claude-*", cfg.Whitelist[0])

	// 验证设置
	assert.Equal(t, "5", cfg.Settings["auto_disable_threshold"])
}

func TestImportConfig(t *testing.T) {
	s := newTestStore(t)

	cfg := &ConfigExport{
		Version:    "1",
		ExportedAt: time.Now().UTC(),
		Upstreams: []UpstreamExport{
			{
				Name:              "imported-up",
				BaseURL:           "https://imported.example.com",
				Priority:          3,
				Enabled:           true,
				KeySchedulingMode: "round-robin",
				AuthMode:          "api_key",
				Remark:            "导入测试",
				ModelPatterns:     []string{"gpt-*", "claude-*"},
			},
		},
		Keys: []KeyExport{
			{Name: "imported-key", RPMLimit: 30, Enabled: true, MaxConcurrent: 5},
		},
		Whitelist: []string{"o1-*"},
		Settings: map[string]string{
			"auto_disable_threshold": "10",
		},
	}

	err := s.ImportConfig(cfg)
	require.NoError(t, err)

	// 验证上游已创建
	upstreams, err := s.ListUpstreams()
	require.NoError(t, err)
	require.Len(t, upstreams, 1)
	assert.Equal(t, "imported-up", upstreams[0].Name)
	assert.Equal(t, "https://imported.example.com", upstreams[0].BaseURL)
	assert.Equal(t, 3, upstreams[0].Priority)

	// 验证模型模式已导入
	patterns, err := s.GetUpstreamModelPatterns(upstreams[0].ID)
	require.NoError(t, err)
	assert.Len(t, patterns, 2)
	assert.Contains(t, patterns, "gpt-*")
	assert.Contains(t, patterns, "claude-*")

	// 验证下游 Key 已创建
	keys, err := s.ListKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "imported-key", keys[0].Name)
	assert.Equal(t, 30, keys[0].RPMLimit)
	assert.Equal(t, 5, keys[0].MaxConcurrent)

	// 验证白名单已创建
	wl, err := s.ListModelWhitelist()
	require.NoError(t, err)
	require.Len(t, wl, 1)
	assert.Equal(t, "o1-*", wl[0].Pattern)

	// 验证设置已导入
	val, err := s.GetSetting("auto_disable_threshold", "0")
	require.NoError(t, err)
	assert.Equal(t, "10", val)
}

func TestImportConfig_SkipDuplicates(t *testing.T) {
	s := newTestStore(t)

	cfg := &ConfigExport{
		Version:    "1",
		ExportedAt: time.Now().UTC(),
		Upstreams: []UpstreamExport{
			{Name: "dup-up", BaseURL: "https://dup.example.com", Priority: 1, Enabled: true},
		},
		Keys: []KeyExport{
			{Name: "dup-key", RPMLimit: 10, Enabled: true},
		},
		Whitelist: []string{"dup-pattern-*"},
	}

	// 第一次导入
	err := s.ImportConfig(cfg)
	require.NoError(t, err)

	// 第二次导入同样的配置，不应报错也不应创建重复记录
	err = s.ImportConfig(cfg)
	require.NoError(t, err)

	upstreams, err := s.ListUpstreams()
	require.NoError(t, err)
	assert.Len(t, upstreams, 1, "不应创建重复上游")

	keys, err := s.ListKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 1, "不应创建重复 Key")

	wl, err := s.ListModelWhitelist()
	require.NoError(t, err)
	assert.Len(t, wl, 1, "不应创建重复白名单条目")
}

func TestImportConfig_SettingsWhitelist(t *testing.T) {
	s := newTestStore(t)

	cfg := &ConfigExport{
		Version:    "1",
		ExportedAt: time.Now().UTC(),
		Settings: map[string]string{
			"auto_disable_threshold": "5",    // 已知 key，应被导入
			"unknown_evil_setting":   "hack", // 未知 key，应被跳过
		},
	}

	err := s.ImportConfig(cfg)
	require.NoError(t, err)

	// 已知 key 应已导入
	val, err := s.GetSetting("auto_disable_threshold", "0")
	require.NoError(t, err)
	assert.Equal(t, "5", val)

	// 未知 key 应使用默认值（未导入）
	val, err = s.GetSetting("unknown_evil_setting", "default")
	require.NoError(t, err)
	assert.Equal(t, "default", val, "未知设置项应被跳过")
}
