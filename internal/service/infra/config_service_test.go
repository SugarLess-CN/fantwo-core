// 配置服务单测：敏感项打码、「空值不覆盖」规则、启动期配置写入拦截。
//
// 说明：本仓库当前没有 sqlmock / sqlite 等内存库依赖（go.mod 由 lead 独占管理，
// 成员禁止引入），因此 ConfigService 的 DB 方法无法在此直接跑。这里采取项目允许的
// 退化方案：把打码、跳过判定、启动期拦截、更新列构造、公开配置聚合全部抽成纯函数，
// 对纯函数做表驱动测试——DB 调用层只负责「查 → 调纯函数 → 写」，规则覆盖等价。
package infra

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// TestConfigIsSensitiveKey 覆盖白名单三项 + 命名兜底 + 非敏感项。
//
// 判定规则来自 model.IsSensitiveConfigKey（唯一事实来源），这里锁住 service 侧的包装行为。
func TestConfigIsSensitiveKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want bool
	}{
		{"白名单-s3_access_key", model.ConfigKeyS3AccessKey, true},
		{"白名单-s3_secret_key", model.ConfigKeyS3SecretKey, true},
		{"白名单-search_password", model.ConfigKeySearchPassword, true},
		{"兜底-foo_password", "foo_password", true},
		{"兜底-my_secret", "my_secret", true},
		{"兜底-大写 SECRET", "S3_SECRET_KEY", true},
		{"兜底-自定义 access_key", "custom_access_key", true},
		{"兜底-_token 后缀", "wx_pay_token", true},
		{"兜底-private_key", "rsa_private_key", true},
		{"兜底-前后空白", "  search_password  ", true},
		{"非敏感-site_name", "site_name", false},
		{"非敏感-s3_endpoint", "s3_endpoint", false},
		{"非敏感-search_username", "search_username", false},
		{"非敏感-空 key", "", false},
		{"非敏感-tokenizer 前缀不误伤", "tokenizer_name", false},
		// 敏感词表不是密钥：不在 model 白名单里，也不命中命名兜底；
		// 它只在 PublicConfigs 侧被单独剔除（见 TestConfigAggregatePublic）。
		{"非密钥-sensitive_words", model.ConfigKeySensitiveWords, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSensitiveKey(tc.key); got != tc.want {
				t.Fatalf("isSensitiveKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
			// 包装语义：service 侧结论必须与 model 的单一事实来源完全一致
			if got := model.IsSensitiveConfigKey(tc.key); got != tc.want {
				t.Fatalf("model.IsSensitiveConfigKey(%q) = %v, want %v（与 service 结论不一致）", tc.key, got, tc.want)
			}
		})
	}
}

// TestConfigSkipValueUpdate 覆盖 Update/UpdateAll 共用的跳过规则。
func TestConfigSkipValueUpdate(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  bool
	}{
		{"敏感-空串跳过", model.ConfigKeyS3SecretKey, "", true},
		{"敏感-占位符跳过", model.ConfigKeyS3SecretKey, maskedValue, true},
		{"敏感-占位符带空白跳过", model.ConfigKeySearchPassword, "  *** ", true},
		{"敏感-真实值不跳过", model.ConfigKeySearchPassword, "real-pass", false},
		// 敏感词表不是密钥项：空串/占位符规则不作用于它，写入语义与旧实现一致
		{"非密钥-敏感词空串不跳过", model.ConfigKeySensitiveWords, "", false},
		{"非密钥-敏感词空数组不跳过", model.ConfigKeySensitiveWords, "[]", false},
		{"非敏感-空串不跳过（保持原语义）", "site_name", "", false},
		{"非敏感-占位符照常写入", "site_name", maskedValue, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipValueUpdate(tc.key, tc.value); got != tc.want {
				t.Fatalf("skipValueUpdate(%q, %q) = %v, want %v", tc.key, tc.value, got, tc.want)
			}
		})
	}
}

