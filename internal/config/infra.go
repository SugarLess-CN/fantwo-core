// 基础设施域配置（上传存储 / S3 / CDN / 上传限制 / Elasticsearch）。
//
// 独立于 internal/config/config.go 的骨架主配置：并行开发期避免与骨架域改同一文件，
// 语义上仍是同一个 config.yaml + 环境变量体系（viper，点号转下划线，无前缀）。
//
// config.yaml 片段示例：
//
//	storage:
//	  driver: local          # local | s3
//	  local_dir: ./uploads
//	s3:
//	  endpoint: 127.0.0.1:9000
//	  bucket: picart
//	  region: us-east-1
//	  access_key: xxx
//	  secret_key: xxx
//	  use_ssl: false
//	  force_path_style: true
//	cdn:
//	  base_url: https://cdn.example.com
//	search:
//	  enabled: true
//	  addresses: ["http://127.0.0.1:9200"]
//	  articles_index: picart_articles
package config

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// InfraConfig 基础设施域配置根。
type InfraConfig struct {
	Storage StorageConfig      `mapstructure:"storage"`
	S3      S3Config           `mapstructure:"s3"`
	CDN     CDNConfig          `mapstructure:"cdn"`
	Upload  UploadLimitConfig  `mapstructure:"upload"`
	Search  SearchEngineConfig `mapstructure:"search"`
}

// StorageConfig 存储驱动选择。
type StorageConfig struct {
	// Driver local（写本地磁盘，默认，本地开发）或 s3（minio-go，S3 兼容）。
	Driver string `mapstructure:"driver"`
	// LocalDir 本地存储根目录，默认 ./uploads（相对工作目录）。
	LocalDir string `mapstructure:"local_dir"`
}

// S3Config S3 兼容对象存储（MinIO / AWS S3 / 阿里云 OSS S3 兼容层）。
type S3Config struct {
	Endpoint       string `mapstructure:"endpoint"`
	Bucket         string `mapstructure:"bucket"`
	Region         string `mapstructure:"region"`
	AccessKey      string `mapstructure:"access_key"`
	SecretKey      string `mapstructure:"secret_key"`
	UseSSL         bool   `mapstructure:"use_ssl"`
	ForcePathStyle bool   `mapstructure:"force_path_style"`
}

// CDNConfig CDN 域名配置：返回给前端的文件 URL 前缀。
type CDNConfig struct {
	// BaseURL 形如 https://cdn.example.com；为空时按存储驱动回退（本地为请求域名，S3 为 endpoint/bucket）。
	BaseURL string `mapstructure:"base_url"`
}

// UploadLimitConfig 上传限制（对齐原 upload-limit.util 默认值）。
type UploadLimitConfig struct {
	MaxImageMB int `mapstructure:"max_image_mb"` // 默认 20
	MaxVideoMB int `mapstructure:"max_video_mb"` // 默认 500
	MaxAudioMB int `mapstructure:"max_audio_mb"` // 默认 50
	MaxOtherMB int `mapstructure:"max_other_mb"` // 默认 100
	MaxCount   int `mapstructure:"max_count"`    // 单次文件数，默认 10

	CompressionEnabled bool `mapstructure:"compression_enabled"` // 图片压缩，默认 true
	WatermarkEnabled   bool `mapstructure:"watermark_enabled"`   // 图片水印，默认 true
	// 缩略图规格（宽,高），等比缩放不裁剪；顺序即生成顺序。
	ThumbnailSpecs []ThumbSpec `mapstructure:"thumbnail_specs"`
}

// ThumbSpec 单个缩略图规格。
type ThumbSpec struct {
	Name string `mapstructure:"name"`
	Size int    `mapstructure:"size"` // 长边像素
}

// SearchEngineConfig Elasticsearch 配置。
type SearchEngineConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	Addresses     []string `mapstructure:"addresses"`
	Username      string   `mapstructure:"username"`
	Password      string   `mapstructure:"password"`
	ArticlesIndex string   `mapstructure:"articles_index"` // 默认 picart_articles
}

