package dto

// 本文件覆盖 upload 域（上传记录、上传配置、S3 直传、迁移补偿）的全部对外
// 请求与响应结构。
//
// 字段来源（不是想象，逐条可追）：
//   - UploadDto      ← internal/handler/upload.go 的 infraFormatUpload（22 个键）
//   - UploadConfigDto ← internal/service/infra/storage.go 的 GetUploadConfig（8 段）
//   - PresignResultDto / ConfirmUploadRequest ← internal/service/infra/storage.go 的 S3Presign / S3Confirm
//   - Migration*Dto  ← internal/service/infra/migration.go 的三个 Result 结构
//
// 空值规则（docs/21-JSON序列化规范.md）：
//   - 字符串 → 值类型 string，空时 ""（不用 *string，指针只会带来 null）
//   - 数组   → 去掉 omitempty，赋值时必须 make([]T, 0, n)，空时 []
//   - 对象   → *T + omitempty，空时整个字段不传
//   - 时间   → *string + omitempty，空时整个字段不传（不传 ""、不传零值）

// ---------------------------------------------------------------------------
// 上传记录（响应）
// ---------------------------------------------------------------------------

// UploadThumbnailDto 缩略图项（model.UploadThumbnail，5 字段）。
type UploadThumbnailDto struct {
	Name   string `json:"name" example:"medium"`
	URL    string `json:"url" example:"https://cdn.example.com/uploads/a-medium.webp"`
	Path   string `json:"path" example:"uploads/2026/11/25/a-medium.webp"`
	Size   int64  `json:"size" example:"20480"`
	Width  int    `json:"width" example:"800"`
	Height int    `json:"height" example:"600"`
}

// UploadOriginalDto 压缩后保留的原图信息（model.UploadOriginal，5 字段）。
type UploadOriginalDto struct {
	URL    string `json:"url" example:"https://cdn.example.com/uploads/a-original.webp"`
	Path   string `json:"path" example:"uploads/2026/11/25/a-original.webp"`
	Size   int64  `json:"size" example:"102400"`
	Width  int    `json:"width" example:"1920"`
	Height int    `json:"height" example:"1080"`
}

// UploadVideoJobDto 视频压缩任务信息（model.UploadVideoJob，6 字段）。
//
// completedAt / error 无值时整个字段不传（对象内的可选标量，不是集合）。
type UploadVideoJobDto struct {
	OriginalSize    int64   `json:"originalSize" example:"10485760"`
	CompressedSize  int64   `json:"compressedSize" example:"3145728"`
	CompressionRate float64 `json:"compressionRatio" example:"70"`
	StartedAt       int64   `json:"startedAt" example:"1795540372656"`
	CompletedAt     int64   `json:"completedAt,omitempty" example:"1795540389000"`
	Error           string  `json:"error,omitempty"`
}

// UploadAuditResultDto 图片审核结果（service.AuditResult 的对外投影，6 字段）。
//
// 字段名逐字对齐 internal/service/audit_service.go 的 AuditResult json tag
// （该结构被原样序列化进 uploads.audit_result jsonb 列）。
type UploadAuditResultDto struct {
	Passed     bool    `json:"passed" example:"true"`
	Action     string  `json:"action" example:"pass"`
	Label      string  `json:"label"`
	Reason     string  `json:"reason"`
	Score      float64 `json:"score" example:"0"`
	DetailJSON string  `json:"detailJson"`
}

