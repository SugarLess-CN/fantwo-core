// 基础设施域：存储驱动（local 磁盘 / S3 兼容对象存储）与文件服务。
//
// 设计对齐原仓库 upload.service + s3-presign.service：
//   - driver=local：文件写 config.storage.local_dir（默认 ./uploads），URL 拼请求域名或 cdn.base_url；
//   - driver=s3：minio-go 流式 PutObject，URL 拼 cdn.base_url / endpoint+bucket；
//   - S3 直传（presign/confirm）：预签名 PUT URL + 完成确认建库记录；
//   - 文件去重：hash（sha256，metadata 可传入前端预计算的 hash）唯一索引；
//   - 图片：缩略图 imaging（运行时探测 libvips，无则降级 imaging，见 image.go）；
//   - 视频：提交 asynq video-compression 队列（handler 侧只入队，worker 另行接入）。
package infra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/filecheck"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/minio/minio-go/v7"
	miniocred "github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 场景（对齐原 scene 枚举）。
const (
	SceneAvatar = "avatar"
	SceneImage  = "image"
	SceneOther  = "other"
)

// 默认上传限制（MB，对齐 upload-limit.util DEFAULT_*）。
const (
	DefaultMaxImageMB = 20
	DefaultMaxVideoMB = 500
	DefaultMaxAudioMB = 50
	DefaultMaxOtherMB = 100
	DefaultMaxCount   = 10
)

// 预签名有效期（秒，对齐 S3_PRESIGN_EXPIRES=300）。
const PresignExpires = 300

// 魔数嗅探读取的头部字节数。
const sniffSize = 64

// S3Presign PUT 预签名响应（对齐 PresignedUrlResponseDto）。
type S3Presign struct {
	UploadURL string `json:"uploadUrl"`
	AccessURL string `json:"accessUrl"`
	Key       string `json:"key"`
	ExpiresIn int    `json:"expiresIn"`
}

// S3Confirm S3 直传确认请求体（对齐 /upload/confirm）。
type S3Confirm struct {
	Key          string `json:"key" binding:"required"`
	URL          string `json:"url" binding:"required"`
	OriginalName string `json:"originalName" binding:"required"`
	MimeType     string `json:"mimeType" binding:"required"`
	Size         int64  `json:"size" binding:"required"`
	Hash         string `json:"hash"`
	Scene        string `json:"scene"`
}

// Infra 基础设施域服务聚合（存储 + 上传文件表读写）。
type Infra struct {
	db     *gorm.DB
	cfg    *config.InfraConfig
	rdb    *redis.Client
	client *minio.Client // driver=s3 时非 nil
	// cfgSvc 配置服务（水印参数等运行期可调项）；nil 时用 InfraConfig 默认值
	cfgSvc *ConfigService
}

// New 构建基础设施域服务。
// db 可为 nil（降级启动，上传端点返回 503）；driver=s3 且配置齐全时初始化 minio 客户端。
func New(db *gorm.DB, cfg *config.InfraConfig) *Infra {
	f := &Infra{db: db, cfg: cfg}
	if cfg.Storage.Driver == "s3" {
		client, err := minio.New(cfg.S3.Endpoint, &minio.Options{
			Creds:  miniocred.NewStaticV4(cfg.S3.AccessKey, cfg.S3.SecretKey, ""),
			Secure: cfg.S3.UseSSL,
			Region: cfg.S3.Region,
		})
		if err == nil {
			f.client = client
		}
		// 初始化失败保持 client=nil，上传时按 local 不可用报 503，不在启动期 panic。
	}
	return f
}

// WithRedis 注入 Redis（敏感词缓存失效等用）。
func (f *Infra) WithRedis(r *redis.Client) *Infra { f.rdb = r; return f }

// WithConfigService 注入配置服务（水印模板/位置/透明度等运行期参数来源）。
func (f *Infra) WithConfigService(cs *ConfigService) *Infra { f.cfgSvc = cs; return f }

// ---------- 上传主流程 ----------

