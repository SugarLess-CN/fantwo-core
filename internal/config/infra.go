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

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	_ = v.ReadInConfig() // 缺失即只用环境变量与默认值

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &InfraConfig{}
	_ = v.Unmarshal(cfg)

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