// UploadDto 上传文件对外视图（infraFormatUpload 的 22 个键）。
//
// 用于：GET /upload（列表项）、GET /upload/info/{id}、POST /upload/file、
// POST /upload/avatar、POST /upload/confirm。
//
// 注意 userId：model 层是 *string（区分「游客上传」的 SQL NULL），此处按
// docs/21 转成值类型 —— 空时 ""，不是 null。前端判断游客上传用 `!userId`。
type UploadDto struct {
	ID           string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Hash         string `json:"hash" example:"e3b0c44298fc1c149afbf4c8996fb924"`
	OriginalName string `json:"originalName" example:"photo.jpg"`
	Storage      string `json:"storage" example:"local"`
	Filename     string `json:"filename" example:"9f8c7b6a5d4e3f2a1b0c.jpg"`
	Path         string `json:"path" example:"uploads/2026/11/25/9f8c7b6a5d4e3f2a1b0c.jpg"`
	URL          string `json:"url" example:"http://127.0.0.1:4011/uploads/2026/11/25/9f8c7b6a5d4e3f2a1b0c.jpg"`
	Size         int64  `json:"size" example:"1572864"`
	MimeType     string `json:"mimeType" example:"image/jpeg"`

	ReferenceCount int `json:"referenceCount" example:"1"`

	Processed   bool `json:"processed" example:"true"`
	Watermarked bool `json:"watermarked" example:"true"`

	VideoCompressionStatus string `json:"videoCompressionStatus" example:"completed"`
	AuditStatus            string `json:"auditStatus" example:"approved"`
	Scene                  string `json:"scene" example:"image"`
	UserID                 string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`

	CreatedAt string `json:"createdAt" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updatedAt" format:"date-time" example:"2026-11-25T10:00:05Z"`

	// ---- 数组字段：空时 []（去掉 omitempty，组装时必须 make）----
	Thumbnails []UploadThumbnailDto `json:"thumbnails"`

	// ---- 对象字段：空时整个字段不传（jsonb 列无值时是 JSON null）----
	Original            *UploadOriginalDto    `json:"original,omitempty"`
	VideoCompressionJob *UploadVideoJobDto    `json:"videoCompressionJob,omitempty"`
	AuditResult         *UploadAuditResultDto `json:"auditResult,omitempty"`
}

// ---------------------------------------------------------------------------
// 上传配置（GET /upload/config）
// ---------------------------------------------------------------------------

// UploadConfigDto 上传限制配置（GetUploadConfig 的 8 段）。
//
// 供前端预压缩用：前端拿到的限制必须等于后端实际执行值（同源读取层保证）。
type UploadConfigDto struct {
	Storage          UploadStorageDto         `json:"storage"`
	Compression      UploadCompressionDto     `json:"compression"`
	Limits           UploadLimitsDto          `json:"limits"`
	AllowedMimeTypes map[string][]string      `json:"allowedMimeTypes"`
	ImageProcessing  UploadImageProcessingDto `json:"imageProcessing"`
	Watermark        UploadWatermarkDto       `json:"watermark"`
	VideoProcessing  UploadVideoProcessingDto `json:"videoProcessing"`
	Queue            UploadQueueDto           `json:"queue"`
}

// UploadStorageDto 存储配置（driver 保持 env，运行期不可改）。
//
// S3 是**指针 + omitempty**：driver != "s3" 时整个 s3 字段不传。
// 这就是 docs/21 里「空对象不传」的落地 —— 改动前该字段是字面量 null
// （gin.H 里塞了 interface{} 的 nil，序列化成 null）。
type UploadStorageDto struct {
	Type string             `json:"type" example:"local"`
	S3   *UploadS3ConfigDto `json:"s3,omitempty"`
}

// UploadS3ConfigDto S3 连接信息（driver=s3 时才出现）。
//
// 五项（endpoint/bucket/region/useSSL/forcePathStyle）故意不读 DB：
// minio client 启动期初始化，改了必须重启。只有 cdnDomain 允许 DB 优先。
type UploadS3ConfigDto struct {
	Region         string `json:"region" example:"us-east-1"`
	Bucket         string `json:"bucket" example:"picart"`
	Endpoint       string `json:"endpoint" example:"minio.example.com:9000"`
	CDNDomain      string `json:"cdnDomain" example:"https://cdn.example.com"`
	ForcePathStyle bool   `json:"forcePathStyle" example:"true"`
	Scheme         string `json:"scheme" example:"https"`
}

// UploadCompressionDto 前端预压缩参数。
type UploadCompressionDto struct {
	Image UploadCompressionImageDto `json:"image"`
	Video UploadCompressionVideoDto `json:"video"`
}