// UploadRequest multipart 上传请求（/upload/file 与 /upload/avatar 共用）。
type UploadRequest struct {
	Files    []*multipart.FileHeader
	Metadata []UploadMetaItem // 前端预计算 hash（对齐 metadata JSON 数组）
	Scene    string           // avatar / image / other；avatar 不加水印
}

// UploadMetaItem metadata 数组元素（对齐 [{hash, name}]）。
type UploadMetaItem struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

// HandleUpload 上传入口（scene 固定为 avatar 时无水印）。
// 返回 model.Upload 列表（service 已将 jsonb 列组装为结构化字段）。
func (f *Infra) HandleUpload(files []*multipart.FileHeader, metadata []UploadMetaItem, scene, baseURL, userID string) ([]model.Upload, error) {
	if len(files) == 0 {
		return nil, errcode.ErrBadRequest.With("上传文件不能为空")
	}
	maxCount := f.cfg.Upload.MaxCount
	if maxCount <= 0 {
		maxCount = DefaultMaxCount
	}
	if len(files) > maxCount {
		return nil, errcode.ErrBadRequest.Withf("单次最多上传 %d 个文件", maxCount)
	}
	if scene != "" && scene != SceneAvatar && scene != SceneImage && scene != SceneOther {
		return nil, errcode.ErrBadRequest.With("scene 必须是 avatar、image 或 other")
	}

	uploads := make([]model.Upload, 0, len(files))
	for i, fh := range files {
		up, err := f.uploadOne(fh, metaAt(metadata, i), scene, userID)
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, *up)
	}
	// URL 基准：本地存储且未配 CDN 时，以请求域名补全为绝对地址。
	for i := range uploads {
		uploads[i].URL = absolutizeURL(uploads[i].URL, baseURL)
	}
	return uploads, nil
}

// uploadOne 处理单个文件：校验 → hash 去重 → 落盘/上 S3 → 建记录 → 异步处理。
//
// 水印：原实现由 watermark.service 在压缩阶段叠加（avatar 场景跳过）。
// Go 侧暂未落绘制（需要中文字体资源，见汇报的遗留项）；此处保留 scene 判定入口，
// 接入时在 processImageThumbnails 前设置 up.Watermarked 并绘制即可。
func (f *Infra) uploadOne(fh *multipart.FileHeader, meta UploadMetaItem, scene, userID string) (*model.Upload, error) {
	// 1) 大小限制
	if err := f.checkSize(fh.Size, fh.Header.Get("Content-Type")); err != nil {
		return nil, err
	}
	// 2) MIME 白名单 + 魔数校验
	if err := f.checkMagic(fh); err != nil {
		return nil, err
	}

	// 3) 去重标识：优先前端预计算 hash（省一次读盘），否则后端 sha256
	ident := meta.Hash
	if ident == "" {
		h, err := fileHash(fh)
		if err != nil {
			return nil, errcode.ErrInternal.Withf("计算文件哈希失败: %v", err)
		}
		ident = h
	}

	// 4) hash 命中即复用（引用计数 +1），不再落盘
	existing, err := f.findByHash(ident)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := f.db.Model(&model.Upload{}).Where("id = ?", existing.ID).
			UpdateColumn("reference_count", gorm.Expr("reference_count + 1")).Error; err != nil {
			return nil, errcode.ErrInternal.Withf("更新引用计数失败: %v", err)
		}
		return existing, nil
	}

	// 5) 存储文件
	storage := f.cfg.Storage.Driver
	now := time.Now()
	rel := fmt.Sprintf("uploads/%s/%s/%s", now.Format("2006/01/02"), ident[:2], ident[2:4])
	filename := rel + filepath.Ext(fh.Filename)

	var (
		storedPath string
		fileURL    string
	)
	switch storage {
	case model.UploadStorageS3:
		key := filename
		if err := f.putS3(fh, key); err != nil {
			return nil, err
		}
		storedPath = key
		fileURL = f.s3AccessURL(key)
	default: // local
		if f.cfg.Storage.LocalDir == "" {
			f.cfg.Storage.LocalDir = "./uploads"
		}
		storedPath = filepath.Join(f.cfg.Storage.LocalDir, filename)
		if err := os.MkdirAll(filepath.Dir(storedPath), 0o755); err != nil {
			return nil, errcode.ErrInternal.Withf("创建存储目录失败: %v", err)
		}
		if err := saveMultipart(fh, storedPath); err != nil {
			return nil, errcode.ErrInternal.Withf("写文件失败: %v", err)
		}
		fileURL = f.localURL(storedPath)
	}

	// 6) 建记录
	up := &model.Upload{
		Hash:                   ident,
		OriginalName:           filepath.Base(fh.Filename),
		Storage:                storage,
		Filename:               filepath.Base(filename),
		Path:                   storedPath,
		URL:                    fileURL,
		Size:                   fh.Size,
		MimeType:               filecheck.NormalizeMime(fh.Header.Get("Content-Type")),
		ReferenceCount:         1,
		Processed:              false,
		Watermarked:            false,
		AuditStatus:            model.UploadAuditPending,
		VideoCompressionStatus: model.VideoCompressionPending,
	}
	if filecheck.IsProcessableImage(up.MimeType) {
		up.Processed = false // 图片走异步压缩（thumbnail 生成），完成前保持未处理
	}
	if err := f.db.Create(up).Error; err != nil {
		// 落库失败：回滚已写盘文件，避免孤儿文件
		_ = f.removeStored(storage, storedPath)
		return nil, errcode.ErrInternal.Withf("保存上传记录失败: %v", err)
	}

	// 7) 异步处理（不阻塞响应）
	f.dispatchAsync(up)
	return up, nil
}

