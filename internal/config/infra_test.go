// Package config 基础设施域配置测试（LoadInfra）。
//
// 防的 bug（T-29，真机联调发现）：
//   1) search.addresses 是切片，viper 的 AutomaticEnv 对 []string 的 Unmarshal
//      完全不解析环境变量 —— 设了 SEARCH_ADDRESSES 也不生效；
//   2) storage.driver 拼错（如 "S3 " 带空格、"aws"）会让上传静默失败，
//      因此必须归一化并回退 local；
//   3) storage= s3 但 bucket/endpoint 缺一个时，上传必然 500，必须在 Load 期降级。
package config

import (
	"os"
	"testing"
)

// clearInfraEnv 清掉所有会影响 LoadInfra 的环境变量，保证基线可控。
func clearInfraEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"STORAGE_DRIVER", "STORAGE_LOCAL_DIR",
		"S3_ENDPOINT", "S3_BUCKET", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY",
		"S3_USE_SSL", "S3_FORCE_PATH_STYLE", "CDN_BASE_URL",
		"UPLOAD_MAX_IMAGE_MB", "UPLOAD_MAX_VIDEO_MB", "UPLOAD_MAX_COUNT",
		"UPLOAD_COMPRESSION_ENABLED", "UPLOAD_WATERMARK_ENABLED",
		"SEARCH_ENABLED", "SEARCH_ADDRESSES", "SEARCH_ADDRESS", "SEARCH_ARTICLES_INDEX",
	} {
		t.Setenv(k, "")
	}
}

// TestLoadInfra_Defaults 无任何外部输入时的默认值。
func TestLoadInfra_Defaults(t *testing.T) {
	chdirTemp(t)
	clearInfraEnv(t)

	cfg := LoadInfra()

	cases := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"storage.driver 默认 local", cfg.Storage.Driver, "local"},
		{"storage.local_dir 默认 ./uploads", cfg.Storage.LocalDir, "./uploads"},
		{"s3.region 默认 us-east-1", cfg.S3.Region, "us-east-1"},
		{"s3.use_ssl 默认 false", cfg.S3.UseSSL, false},
		{"s3.force_path_style 默认 true", cfg.S3.ForcePathStyle, true},
		{"upload.max_image_mb 默认 20", cfg.Upload.MaxImageMB, 20},
		{"upload.max_video_mb 默认 500", cfg.Upload.MaxVideoMB, 500},
		{"upload.max_audio_mb 默认 50", cfg.Upload.MaxAudioMB, 50},
		{"upload.max_other_mb 默认 100", cfg.Upload.MaxOtherMB, 100},
		{"upload.max_count 默认 10", cfg.Upload.MaxCount, 10},
		{"upload.compression_enabled 默认 true", cfg.Upload.CompressionEnabled, true},
		{"upload.watermark_enabled 默认 true", cfg.Upload.WatermarkEnabled, true},
		{"search.enabled 默认 false", cfg.Search.Enabled, false},
		{"search.articles_index 默认 picart_articles", cfg.Search.ArticlesIndex, "picart_articles"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: 得到 %v，期望 %v", c.name, c.got, c.want)
		}
	}

	// 默认四档缩略图规格（顺序即生成顺序）
	wantSpecs := []ThumbSpec{{"thumb", 200}, {"small", 480}, {"medium", 1024}, {"large", 1920}}
	if len(cfg.Upload.ThumbnailSpecs) != len(wantSpecs) {
		t.Fatalf("缩略图规格数量：得到 %d，期望 %d", len(cfg.Upload.ThumbnailSpecs), len(wantSpecs))
	}
	for i, w := range wantSpecs {
		if cfg.Upload.ThumbnailSpecs[i] != w {
			t.Errorf("缩略图规格[%d]：得到 %+v，期望 %+v", i, cfg.Upload.ThumbnailSpecs[i], w)
		}
	}
}