// UploadCompressionImageDto 图片预压缩参数。
type UploadCompressionImageDto struct {
	MaxWidth                 int      `json:"maxWidth" example:"1920"`
	Quality                  int      `json:"quality" example:"75"`
	Format                   string   `json:"format" example:"webp"`
	PreserveAnimation        bool     `json:"preserveAnimation" example:"true"`
	SkipPrecompressMimeTypes []string `json:"skipPrecompressMimeTypes"`
}

// UploadCompressionVideoDto 视频预压缩参数。
type UploadCompressionVideoDto struct {
	Enabled   bool `json:"enabled" example:"true"`
	MaxWidth  int  `json:"maxWidth" example:"1920"`
	MaxHeight int  `json:"maxHeight" example:"1080"`
	CRF       int  `json:"crf" example:"23"`
}

// UploadLimitsDto 上传限制（maxFileCount + 四类大小的 mb/bytes 同源对）。
type UploadLimitsDto struct {
	MaxSize      UploadSizeLimitsDto `json:"maxSize"`
	MaxFileCount int                 `json:"maxFileCount" example:"10"`
}

// UploadSizeLimitsDto 四类文件的大小上限。
type UploadSizeLimitsDto struct {
	Image UploadSizeLimitDto `json:"image"`
	Video UploadSizeLimitDto `json:"video"`
	Audio UploadSizeLimitDto `json:"audio"`
	Other UploadSizeLimitDto `json:"other"`
}

// UploadSizeLimitDto 单类大小的 mb 与 bytes（两值必须同源，避免并发改配置时不一致）。
type UploadSizeLimitDto struct {
	MB    int   `json:"mb" example:"20"`
	Bytes int64 `json:"bytes" example:"20971520"`
}

// UploadImageProcessingDto 服务端图片处理参数（对齐原 image-processor）。
type UploadImageProcessingDto struct {
	CompressionEnabled bool                  `json:"compressionEnabled" example:"true"`
	Format             string                `json:"format" example:"webp"`
	Quality            int                   `json:"quality" example:"85"`
	MaxWidth           int                   `json:"maxWidth" example:"3840"`
	MaxHeight          int                   `json:"maxHeight" example:"2160"`
	KeepOriginal       bool                  `json:"keepOriginal" example:"true"`
	Animated           UploadAnimatedOptsDto `json:"animated"`
}

// UploadAnimatedOptsDto 动图处理选项。
type UploadAnimatedOptsDto struct {
	StaticThumb            bool `json:"staticThumb" example:"true"`
	OriginalKeepsAnimation bool `json:"originalKeepsAnimation" example:"true"`
}

// UploadWatermarkDto 水印参数（与 watermark.go 的 watermarkOptions 同源）。
type UploadWatermarkDto struct {
	Enabled      bool     `json:"enabled" example:"true"`
	TextTemplate string   `json:"textTemplate" example:"PicArt"`
	Position     string   `json:"position" example:"bottom-right"`
	Opacity      float64  `json:"opacity" example:"0.6"`
	SkipScenes   []string `json:"skipScenes"`
}

// UploadVideoProcessingDto 服务端视频压缩参数。
type UploadVideoProcessingDto struct {
	CompressionEnabled bool   `json:"compressionEnabled" example:"true"`
	Preset             string `json:"preset" example:"medium"`
	CRF                int    `json:"crf" example:"23"`
	MaxWidth           int    `json:"maxWidth" example:"1920"`
	MaxHeight          int    `json:"maxHeight" example:"1080"`
	VideoBitrate       string `json:"videoBitrate" example:"5000k"`
	AudioBitrate       string `json:"audioBitrate" example:"128k"`
	MinCompressSize    int    `json:"minCompressSize" example:"10"`
}

// UploadQueueDto 队列名（前端排障用：后台任务进哪个队列）。
type UploadQueueDto struct {
	VideoCompression string `json:"videoCompression" example:"video:compression"`
	TaskType         string `json:"taskType" example:"video:compress"`
}