// LoadInfra 加载基础设施域配置（config.yaml + 环境变量覆盖，缺文件不报错）。
func LoadInfra() *InfraConfig {
	v := viper.New()

	v.SetDefault("storage.driver", "local")
	v.SetDefault("storage.local_dir", "./uploads")
	// 注意：viper 的 Unmarshal 只遍历 AllSettings()，AutomaticEnv 不会把
	// 「无默认值且不在配置文件里」的键加入其中 —— 所以**每个可能用环境变量
	// 覆盖的键都必须在这里 SetDefault**，否则 S3_ENDPOINT / CDN_BASE_URL 等
	// 环境变量会被静默忽略（与 internal/config/config.go 踩的是同一个坑）。
	v.SetDefault("s3.endpoint", "")
	v.SetDefault("s3.bucket", "")
	v.SetDefault("s3.access_key", "")
	v.SetDefault("s3.secret_key", "")
	v.SetDefault("s3.region", "us-east-1")
	v.SetDefault("s3.use_ssl", false)
	v.SetDefault("s3.force_path_style", true)
	v.SetDefault("cdn.base_url", "")
	v.SetDefault("upload.max_image_mb", 20)
	v.SetDefault("upload.max_video_mb", 500)
	v.SetDefault("upload.max_audio_mb", 50)
	v.SetDefault("upload.max_other_mb", 100)
	v.SetDefault("upload.max_count", 10)
	v.SetDefault("upload.compression_enabled", true)
	v.SetDefault("upload.watermark_enabled", true)
	// 缩略图规格对齐原 image-processor 的 thumb / small / medium / large
	v.SetDefault("upload.thumbnail_specs", []map[string]interface{}{
		{"name": "thumb", "size": 200},
		{"name": "small", "size": 480},
		{"name": "medium", "size": 1024},
		{"name": "large", "size": 1920},
	})
	v.SetDefault("search.enabled", false)
	v.SetDefault("search.articles_index", "picart_articles")
	// search.addresses 是切片：viper 的 Unmarshal 不会从环境变量解析数组，
	// 这里给空切片占位（让键进入 AllSettings），真正的 env 解析见下方显式处理；
	// search.address 是单值兜底键（SEARCH_ADDRESS），两者在下方取并集。
	v.SetDefault("search.addresses", []string{})
	v.SetDefault("search.address", "")

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	_ = v.ReadInConfig() // 缺失即只用环境变量与默认值

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &InfraConfig{}
	_ = v.Unmarshal(cfg)

	// 切片类配置的显式 env 覆盖：viper 的 AutomaticEnv 只对「读取单键」生效，
	// Unmarshal 到 []string 时不会解析环境变量（已知限制），因此显式兜一层。
	//
	// 支持三种来源，取并集（去重、去空）：
	//   1) config.yaml 的 search.addresses（已由 Unmarshal 读入 cfg）
	//   2) 环境变量 SEARCH_ADDRESSES：JSON 数组或逗号分隔
	//   3) 环境变量 SEARCH_ADDRESS / 配置项 search.address：单个地址
	addresses := make([]string, 0, 4)
	seen := map[string]struct{}{}
	addAddr := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" {
			return
		}
		if _, ok := seen[a]; ok {
			return
		}
		seen[a] = struct{}{}
		addresses = append(addresses, a)
	}
	// 1) config.yaml 的 search.addresses（已由 Unmarshal 读入 cfg）。
	//    但 SEARCH_ADDRESSES 存在时**必须跳过**：viper 的 Unmarshal 会把 JSON 数组型
	//    环境变量按逗号切成畸形元素（如 `["http://a:9200"` 与 `"http://b:9200"]`），
	//    此时 cfg.Search.Addresses 已被污染，只能以 parseListEnv 的解析结果为准。
	//    （回归测试：infra_test.go TestLoadInfra_SearchAddressesEnv/仅_JSON_数组）
	if strings.TrimSpace(os.Getenv("SEARCH_ADDRESSES")) == "" {
		for _, a := range cfg.Search.Addresses {
			addAddr(a)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SEARCH_ADDRESSES")); raw != "" {
		for _, a := range parseListEnv(raw) {
			addAddr(a)
		}
	}
	addAddr(os.Getenv("SEARCH_ADDRESS"))
	addAddr(v.GetString("search.address"))
	cfg.Search.Addresses = addresses

	// 布尔/字符串键的显式 env 兜底（SetDefault 后 AutomaticEnv 已能生效，这里再保一层，
	// 防止有人用 1/0、yes/no 之类非标准写法被 cast 成 false）。
	if raw := strings.TrimSpace(os.Getenv("SEARCH_ENABLED")); raw != "" {
		cfg.Search.Enabled = raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
	}

	// 存储驱动归一化：空值/未知值回退 local，避免拼写错误导致上传静默失败。
	cfg.Storage.Driver = strings.ToLower(strings.TrimSpace(cfg.Storage.Driver))
	if cfg.Storage.Driver != "s3" {
		cfg.Storage.Driver = "local"
	}
	if cfg.Storage.Driver == "s3" && (cfg.S3.Bucket == "" || cfg.S3.Endpoint == "") {
		// S3 配置不完整时降级为本地存储，保证上传端点可用（启动期不 panic）。
		cfg.Storage.Driver = "local"
	}
	if len(cfg.Upload.ThumbnailSpecs) == 0 {
		cfg.Upload.ThumbnailSpecs = []ThumbSpec{
			{Name: "thumb", Size: 200},
			{Name: "small", Size: 480},
			{Name: "medium", Size: 1024},
			{Name: "large", Size: 1920},
		}
	}
	return cfg
}

// parseListEnv 解析列表型环境变量：优先按 JSON 数组，失败则按逗号分隔。
func parseListEnv(raw string) []string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var list []string
		if err := json.Unmarshal([]byte(raw), &list); err == nil {
			return list
		}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if item := strings.TrimSpace(p); item != "" {
			out = append(out, item)
		}
	}
	return out
}