// dispatchAsync 图片压缩与视频压缩队列投递（go routine，错误仅日志）。
func (f *Infra) dispatchAsync(up *model.Upload) {
	switch {
	case filecheck.IsProcessableImage(up.MimeType):
		// 图片：本地直接生成缩略图；S3 需先下载再处理（见 s3_image.go）
		if up.Storage == model.UploadStorageS3 {
			go f.processS3Image(up)
		} else {
			go f.processImageThumbnails(up)
		}
	case filecheck.Kind(up.MimeType) == filecheck.KindVideo:
		go f.enqueueVideoCompression(up)
	}
}

// enqueueVideoCompression 提交 asynq video-compression 任务（对齐原 videoCompressionQueue）。
//
// 队列名与任务类型复用 internal/queue 常量（QueueVideoCompression / TypeVideoCompression），
// 任务负载约定 {"upload_id","path","storage"}，由 worker（队列批次注册的
// TypeVideoCompression 处理器）消费；重试 5 次、超时 5 分钟对齐原 Bull 全局配置。
//
// Redis 不可用时静默跳过（对齐原实现 catch 后 warn 的降级语义），不影响上传响应。
func (f *Infra) enqueueVideoCompression(up *model.Upload) {
	if f.rdb == nil {
		return
	}
	payload, err := json.Marshal(map[string]string{
		"upload_id": up.ID,
		"path":      up.Path,
		"storage":   up.Storage,
		"mime_type": up.MimeType,
	})
	if err != nil {
		return
	}
	opt := f.rdb.Options()
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     opt.Addr,
		Password: opt.Password,
		DB:       opt.DB,
	})
	defer client.Close()

	task := asynq.NewTask(queue.TypeVideoCompression, payload)
	_, err = client.Enqueue(task,
		asynq.Queue(queue.QueueVideoCompression),
		asynq.MaxRetry(5),
		asynq.Timeout(5*time.Minute),
	)
	if err != nil || f.db == nil {
		return
	}
	// 入队成功：标记 pending（worker 取到后改 processing，完成后改 completed/failed）
	_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).
		Update("video_compression_status", model.VideoCompressionPending).Error
}