// TestConfigToView 覆盖视图转换：敏感项打码/未配置、非敏感项原样、时间戳透传。
func TestConfigToView(t *testing.T) {
	created := time.Date(2026, 11, 22, 10, 30, 0, 0, time.UTC)
	updated := created.Add(2 * time.Hour)
	base := func(key, value string) model.Config {
		return model.Config{
			BaseModel:   model.BaseModel{ID: "cfg-1", CreatedAt: created, UpdatedAt: updated},
			Key:         key,
			Value:       value,
			Description: "desc",
			Type:        model.ConfigTypeString,
			Group:       model.ConfigGroupStorage,
		}
	}

	t.Run("敏感项有值-打码且 hasValue", func(t *testing.T) {
		v := toView(base(model.ConfigKeyS3AccessKey, "AKID-real"))
		if v.Value != maskedValue {
			t.Fatalf("Value = %q, want %q", v.Value, maskedValue)
		}
		if !v.Masked || !v.HasValue {
			t.Fatalf("Masked/HasValue = %v/%v, want true/true", v.Masked, v.HasValue)
		}
		if v.Key != model.ConfigKeyS3AccessKey || v.Description != "desc" ||
			v.Type != model.ConfigTypeString || v.Group != model.ConfigGroupStorage {
			t.Fatalf("非 value 字段被改动: %+v", v)
		}
	})

	t.Run("时间戳与 ID 透传", func(t *testing.T) {
		v := toView(base(model.ConfigKeyS3SecretKey, "x"))
		if v.ID != "cfg-1" {
			t.Fatalf("ID = %q, want cfg-1", v.ID)
		}
		if !v.CreatedAt.Equal(created) {
			t.Fatalf("CreatedAt = %v, want %v", v.CreatedAt, created)
		}
		if !v.UpdatedAt.Equal(updated) {
			t.Fatalf("UpdatedAt = %v, want %v", v.UpdatedAt, updated)
		}
		if v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
			t.Fatalf("时间戳为零值: %+v", v)
		}
	})

	// 启动期标记（T-34c3）：列表里给老库残留的 s3_* / storage_driver 行打标，前端可置灰只读
	t.Run("启动期配置-标记但非敏感", func(t *testing.T) {
		v := toView(base(model.ConfigKeyS3Bucket, "my-bucket"))
		if !v.StartupOnly {
			t.Fatal("StartupOnly = false, want true（s3_bucket 属启动期配置）")
		}
		if v.Masked {
			t.Fatal("Masked = true, want false（bucket 名不是密钥）")
		}
		// 非敏感项：value 原样，不打码
		if v.Value != "my-bucket" {
			t.Fatalf("Value = %q, want my-bucket", v.Value)
		}
	})

	t.Run("运行期可调配置-不标记", func(t *testing.T) {
		v := toView(base(model.ConfigKeyStorageLocalDir, "./uploads"))
		if v.StartupOnly {
			t.Fatal("StartupOnly = true, want false（storage_local_dir 运行期可调）")
		}
		if v.Masked {
			t.Fatal("Masked = true, want false")
		}
	})

	// 两个判定同时命中，语义必须能共存：写路径拒写（启动期）、读路径打码（敏感）
	t.Run("启动期与敏感同时命中-互不排斥", func(t *testing.T) {
		v := toView(base(model.ConfigKeyS3AccessKey, "AKID-real"))
		if !v.StartupOnly {
			t.Fatal("StartupOnly = false, want true（s3_access_key 属启动期配置）")
		}
		if !v.Masked {
			t.Fatal("Masked = false, want true（s3_access_key 属敏感项）")
		}
		if v.Value != maskedValue || !v.HasValue {
			t.Fatalf("Value/HasValue = %q/%v, want ***/true", v.Value, v.HasValue)
		}
	})

	t.Run("敏感项无值-空串且不打码占位", func(t *testing.T) {
		v := toView(base(model.ConfigKeyS3SecretKey, ""))
		if v.Value != "" {
			t.Fatalf("Value = %q, want \"\"", v.Value)
		}
		if v.HasValue {
			t.Fatal("HasValue = true, want false")
		}
		if !v.Masked {
			t.Fatal("Masked = false, want true（该条仍是敏感项）")
		}
	})

	t.Run("非敏感项原样", func(t *testing.T) {
		v := toView(base("site_name", "PicArt"))
		if v.Value != "PicArt" || v.Masked {
			t.Fatalf("Value/Masked = %q/%v, want PicArt/false", v.Value, v.Masked)
		}
		if !v.HasValue {
			t.Fatal("HasValue = false, want true")
		}
	})

	t.Run("非敏感项空值", func(t *testing.T) {
		v := toView(base("site_logo", ""))
		if v.Value != "" || v.HasValue || v.Masked {
			t.Fatalf("Value/HasValue/Masked = %q/%v/%v, want \"\"/false/false", v.Value, v.HasValue, v.Masked)
		}
	})
}

