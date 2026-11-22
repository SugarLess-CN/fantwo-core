package service

import "testing"

// kvGetter 用 map 桩模拟 LoadAuditConfig 里 DB getter 的语义：
// key 查不到、或值等于空串时回落 def（与真实现 `err != nil || c.Value == ""` 一致）。
//
// 之所以必须模拟「空串回落 def」：DB 里存在 content_audit_sensitivity 行但 value=”
// 的情况（后台清空配置项）很常见，若回落失效会把空灵敏度读进来。
func kvGetter(kv map[string]string) func(key, def string) string {
	return func(key, def string) string {
		if v, ok := kv[key]; ok && v != "" {
			return v
		}
		return def
	}
}

// TestAuditConfigSensitivity 灵敏度读取（对齐原 content-audit.service.ts:127-165）。
//
// 背景：原 NestJS 读 16 个 key，其中 content_audit_sensitivity 默认 'medium'；
// 本仓库此前 AuditConfig 无此字段，属配置读取缺口。本测试锁三点：
//  1. key 缺失 / 空串 → 回落 "medium"；
//  2. 显式值原样读入；
//  3. 加字段不破坏其它字段（provider / autoBlock / reviewMode 同批断言）。
//
// 用纯函数 parseAuditConfig 测，不依赖 DB（LoadAuditConfig 走 s.DB.First，无库不可测）。
func TestAuditConfigSensitivity(t *testing.T) {
	type want struct {
		sensitivity string
		provider    string
		autoBlock   bool
		reviewMode  string
	}
	cases := []struct {
		name string
		kv   map[string]string
		want want
	}{
		{
			name: "无 content_audit_sensitivity → 默认 medium",
			kv:   map[string]string{},
			want: want{sensitivity: "medium", provider: AuditProviderNone, autoBlock: true, reviewMode: "auto"},
		},
		{
			name: "显式 high → 原样读入",
			kv:   map[string]string{"content_audit_sensitivity": "high"},
			want: want{sensitivity: "high", provider: AuditProviderNone, autoBlock: true, reviewMode: "auto"},
		},
		{
			name: "空串值 → 回落 medium",
			kv:   map[string]string{"content_audit_sensitivity": ""},
			want: want{sensitivity: "medium", provider: AuditProviderNone, autoBlock: true, reviewMode: "auto"},
		},
		{
			name: "low + 其它字段同批读入（provider=tencent / auto_block=false / review_mode=manual）",
			kv: map[string]string{
				"content_audit_sensitivity": "low",
				"content_audit_provider":    AuditProviderTencent,
				"content_audit_auto_block":  "false",
				"content_audit_review_mode": "manual",
			},
			want: want{sensitivity: "low", provider: AuditProviderTencent, autoBlock: false, reviewMode: "manual"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseAuditConfig(kvGetter(tc.kv))
			if cfg.Sensitivity != tc.want.sensitivity {
				t.Fatalf("Sensitivity = %q，期望 %q", cfg.Sensitivity, tc.want.sensitivity)
			}
			if cfg.Provider != tc.want.provider {
				t.Errorf("Provider = %q，期望 %q", cfg.Provider, tc.want.provider)
			}
			if cfg.AutoBlock != tc.want.autoBlock {
				t.Errorf("AutoBlock = %v，期望 %v", cfg.AutoBlock, tc.want.autoBlock)
			}
			if cfg.ReviewMode != tc.want.reviewMode {
				t.Errorf("ReviewMode = %q，期望 %q", cfg.ReviewMode, tc.want.reviewMode)
			}
		})
	}
}