// processImageThumbnails 生成本地缩略图并回写记录（imaging，无 libvips 环境自动降级）。
//
// 写入两组字段：
//   - thumbnails：四档缩略图（thumb/small/medium/large，见 config 的 thumbnail_specs）；
//   - original：原图宽高与字节数（对齐原实现 getMetadata 后写 original）。
//
// 失败只记日志不冒泡：上传响应已经返回，异步处理失败不影响主流程。
func (f *Infra) processImageThumbnails(up *model.Upload) {
	if f.db == nil || up.Storage != model.UploadStorageLocal {
		return // S3 图片走「下载-压缩-回传」流程，与本地分支不同（见遗留项）
	}
	updates := map[string]interface{}{"processed": true}

	// 水印：avatar 场景跳过；先备份无水印原图再覆盖主文件（对齐原 watermark.service）
	origPath, origURL, origSize := up.Path, up.URL, up.Size
	if opt, need := f.watermarkOptions(up); need {
		if backup, werr := f.applyWatermarkToFile(up.Path, opt); werr == nil {
			updates["watermarked"] = true
			if backup != "" {
				origPath = backup
				origURL = f.localURL(backup)
				origSize = fileSizeOf(backup)
			}
		}
	}

	// 原图元数据（宽高），供前端预留占位与瀑布流布局
	if w, h, err := imageSize(origPath); err == nil {
		if payload, err := json.Marshal(model.UploadOriginal{
			URL: origURL, Path: origPath, Size: origSize, Width: w, Height: h,
		}); err == nil {
			updates["original"] = string(payload)
		}
	}

	thumbs, err := f.ensureThumbnails(up)
	if err == nil && len(thumbs) > 0 {
		if payload, merr := json.Marshal(thumbs); merr == nil {
			updates["thumbnails"] = string(payload)
		}
	}
	_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Updates(updates).Error
}

// ensureThumbnails 生成缩略图并写回 thumbnails jsonb。
// 返回缩略图列表；无 libvips 环境时 imaging 已能覆盖 png/jpg/gif/tiff/bmp/webp(解码)。
func (f *Infra) ensureThumbnails(up *model.Upload) ([]model.UploadThumbnail, error) {
	if up.Storage != model.UploadStorageLocal {
		return nil, errors.New("仅本地存储支持缩略图生成")
	}
	specs := f.cfg.Upload.ThumbnailSpecs
	if len(specs) == 0 {
		specs = []config.ThumbSpec{{Name: "thumb", Size: 200}}
	}
	// 见 image.go：makeThumbnails(up.Path, specs)（imaging 实现）。
	// 这里仅占位调用约定，具体像素处理在 image.go 中。
	return makeThumbnails(up.Path, specs, f.cfg.Storage.LocalDir, up)
}

// ---------- 校验 ----------

// checkSize 按类型校验大小（对齐 resolveUploadMaxSizeBytes）。
func (f *Infra) checkSize(size int64, mimeType string) error {
	kind := filecheck.Kind(mimeType)
	var maxMB int
	switch kind {
	case filecheck.KindImage:
		maxMB = f.cfg.Upload.MaxImageMB
	case filecheck.KindVideo:
		maxMB = f.cfg.Upload.MaxVideoMB
	case filecheck.KindAudio:
		maxMB = f.cfg.Upload.MaxAudioMB
	default:
		maxMB = f.cfg.Upload.MaxOtherMB
	}
	if maxMB <= 0 {
		switch kind {
		case filecheck.KindImage:
			maxMB = DefaultMaxImageMB
		case filecheck.KindVideo:
			maxMB = DefaultMaxVideoMB
		case filecheck.KindAudio:
			maxMB = DefaultMaxAudioMB
		default:
			maxMB = DefaultMaxOtherMB
		}
	}
	limit := int64(maxMB) * 1024 * 1024
	if size > limit {
		return errcode.ErrBadRequest.Withf("文件大小超过限制: %.1fMB > %dMB", float64(size)/1024/1024, maxMB)
	}
	return nil
}