// ---------------------------------------------------------------------------
// S3 直传（请求 + 响应）
// ---------------------------------------------------------------------------

// PresignRequest 申请预签名 PUT URL（三个字段全部必填）。
type PresignRequest struct {
	Filename    string `json:"filename" binding:"required" example:"photo.jpg"`
	ContentType string `json:"contentType" binding:"required" example:"image/jpeg"`
	Size        int64  `json:"size" binding:"required" example:"1572864"`
}

// PresignResultDto 预签名结果（service.S3Presign，4 字段）。
type PresignResultDto struct {
	UploadURL string `json:"uploadUrl" example:"https://minio.example.com/picart/uploads/...&X-Amz-Signature=..."`
	AccessURL string `json:"accessUrl" example:"https://cdn.example.com/uploads/2026/11/25/a.jpg"`
	Key       string `json:"key" example:"uploads/2026/11/25/a.jpg"`
	ExpiresIn int    `json:"expiresIn" example:"3600"`
}

// ConfirmUploadRequest 确认直传完成（service.S3Confirm，7 字段）。
//
// 前 5 个必填（key/url/originalName/mimeType/size），hash 与 scene 可选。
// 请求体的可选字段按 docs/21 §四 例外保留「不传 = 不改」语义。
type ConfirmUploadRequest struct {
	Key          string `json:"key" binding:"required" example:"uploads/2026/11/25/a.jpg"`
	URL          string `json:"url" binding:"required" example:"https://cdn.example.com/uploads/2026/11/25/a.jpg"`
	OriginalName string `json:"originalName" binding:"required" example:"photo.jpg"`
	MimeType     string `json:"mimeType" binding:"required" example:"image/jpeg"`
	Size         int64  `json:"size" binding:"required" example:"1572864"`
	Hash         string `json:"hash" example:"e3b0c44298fc1c149afbf4c8996fb924"`
	Scene        string `json:"scene" example:"image"`
}

// ---------------------------------------------------------------------------
// 迁移补偿（管理端）
// ---------------------------------------------------------------------------

// MigrationStatsDto 压缩处理统计（service.MigrationStatsResult，8 字段）。
type MigrationStatsDto struct {
	Total          int64  `json:"total" example:"128"`
	Images         int64  `json:"images" example:"120"`
	Processed      int64  `json:"processed" example:"118"`
	Unprocessed    int64  `json:"unprocessed" example:"2"`
	WithThumbnails int64  `json:"withThumbnails" example:"118"`
	WithOriginal   int64  `json:"withOriginal" example:"118"`
	Watermarked    int64  `json:"watermarked" example:"110"`
	ProcessedPct   string `json:"processedPercent" example:"98.33"`
}

// MigrationProcessRequest 批量处理未压缩图片的请求体（query 同名参数并存，body 优先）。
type MigrationProcessRequest struct {
	Limit  *int  `json:"limit" example:"100"`
	DryRun *bool `json:"dryRun" example:"false"`
}

// MigrationProcessResultDto 批量处理结果（service.MigrationProcessResult，6 字段）。
type MigrationProcessResultDto struct {
	Total     int      `json:"total" example:"100"`
	Processed int      `json:"processed" example:"95"`
	Failed    int      `json:"failed" example:"3"`
	Skipped   int      `json:"skipped" example:"2"`
	DryRun    bool     `json:"dryRun" example:"false"`
	Errors    []string `json:"errors"`
}

// MigrationFillResultDto 元数据补齐结果（service.MigrationFillResult，4 字段）。
type MigrationFillResultDto struct {
	Total  int      `json:"total" example:"100"`
	Filled int      `json:"filled" example:"98"`
	Failed int      `json:"failed" example:"2"`
	Errors []string `json:"errors"`
}

// DeletedDto 删除类动作的统一 data（{"deleted":true}）。
//
// 不只 upload 域在用（emoji/decoration 的删除端点同形），此处定义一次供
// 三个域复用 —— 这正是 DTO 复用的价值（docs/18 §三）。
type DeletedDto struct {
	Deleted bool `json:"deleted" example:"true"`
}