// TestConfigToViews 覆盖批量转换与 nil 安全。
func TestConfigToViews(t *testing.T) {
	if got := toViews(nil); got == nil || len(got) != 0 {
		t.Fatalf("toViews(nil) = %#v, want 空切片", got)
	}
	rows := []model.Config{
		{Key: "site_name", Value: "PicArt", Type: model.ConfigTypeString, Group: model.ConfigGroupSite},
		{Key: model.ConfigKeyS3AccessKey, Value: "AK", Type: model.ConfigTypeString, Group: model.ConfigGroupStorage},
		{Key: model.ConfigKeySearchPassword, Value: "pw", Type: model.ConfigTypeString, Group: model.ConfigGroupSearch},
	}
	views := toViews(rows)
	if len(views) != 3 {
		t.Fatalf("len = %d, want 3", len(views))
	}
	if views[0].Value != "PicArt" || views[0].Masked {
		t.Fatalf("非敏感项被打码: %+v", views[0])
	}
	for _, v := range views[1:] {
		if v.Value != maskedValue || !v.Masked || !v.HasValue {
			t.Fatalf("敏感项未正确打码: %+v", v)
		}
	}
}

// TestConfigViewJSONFields 对外 JSON 契约回归测试。
//
// 目的：把「ConfigView 必须与改前 model.Config 的响应字段集合一致」钉死——
// 改前 List/ByID/ByKey 返回 GORM 实体，其 JSON 含 id / createdAt / updatedAt
// （BaseModel 的 deletedAt 是 json:"-"，本来就不出现）；打码只允许**新增**
// hasValue / masked，不允许删除、改名任何既有字段。谁再删字段，此用例直接红。
func TestConfigViewJSONFields(t *testing.T) {
	created := time.Date(2026, 11, 22, 10, 30, 0, 0, time.UTC)
	updated := created.Add(2 * time.Hour)
	src := model.Config{
		BaseModel:   model.BaseModel{ID: "cfg-1", CreatedAt: created, UpdatedAt: updated},
		Key:         model.ConfigKeyS3SecretKey,
		Value:       "s3cr3t",
		Description: "S3 Secret Key",
		Type:        model.ConfigTypeString,
		Group:       model.ConfigGroupStorage,
	}

	raw, err := json.Marshal(toView(src))
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	// ① 字段名齐全（12 个）。只断言「命名存在」，不断言字段总数：
	//    以后再加字段不该让这条契约测试变脆，但删字段/改名必须红。
	for _, k := range []string{
		"id", "key", "value", "description", "type", "group", "public",
		"created_at", "updated_at", // ← 与改前 GORM 实体响应一致的既有字段，不许删
		"has_value", "masked", "startup_only", // ← 打码 + 启动期标记（新增字段）
	} {
		if _, ok := m[k]; !ok {
			t.Fatalf("JSON 缺少字段 %q（契约回归）: %s", k, raw)
		}
	}
	// ② 改前就不出现的字段，现在也不该出现
	if _, leaked := m["deleted_at"]; leaked {
		t.Fatalf("JSON 出现 deletedAt（改前为 json:\"-\"，不该下发）: %s", raw)
	}

	// ③ 打码语义
	if m["value"] != maskedValue {
		t.Fatalf("JSON value = %v, want %q", m["value"], maskedValue)
	}
	if m["has_value"] != true || m["masked"] != true {
		t.Fatalf("JSON hasValue/masked = %v/%v, want true/true", m["has_value"], m["masked"])
	}
	// ③b 启动期标记：s3_secret_key 同时命中「启动期」与「敏感」，两者共存于同一行
	if m["startup_only"] != true {
		t.Fatalf("JSON startupOnly = %v, want true（s3_secret_key 属启动期配置）", m["startup_only"])
	}

	// ④ 时间戳真实下发（前端按 updatedAt 展示「最后修改时间」）
	for _, k := range []string{"created_at", "updated_at"} {
		s, ok := m[k].(string)
		if !ok || s == "" {
			t.Fatalf("JSON %s = %#v, want 非空 RFC3339 字符串", k, m[k])
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			t.Fatalf("JSON %s = %q 不是 RFC3339 时间: %v", k, s, err)
		}
	}
	if got := m["created_at"].(string); got[:10] != "2026-11-22" {
		t.Fatalf("JSON createdAt = %q, want 2026-11-22 开头", got)
	}
}