// TestLoadInfra_ScalarEnvOverride 标量键的环境变量覆盖。
// 每个 case 对应 infra.go 里一行 SetDefault；漏了就红。
func TestLoadInfra_ScalarEnvOverride(t *testing.T) {
	cases := []struct {
		env   string
		value string
		check func(*InfraConfig) (interface{}, interface{})
		desc  string
	}{
		{"STORAGE_LOCAL_DIR", "/data/uploads",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Storage.LocalDir, "/data/uploads" },
			"storage.local_dir → STORAGE_LOCAL_DIR"},
		{"S3_REGION", "cn-east-1",
			func(c *InfraConfig) (interface{}, interface{}) { return c.S3.Region, "cn-east-1" },
			"s3.region → S3_REGION"},
		{"S3_USE_SSL", "true",
			func(c *InfraConfig) (interface{}, interface{}) { return c.S3.UseSSL, true },
			"s3.use_ssl → S3_USE_SSL（bool）"},
		{"S3_FORCE_PATH_STYLE", "false",
			func(c *InfraConfig) (interface{}, interface{}) { return c.S3.ForcePathStyle, false },
			"s3.force_path_style → S3_FORCE_PATH_STYLE（bool）"},
		{"CDN_BASE_URL", "https://cdn.example.com",
			func(c *InfraConfig) (interface{}, interface{}) { return c.CDN.BaseURL, "https://cdn.example.com" },
			"cdn.base_url → CDN_BASE_URL（曾因缺 SetDefault 静默失效）"},
		{"UPLOAD_MAX_IMAGE_MB", "5",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Upload.MaxImageMB, 5 },
			"upload.max_image_mb → UPLOAD_MAX_IMAGE_MB"},
		{"UPLOAD_MAX_VIDEO_MB", "100",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Upload.MaxVideoMB, 100 },
			"upload.max_video_mb → UPLOAD_MAX_VIDEO_MB"},
		{"UPLOAD_MAX_COUNT", "3",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Upload.MaxCount, 3 },
			"upload.max_count → UPLOAD_MAX_COUNT"},
		{"UPLOAD_WATERMARK_ENABLED", "false",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Upload.WatermarkEnabled, false },
			"upload.watermark_enabled → UPLOAD_WATERMARK_ENABLED（bool）"},
		{"SEARCH_ARTICLES_INDEX", "custom_idx",
			func(c *InfraConfig) (interface{}, interface{}) { return c.Search.ArticlesIndex, "custom_idx" },
			"search.articles_index → SEARCH_ARTICLES_INDEX"},
	}

	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			chdirTemp(t)
			clearInfraEnv(t)
			t.Setenv(c.env, c.value)

			cfg := LoadInfra()
			got, want := c.check(cfg)
			if got != want {
				t.Errorf("%s：得到 %v，期望 %v", c.desc, got, want)
			}
		})
	}
}

// TestLoadInfra_SearchAddressesEnv 切片键的环境变量覆盖（T-29 核心回归）。
//
// 三种来源取并集：配置文件值 + SEARCH_ADDRESSES（JSON 或逗号）+ SEARCH_ADDRESS 单值。
// viper 的 Unmarshal 不解析 []string 的环境变量，若有人删掉 infra.go 里的显式兜底，
// 本测试会立刻失败（地址列表变空 → 搜索服务连不上 ES）。
func TestLoadInfra_SearchAddressesEnv(t *testing.T) {
	cases := []struct {
		name      string
		addresses string // SEARCH_ADDRESSES
		single    string // SEARCH_ADDRESS
		want      []string
	}{
		{"仅逗号分隔", "http://a:9200,http://b:9200", "", []string{"http://a:9200", "http://b:9200"}},
		{"仅 JSON 数组", `["http://a:9200","http://b:9200"]`, "", []string{"http://a:9200", "http://b:9200"}},
		{"单值兜底 SEARCH_ADDRESS", "", "http://single:9200", []string{"http://single:9200"}},
		{"两者并集", "http://a:9200", "http://single:9200", []string{"http://a:9200", "http://single:9200"}},
		{"去重", "http://a:9200,http://a:9200", "http://a:9200", []string{"http://a:9200"}},
		{"去空白", "  http://a:9200 , , http://b:9200  ", "", []string{"http://a:9200", "http://b:9200"}},
		{"都没有", "", "", []string{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chdirTemp(t)
			clearInfraEnv(t)
			if c.addresses != "" {
				t.Setenv("SEARCH_ADDRESSES", c.addresses)
			}
			if c.single != "" {
				t.Setenv("SEARCH_ADDRESS", c.single)
			}

			cfg := LoadInfra()
			if len(cfg.Search.Addresses) != len(c.want) {
				t.Fatalf("地址数量：得到 %v，期望 %v", cfg.Search.Addresses, c.want)
			}
			for i := range c.want {
				if cfg.Search.Addresses[i] != c.want[i] {
					t.Errorf("地址[%d]：得到 %q，期望 %q（全部：%v）",
						i, cfg.Search.Addresses[i], c.want[i], cfg.Search.Addresses)
				}
			}
		})
	}
}

