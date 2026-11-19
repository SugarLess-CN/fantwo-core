// Package model 基础设施域实体：上传文件表（upload）。
//
// 对齐原 TypEORM 实体 tmp/picart/src/modules/upload/entities/upload.entity.ts：
// 字段、枚举、默认值逐一平移；主键按骨架约定用 UUID char(36)（docs/04 §5.4）。
// original / thumbnails / videoCompressionJob / auditResult 在原实体中是 json 列，
// Go 侧统一以 jsonb 文本列承载（UploadJSON），由 service 层序列化/反序列化，
// 对外响应再由 service 组装成结构化对象——避免给每个嵌套结构写 Valuer/Scanner。
package model

// 上传存储类型（原 UploadStorageType）。
const (
	UploadStorageLocal = "local"
	UploadStorageS3    = "s3"
)

// 视频压缩状态（原 videoCompressionStatus enum）。
const (
	VideoCompressionPending    = "pending"
	VideoCompressionProcessing = "processing"
	VideoCompressionCompleted  = "completed"
	VideoCompressionFailed     = "failed"
)

// 图片审核状态（原 auditStatus enum）。
const (
	UploadAuditPending  = "pending"
	UploadAuditApproved = "approved"
	UploadAuditRejected = "rejected"
)

// UploadThumbnail 缩略图信息（原 ThumbnailInfo）。
type UploadThumbnail struct {
	Name   string `json:"name"`   // 尺寸名：thumb / small / medium / large
	URL    string `json:"url"`    // 访问 URL
	Path   string `json:"path"`   // 存储路径（本地磁盘路径或对象 key）
	Size   int64  `json:"size"`   // 字节数
	Width  int    `json:"width"`  // 宽
	Height int    `json:"height"` // 高
}

// UploadOriginal 原图信息（原 OriginalInfo，压缩后保留原图时写入）。
type UploadOriginal struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// UploadVideoJob 视频压缩任务信息（原 videoCompressionJob）。
type UploadVideoJob struct {
	OriginalSize    int64   `json:"originalSize"`
	CompressedSize  int64   `json:"compressedSize"`
	CompressionRate float64 `json:"compressionRatio"`
	StartedAt       int64   `json:"startedAt"`             // 毫秒时间戳
	CompletedAt     int64   `json:"completedAt,omitempty"` // 毫秒时间戳
	Error           string  `json:"error,omitempty"`
}

// Upload 上传文件表（原 uploads 表）。
//
// TableName 显式指定表名：GORM 默认复数化为 uploads，与原表名一致，此处写明避免歧义。
type Upload struct {
	BaseModel

	Hash         string `gorm:"column:hash;type:varchar(128);uniqueIndex;not null" json:"hash"` // 文件 sha256，去重标识
	OriginalName string `gorm:"column:original_name;type:varchar(255);not null" json:"originalName"`
	Storage      string `gorm:"column:storage;type:varchar(16);not null;default:local" json:"storage"`
	Filename     string `gorm:"column:filename;type:varchar(255);not null" json:"filename"`
	Path         string `gorm:"column:path;type:text;not null" json:"path"`
	URL          string `gorm:"column:url;type:text;not null" json:"url"`
	Size         int64  `gorm:"column:size;not null;default:0" json:"size"`
	MimeType     string `gorm:"column:mime_type;type:varchar(128);not null" json:"mimeType"`

	ReferenceCount int `gorm:"column:reference_count;not null;default:1" json:"referenceCount"`

	OriginalJSON string `gorm:"column:original;type:jsonb" json:"-"`
	ThumbsJSON   string `gorm:"column:thumbnails;type:jsonb" json:"-"`

	Processed   bool `gorm:"column:processed;not null;default:false" json:"processed"`
	Watermarked bool `gorm:"column:watermarked;not null;default:false" json:"watermarked"`

	VideoCompressionStatus string `gorm:"column:video_compression_status;type:varchar(16);not null;default:pending;index:idx_upload_video_status" json:"videoCompressionStatus"`
	VideoJobJSON           string `gorm:"column:video_compression_job;type:jsonb" json:"-"`

	// Scene 上传场景（avatar / image / other）：avatar 不加水印（对齐原 skipScenes）
	Scene string `gorm:"column:scene;type:varchar(16);not null;default:image;index:idx_upload_scene" json:"scene"`
	// UserID 上传者（游客上传为 NULL）；水印取昵称、管理端按人筛选都用它
	UserID *string `gorm:"column:user_id;type:char(36);index:idx_upload_user" json:"userId"`

	AuditStatus   string `gorm:"column:audit_status;type:varchar(16);not null;default:pending" json:"auditStatus"`
	AuditResultJS string `gorm:"column:audit_result;type:jsonb" json:"-"`
}

// TableName 表名（对齐原 TypeORM uploads）。
func (Upload) TableName() string { return "uploads" }