// TestConfigBuildUpdates 覆盖 PATCH 更新列构造（含旧 bug：只改 type 时写过 value=""）。
func TestConfigBuildUpdates(t *testing.T) {
	str := func(p bool) *bool { return &p }

	cases := []struct {
		name        string
		cur         model.Config
		in          ConfigInput
		wantValue   bool
		wantValueV  string
		wantType    string
		wantPublic  interface{}
		wantHasPubl bool
	}{
		{
			name: "敏感项传***-不写value",
			cur:  model.Config{Key: model.ConfigKeyS3AccessKey, Value: "AK"},
			in:   ConfigInput{Value: maskedValue},
		},
		{
			name: "敏感项传空串-不写value",
			cur:  model.Config{Key: model.ConfigKeySearchPassword, Value: "pw"},
			in:   ConfigInput{Value: ""},
		},
		{
			name:       "敏感项传真实值-写value",
			cur:        model.Config{Key: model.ConfigKeySearchPassword, Value: "old"},
			in:         ConfigInput{Value: "new"},
			wantValue:  true,
			wantValueV: "new",
		},
		{
			name:     "只改type不写value（旧实现会清空value）",
			cur:      model.Config{Key: "site_name", Value: "PicArt"},
			in:       ConfigInput{Type: model.ConfigTypeNumber},
			wantType: model.ConfigTypeNumber,
		},
		{
			name:       "非敏感项传***-照原语义写入",
			cur:        model.Config{Key: "site_logo", Value: "a.png"},
			in:         ConfigInput{Value: maskedValue},
			wantValue:  true,
			wantValueV: maskedValue,
		},
		{
			name:        "public 指针非 nil-写public",
			cur:         model.Config{Key: "site_name"},
			in:          ConfigInput{Public: str(true)},
			wantPublic:  true,
			wantHasPubl: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := buildUpdates(tc.cur, tc.in)
			got, ok := u["value"]
			if ok != tc.wantValue {
				t.Fatalf("value 是否写入 = %v, want %v（updates=%#v）", ok, tc.wantValue, u)
			}
			if ok && got != tc.wantValueV {
				t.Fatalf("value = %v, want %q", got, tc.wantValueV)
			}
			if tc.wantType != "" && u["type"] != tc.wantType {
				t.Fatalf("type = %v, want %q", u["type"], tc.wantType)
			}
			pub, hasPub := u["public"]
			if hasPub != tc.wantHasPubl {
				t.Fatalf("public 是否写入 = %v, want %v", hasPub, tc.wantHasPubl)
			}
			if hasPub && pub != tc.wantPublic {
				t.Fatalf("public = %v, want %v", pub, tc.wantPublic)
			}
		})
	}
}

// TestConfigAggregatePublic 覆盖 /config/public 的兜底剔除与类型还原。
func TestConfigAggregatePublic(t *testing.T) {
	rows := []model.Config{
		{Key: "site_name", Value: "PicArt", Type: model.ConfigTypeString, Group: model.ConfigGroupSite, Public: true},
		{Key: "upload_max_image_size", Value: "20", Type: model.ConfigTypeNumber, Group: model.ConfigGroupUpload, Public: true},
		// 误标 public 的敏感项：必须剔除
		{Key: model.ConfigKeyS3SecretKey, Value: "s3cr3t", Type: model.ConfigTypeString, Group: model.ConfigGroupStorage, Public: true},
		{Key: model.ConfigKeySearchPassword, Value: "pw", Type: model.ConfigTypeString, Group: model.ConfigGroupSearch, Public: true},
		{Key: model.ConfigKeySensitiveWords, Value: `["违禁词"]`, Type: model.ConfigTypeJSON, Group: model.ConfigGroupSystem, Public: true},
	}
	out := aggregatePublic(rows)

	site := out[model.ConfigGroupSite]
	if site["site_name"] != "PicArt" {
		t.Fatalf("site_name = %v, want PicArt", site["site_name"])
	}
	upload := out[model.ConfigGroupUpload]
	if v, ok := upload["upload_max_image_size"].(float64); !ok || v != 20 {
		t.Fatalf("upload_max_image_size = %#v, want float64(20)", upload["upload_max_image_size"])
	}
	for _, group := range out {
		for _, sensitive := range []string{
			model.ConfigKeyS3SecretKey,
			model.ConfigKeyS3AccessKey,
			model.ConfigKeySearchPassword,
			model.ConfigKeySensitiveWords,
		} {
			if _, leaked := group[sensitive]; leaked {
				t.Fatalf("公开配置泄露敏感 key %q（group 内容：%#v）", sensitive, group)
			}
		}
	}
}