// TestLoadInfra_SearchEnabledBoolVariants 布尔环境变量的宽松解析。
// 防的是：运维写 yes/1 被 cast 成 false 导致搜索静默不启用。
func TestLoadInfra_SearchEnabledBoolVariants(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"true", true}, {"TRUE", true}, {"1", true}, {"yes", true}, {"Yes", true},
		{"false", false}, {"0", false}, {"no", false}, {"off", false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			chdirTemp(t)
			clearInfraEnv(t)
			t.Setenv("SEARCH_ENABLED", c.raw)

			cfg := LoadInfra()
			if cfg.Search.Enabled != c.want {
				t.Errorf("SEARCH_ENABLED=%q：得到 %v，期望 %v", c.raw, cfg.Search.Enabled, c.want)
			}
		})
	}
}

// TestLoadInfra_DriverNormalization 存储驱动归一化与降级。
//
// 防三个后果严重的静默失败：
//   - 拼写错误（"aws"/"S3 "）不该让上传全挂 → 归一化为 s3 / local；
//   - 写了 s3 但 bucket/endpoint 缺失 → 上传必 500，必须在加载期降级 local；
//   - 大小写不敏感（"S3" 应等价 "s3"）。
func TestLoadInfra_DriverNormalization(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    string
		comment string
	}{
		{"空值回退 local", map[string]string{"STORAGE_DRIVER": ""}, "local", "未配置即本地"},
		{"未知值回退 local", map[string]string{"STORAGE_DRIVER": "aws"}, "local", "拼错不致命"},
		{"大写 S3 归一", map[string]string{
			"STORAGE_DRIVER": "S3", "S3_BUCKET": "b", "S3_ENDPOINT": "e"}, "s3", "大小写不敏感"},
		{"带空格归一", map[string]string{
			"STORAGE_DRIVER": "  S3  ", "S3_BUCKET": "b", "S3_ENDPOINT": "e"}, "s3", "去空白"},
		{"s3 缺 bucket 降级", map[string]string{
			"STORAGE_DRIVER": "s3", "S3_ENDPOINT": "e"}, "local", "配置不全降级，避免上传 500"},
		{"s3 缺 endpoint 降级", map[string]string{
			"STORAGE_DRIVER": "s3", "S3_BUCKET": "b"}, "local", "配置不全降级"},
		{"s3 完整保留", map[string]string{
			"STORAGE_DRIVER": "s3", "S3_BUCKET": "b", "S3_ENDPOINT": "e"}, "s3", "完整配置生效"},
		{"local 显式", map[string]string{"STORAGE_DRIVER": "LOCAL"}, "local", "大写 local"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chdirTemp(t)
			clearInfraEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			cfg := LoadInfra()
			if cfg.Storage.Driver != c.want {
				t.Errorf("%s：driver 得到 %q，期望 %q", c.comment, cfg.Storage.Driver, c.want)
			}
		})
	}
}

// TestLoadInfra_EmptyThumbnailSpecsFallback 缩略图规格为空时回退四档默认。
// 防的是：配置文件写了 thumbnail_specs: [] 后，图片处理链路产出零张缩略图。
func TestLoadInfra_EmptyThumbnailSpecsFallback(t *testing.T) {
	chdirTemp(t)
	clearInfraEnv(t)

	yaml := []byte("upload:\n  thumbnail_specs: []\n")
	if err := os.WriteFile("config.yaml", yaml, 0o644); err != nil {
		t.Fatalf("写 config.yaml 失败: %v", err)
	}

	cfg := LoadInfra()
	if len(cfg.Upload.ThumbnailSpecs) != 4 {
		t.Fatalf("空规格未回退默认：得到 %d 项，期望 4 项", len(cfg.Upload.ThumbnailSpecs))
	}
}

// TestParseListEnv 列表解析单元测试（JSON 优先、逗号兜底、去空）。
func TestParseListEnv(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"JSON 数组", `["a","b"]`, []string{"a", "b"}},
		{"JSON 空数组", `[]`, []string{}},
		{"JSON 非法回落逗号", `["a"`, []string{`["a"`}},
		{"逗号分隔", "a,b,c", []string{"a", "b", "c"}},
		{"逗号带空白", " a , b ", []string{"a", "b"}},
		{"空串", "", []string{}},
		{"全空白", "  ,  ", []string{}},
		{"单值", "http://x:9200", []string{"http://x:9200"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseListEnv(c.raw)
			if len(got) != len(c.want) {
				t.Fatalf("得到 %v，期望 %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("[%d] 得到 %q，期望 %q（全部：%v）", i, got[i], c.want[i], got)
				}
			}
		})
	}
}