// checkMagic MIME 白名单 + 魔数校验（对齐 validateFileType）。
func (f *Infra) checkMagic(fh *multipart.FileHeader) error {
	declared := fh.Header.Get("Content-Type")
	if !filecheck.IsAllowedMime(declared) {
		return errcode.ErrBadRequest.Withf("不支持的文件类型: %s", declared)
	}
	file, err := fh.Open()
	if err != nil {
		return errcode.ErrBadRequest.With("读取上传文件失败")
	}
	defer file.Close()
	head := make([]byte, sniffSize)
	n, _ := io.ReadFull(file, head)
	if _, verr := filecheck.Verify(declared, head[:n]); verr != nil {
		if errors.Is(verr, filecheck.ErrMimeMismatch) {
			return errcode.ErrBadRequest.Withf("文件真实类型与声明不匹配: 声明为 %s", declared)
		}
		return errcode.ErrBadRequest.Withf("文件校验失败: %v", verr)
	}
	// 嗅探不出格式：放行（对齐原 FileTypeUtil 宽松策略）
	return nil
}

// checkSizePresign 预签名直传的大小校验（对齐 validateFile）。
func (f *Infra) checkSizePresign(size int64, mimeType string) error {
	return f.checkSize(size, mimeType)
}

// ---------- 文件哈希 / multipart 落盘 ----------