// TestIsStartupOnlyRejected 启动期配置写入拦截的表驱动测试（T-34c2）。
//
// 被测对象是 service 层唯一的拦截函数 rejectStartupOnlyKey —— Create / Update /
// UpdateAll / UpdateOne 四个入口共用它（不在各入口各写一份 if，避免以后漏入口）。
// DB 调用本身无法在此覆盖（无 sqlmock/sqlite 依赖，见文件头说明）。
func TestIsStartupOnlyRejected(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		wantErr bool
		why     string
	}{
		// ① 8 个启动期 key：一律拒绝，且文案里必须点名 key
		{"storage_driver", "storage_driver", true, "minio client 类型，启动期决定"},
		{"s3_endpoint", model.ConfigKeyS3Endpoint, true, ""},
		{"s3_bucket", model.ConfigKeyS3Bucket, true, ""},
		{"s3_region", model.ConfigKeyS3Region, true, ""},
		{"s3_access_key", model.ConfigKeyS3AccessKey, true, "同时是敏感项"},
		{"s3_secret_key", model.ConfigKeyS3SecretKey, true, "同时是敏感项"},
		{"s3_use_ssl", model.ConfigKeyS3UseSSL, true, ""},
		{"s3_force_path_style", model.ConfigKeyS3ForcePathStyle, true, ""},

		// ② 运行期可调的配置：放行（误判会让运维明明该改的配置被拒）
		{"storage_local_dir", model.ConfigKeyStorageLocalDir, false, ""},
		{"cdn_base_url", model.ConfigKeyCDNBaseURL, false, ""},
		{"upload_max_image_size", model.ConfigKeyUploadMaxImageSize, false, ""},
		{"search_addresses", model.ConfigKeySearchAddresses, false, ""},
		{"upload_compression_enabled", model.ConfigKeyUploadCompression, false, ""},
		{"sensitive_words", model.ConfigKeySensitiveWords, false, "词表是运行期可调"},

		// ③ 空 key 放行：交给 Create 自己的「配置 key 不能为空」去报，不在这里抢
		{"空 key", "", false, ""},
		{"纯空白 key", "   ", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectStartupOnlyKey(tc.key)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("rejectStartupOnlyKey(%q) = %v, want nil（%s）", tc.key, err, tc.why)
				}
			} else {
				if err == nil {
					t.Fatalf("rejectStartupOnlyKey(%q) = nil, want 拒绝（%s）", tc.key, tc.why)
				}
				msg := err.Error()
				if !strings.Contains(msg, tc.key) {
					t.Fatalf("错误文案未点名 key %q（运维不知道改哪一项）: %s", tc.key, msg)
				}
				// 契约级断言：锁定 i18n key（文案润色不假红），并验证可执行要素仍在
				if ec, ok := err.(*errcode.Err); !ok || ec.Key != i18n.ConfigStartupOnlyFix {
					t.Fatalf("错误应绑定 i18n.ConfigStartupOnlyFix，实际 %v", err)
				}
				for _, want := range []string{"startup-only", "environment variable", "restart"} {
					if !strings.Contains(msg, want) {
						t.Fatalf("错误文案缺少可执行要素 %q: %s", want, msg)
					}
				}
			}
			// 与 model 的唯一事实来源保持一致（service 侧不做任何硬编码）
			if got, want := rejectStartupOnlyKey(tc.key) != nil, model.IsStartupOnlyConfigKey(tc.key); got != want {
				t.Fatalf("rejectStartupOnlyKey(%q) 与 model.IsStartupOnlyConfigKey 结论不一致: %v vs %v", tc.key, got, want)
			}
		})
	}
}