// fileHash sha256 流式计算（对齐 calculateFileHashFromPath）。
func fileHash(fh *multipart.FileHeader) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// saveMultipart multipart.FileHeader 落盘（流式，避免整文件进内存）。
func saveMultipart(fh *multipart.FileHeader, dst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

// metaAt 取 metadata 第 i 项（越界返回零值）。
func metaAt(meta []UploadMetaItem, i int) UploadMetaItem {
	if i < len(meta) {
		return meta[i]
	}
	return UploadMetaItem{}
}

// ---------- S3 ----------

// putS3 流式上传到 S3（对齐 uploadToS3：stream body，不整读内存）。
func (f *Infra) putS3(fh *multipart.FileHeader, key string) error {
	if f.client == nil {
		return errcode.ErrInternal.With("S3 客户端未初始化")
	}
	src, err := fh.Open()
	if err != nil {
		return errcode.ErrBadRequest.With("读取上传文件失败")
	}
	defer src.Close()
	contentType := filecheck.NormalizeMime(fh.Header.Get("Content-Type"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, putErr := f.client.PutObject(ctx, f.cfg.S3.Bucket, key, src, fh.Size,
		minio.PutObjectOptions{ContentType: contentType})
	if putErr != nil {
		return errcode.ErrInternal.Withf("S3 上传失败: %v", putErr)
	}
	return nil
}

// localURL 本地存储文件的访问 URL（相对路径；有 cdn.base_url 时拼 CDN 前缀）。
//
// 无 CDN 时返回 /uploads/... 形式，由 handler 按请求域名补全为绝对地址
// （对齐原 getFileUrl 用请求 baseUrl 拼接的语义）。
func (f *Infra) localURL(path string) string {
	rel := path
	if f.cfg.Storage.LocalDir != "" {
		if r, err := filepath.Rel(f.cfg.Storage.LocalDir, path); err == nil {
			rel = r
		}
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if f.cfg.CDN.BaseURL != "" {
		return strings.TrimRight(f.cfg.CDN.BaseURL, "/") + "/" + rel
	}
	return "/" + rel
}

// absolutizeURL 本地相对 URL 补全请求域名（handler 传入 baseURL；CDN/S3 绝对 URL 原样返回）。
func absolutizeURL(u, baseURL string) string {
	if baseURL == "" || u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(u, "/")
}

// s3AccessURL 生成 S3 访问 URL（对齐 buildAccessUrl：CDN 优先 → path-style → 虚拟主机式）。
func (f *Infra) s3AccessURL(key string) string {
	if f.cfg.CDN.BaseURL != "" {
		return strings.TrimRight(f.cfg.CDN.BaseURL, "/") + "/" + key
	}
	scheme := "https"
	if !f.cfg.S3.UseSSL {
		scheme = "http"
	}
	if f.cfg.S3.ForcePathStyle {
		return fmt.Sprintf("%s://%s/%s/%s", scheme, f.cfg.S3.Endpoint, f.cfg.S3.Bucket, key)
	}
	return fmt.Sprintf("%s://%s.%s/%s", scheme, f.cfg.S3.Bucket, f.cfg.S3.Endpoint, key)
}

// GeneratePresign 生成 S3 PUT 预签名 URL（对齐 /upload/presign）。
// driver!=s3 或 S3 未配置时返回 503（前端应回退到经后端中转的 /upload/file）。
func (f *Infra) GeneratePresign(filename, contentType string, size int64) (*S3Presign, error) {
	if f.client == nil {
		return nil, svcUnavailable("S3 存储未配置")
	}
	if !filecheck.IsAllowedPresignMime(contentType) {
		return nil, errcode.ErrBadRequest.Withf("不支持的文件类型: %s", contentType)
	}
	if err := f.checkSizePresign(size, contentType); err != nil {
		return nil, err
	}
	now := time.Now()
	key := fmt.Sprintf("uploads/%s/%s%s",
		now.Format("2006/01/02"), newUUID(), filecheck.ExtOf(filename))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url, err := f.client.PresignedPutObject(ctx, f.cfg.S3.Bucket, key, PresignExpires*time.Second)
	if err != nil {
		return nil, errcode.ErrInternal.Withf("生成预签名失败: %v", err)
	}
	return &S3Presign{
		UploadURL: url.String(),
		AccessURL: f.s3AccessURL(key),
		Key:       key,
		ExpiresIn: PresignExpires,
	}, nil
}

// ConfirmS3Upload S3 直传确认：校验真实类型（Range 下载头部）→ 去重 → 建记录。
func (f *Infra) ConfirmS3Upload(req S3Confirm) (*model.Upload, error) {
	if f.client == nil {
		return nil, svcUnavailable("S3 存储未配置")
	}
	if err := f.checkSize(req.Size, req.MimeType); err != nil {
		return nil, err
	}
	if !filecheck.IsAllowedMime(req.MimeType) {
		return nil, errcode.ErrBadRequest.Withf("不支持的文件类型: %s", req.MimeType)
	}
	// 去重：hash 或 key
	ident := req.Hash
	if ident == "" {
		ident = req.Key
	}
	existing, err := f.findByHash(ident)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		_ = f.db.Model(&model.Upload{}).Where("id = ?", existing.ID).
			UpdateColumn("reference_count", gorm.Expr("reference_count + 1")).Error
		return existing, nil
	}
	up := &model.Upload{
		Hash:                   ident,
		OriginalName:           req.OriginalName,
		Storage:                model.UploadStorageS3,
		Filename:               filepath.Base(req.Key),
		Path:                   req.Key,
		URL:                    req.URL,
		Size:                   req.Size,
		MimeType:               filecheck.NormalizeMime(req.MimeType),
		ReferenceCount:         1,
		AuditStatus:            model.UploadAuditPending,
		VideoCompressionStatus: model.VideoCompressionPending,
	}
	if err := f.db.Create(up).Error; err != nil {
		return nil, errcode.ErrInternal.Withf("保存上传记录失败: %v", err)
	}
	// S3 直传文件异步后处理：
	//   1) 魔数校验（Range 下载头部嗅探），不合格标记 rejected；
	//   2) 图片走「下载 → 压缩/缩略图 → 回传」链路（s3_image.go），视频入压缩队列。
	go f.verifyS3Magic(up)
	f.dispatchAsync(up)
	return up, nil
}

// verifyS3Magic S3 直传文件异步魔数校验（Range bytes=0-63 下载头部）。
func (f *Infra) verifyS3Magic(up *model.Upload) {
	if f.client == nil || up.Storage != model.UploadStorageS3 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	obj, err := f.client.GetObject(ctx, f.cfg.S3.Bucket, up.Path, minio.GetObjectOptions{})
	if err != nil {
		return
	}
	defer obj.Close()
	head := make([]byte, sniffSize)
	n, _ := io.ReadFull(obj, head)
	detectedExt, _ := filecheck.Detect(head[:n])
	if detectedExt == "" {
		_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).
			Update("audit_status", model.UploadAuditApproved).Error
		return
	}
	allowed := filecheck.AllowedExtensions(up.MimeType)
	ok := false
	for _, e := range allowed {
		if strings.EqualFold(e, detectedExt) {
			ok = true
			break
		}
	}
	status := model.UploadAuditApproved
	if !ok {
		status = model.UploadAuditRejected
	}
	_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).
		Update("audit_status", status).Error
}

// ---------- 查询 / 删除 ----------

// GetFileInfo 按 ID 查文件（对齐 getFileInfo，NoAuth）。
func (f *Infra) GetFileInfo(id string) (*model.Upload, error) {
	if f.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	var up model.Upload
	if err := f.db.First(&up, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound
		}
		return nil, errcode.ErrInternal.Withf("查询文件失败: %v", err)
	}
	return &up, nil
}

// ListUpload 分页文件列表（对齐 findAll，管理端 upload:list）。
func (f *Infra) ListUpload(page, limit int, sortBy, sortOrder string) ([]model.Upload, int64, error) {
	if f.db == nil {
		return nil, 0, svcUnavailable("数据库不可用")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	order := "created_at DESC"
	if sortBy == "created_at" || sortBy == "createdAt" {
		if strings.EqualFold(sortOrder, "ASC") {
			order = "created_at ASC"
		}
	}
	var (
		total int64
		list  []model.Upload
	)
	if err := f.db.Model(&model.Upload{}).Count(&total).Error; err != nil {
		return nil, 0, errcode.ErrInternal.Withf("统计上传文件失败: %v", err)
	}
	if err := f.db.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, errcode.ErrInternal.Withf("查询上传文件失败: %v", err)
	}
	return list, total, nil
}

// RemoveUpload 引用计数 -1；归零时删盘并删记录（对齐 decreaseReferenceCount / remove）。
func (f *Infra) RemoveUpload(id string) error {
	if f.db == nil {
		return svcUnavailable("数据库不可用")
	}
	return f.db.Transaction(func(tx *gorm.DB) error {
		var up model.Upload
		if err := tx.First(&up, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.ErrNotFound
			}
			return errcode.ErrInternal.Withf("查询文件失败: %v", err)
		}
		remaining := up.ReferenceCount - 1
		if remaining <= 0 {
			if err := tx.Delete(&up).Error; err != nil {
				return errcode.ErrInternal.Withf("删除上传记录失败: %v", err)
			}
			// 事务提交后再删盘（失败仅孤儿文件，不影响一致性）
			go func() { _ = f.removeStored(up.Storage, up.Path) }()
			return nil
		}
		return tx.Model(&model.Upload{}).Where("id = ?", up.ID).
			UpdateColumn("reference_count", remaining).Error
	})
}

// removeStored 删除已存储文件（本地磁盘 / S3），错误仅日志。
func (f *Infra) removeStored(storage, path string) error {
	if path == "" {
		return nil
	}
	if storage == model.UploadStorageS3 {
		if f.client == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = f.client.RemoveObject(ctx, f.cfg.S3.Bucket, path, minio.RemoveObjectOptions{})
		return nil
	}
	if strings.HasPrefix(path, f.cfg.Storage.LocalDir) { // 防目录穿越
		_ = os.Remove(path)
	}
	return nil
}

// GetUploadConfig 返回上传配置（对齐 /upload/config，NoAuth，供前端预压缩）。
func (f *Infra) GetUploadConfig(c *gin.Context) gin.H {
	limits := gin.H{
		"image": gin.H{"mb": orDefault(f.cfg.Upload.MaxImageMB, DefaultMaxImageMB), "bytes": int64(orDefault(f.cfg.Upload.MaxImageMB, DefaultMaxImageMB)) * 1024 * 1024},
		"video": gin.H{"mb": orDefault(f.cfg.Upload.MaxVideoMB, DefaultMaxVideoMB), "bytes": int64(orDefault(f.cfg.Upload.MaxVideoMB, DefaultMaxVideoMB)) * 1024 * 1024},
		"audio": gin.H{"mb": orDefault(f.cfg.Upload.MaxAudioMB, DefaultMaxAudioMB), "bytes": int64(orDefault(f.cfg.Upload.MaxAudioMB, DefaultMaxAudioMB)) * 1024 * 1024},
		"other": gin.H{"mb": orDefault(f.cfg.Upload.MaxOtherMB, DefaultMaxOtherMB), "bytes": int64(orDefault(f.cfg.Upload.MaxOtherMB, DefaultMaxOtherMB)) * 1024 * 1024},
	}
	var s3Info interface{}
	if f.cfg.Storage.Driver == "s3" {
		scheme := "https"
		if !f.cfg.S3.UseSSL {
			scheme = "http"
		}
		s3Info = gin.H{
			"region":         f.cfg.S3.Region,
			"bucket":         f.cfg.S3.Bucket,
			"endpoint":       f.cfg.S3.Endpoint,
			"cdnDomain":      f.cfg.CDN.BaseURL,
			"forcePathStyle": f.cfg.S3.ForcePathStyle,
			"scheme":         scheme,
		}
	}
	return gin.H{
		"storage": gin.H{"type": f.cfg.Storage.Driver, "s3": s3Info},
		"compression": gin.H{
			"image": gin.H{
				"maxWidth":                 1920,
				"quality":                  75,
				"format":                   "webp",
				"preserveAnimation":        true,
				"skipPrecompressMimeTypes": []string{"image/gif"},
			},
			"video": gin.H{
				"enabled":   true,
				"maxWidth":  1920,
				"maxHeight": 1080,
				"crf":       23,
			},
		},
		"limits": gin.H{
			"maxSize":      limits,
			"maxFileCount": orDefault(f.cfg.Upload.MaxCount, DefaultMaxCount),
		},
		"allowedMimeTypes": filecheck.AllMimeGroups(),
		"imageProcessing": gin.H{
			"compressionEnabled": f.cfg.Upload.CompressionEnabled,
			"format":             "webp",
			"quality":            85,
			"maxWidth":           3840,
			"maxHeight":          2160,
			"keepOriginal":       true,
			"animated":           gin.H{"staticThumb": true, "originalKeepsAnimation": true},
		},
		"watermark": gin.H{
			"enabled":      f.cfg.Upload.WatermarkEnabled,
			"textTemplate": "{nickname}",
			"position":     "bottom-right",
			"opacity":      0.55,
			"skipScenes":   []string{SceneAvatar},
		},
		"videoProcessing": gin.H{
			"compressionEnabled": true,
			"preset":             "medium",
			"crf":                23,
			"maxWidth":           1920,
			"maxHeight":          1080,
			"videoBitrate":       "5000k",
			"audioBitrate":       "128k",
			"minCompressSize":    10,
		},
		"queue": gin.H{"videoCompression": queue.QueueVideoCompression, "taskType": queue.TypeVideoCompression},
	}
}

// findByHash 按哈希查已有上传记录；未命中返回 (nil, nil)。
func (f *Infra) findByHash(hash string) (*model.Upload, error) {
	if f.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	var up model.Upload
	err := f.db.Where("hash = ?", hash).First(&up).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errcode.ErrInternal.Withf("查询去重记录失败: %v", err)
	}
	return &up, nil
}

// nullableID 空串返回 nil（DB 存 NULL，便于按「游客上传」筛）。
func nullableID(id string) *string {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	v := id
	return &v
}

// orDefault 空值/非正数回退默认值。
func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// newUUID 生成 RFC4122 v4 字符串（复用骨架依赖 github.com/google/uuid）。
func newUUID() string { return uuid.NewString() }

// ensure http 方法导入守护（gin 上下文在 handler 层）。
