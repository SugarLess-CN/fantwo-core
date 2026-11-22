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
	"sync"
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
	// 单次文件数上限：env 兜底 + DB 优先（upload_max_file_count）
	maxCount := f.uploadMaxFileCount()
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
	// 本地图片：同步并行处理（水印 + 缩略图 + 原图元数据），
	// 让上传响应直接带回处理后的 size / thumbnails / watermarked，
	// 前端一次拿到完整数据，无需再轮询 /upload/info。
	//
	// 并发上限 3：单张 20MB 图处理约百毫秒级，串行 10 张会明显拖慢上传响应。
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i := range uploads {
		up := &uploads[i]
		if up.Storage != model.UploadStorageLocal || !filecheck.IsProcessableImage(up.MimeType) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(item *model.Upload) {
			defer wg.Done()
			defer func() { <-sem }()
			f.processImageThumbnails(item)
		}(up)
	}
	wg.Wait()

	// URL 基准：本地存储且未配 CDN 时，以请求域名补全为绝对地址。
	for i := range uploads {
		uploads[i].URL = absolutizeURL(uploads[i].URL, baseURL)
	}
	// 回读：把异步/同步处理写入的 size / thumbnails / original / watermarked 带回响应
	for i := range uploads {
		if fresh, err := f.GetFileInfo(uploads[i].ID); err == nil {
			uploads[i] = *fresh
			uploads[i].URL = absolutizeURL(uploads[i].URL, baseURL)
		}
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
	// 相对键：<年>/<月>/<日>/<hash前2>/<hash次2><ext>，与上传场景无关
	rel := fmt.Sprintf("%s/%s/%s/%s", now.Format("2006/01/02"), ident[:2], ident[2:4], ident)
	rel += filepath.Ext(fh.Filename)
	// 本地：直接落在 storage.local_dir 下；S3：对象键加 uploads/ 前缀（桶内目录惯例）
	filename := rel

	var (
		storedPath string
		fileURL    string
	)
	switch storage {
	case model.UploadStorageS3:
		key := "uploads/" + filename
		if err := f.putS3(fh, key); err != nil {
			return nil, err
		}
		storedPath = key
		fileURL = f.s3AccessURL(key)
	default: // local
		// 本地根目录：env 兜底 + DB 优先（storage_local_dir）；不再回写 f.cfg 避免并发写共享配置
		localDir := f.localDirRoot()
		storedPath = filepath.Join(localDir, filename)
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
		Scene:                  orDefaultStr(scene, SceneImage),
		UserID:                 nullableID(userID),
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
		// 本地图片由 HandleUpload 同步并行处理（见下），这里只处理 S3（需先下载）
		if up.Storage == model.UploadStorageS3 {
			go f.processS3Image(up)
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

// processImageThumbnails 本地图片后处理：水印 + 缩略图 + 原图元数据，回写记录。
//
// 语义对齐原仓库 upload.service.ts:332-373（srcUpload 的处理分支）：
//
//   - 压缩关 + 水印不要 → **跳过整个后处理**（不重新编码、不生成缩略图），只把
//     processed 置 true 表明「已决最终态」，审核照常由 handler 侧发起；
//   - 压缩关 + 水印要   → **只打水印**（水印必然重新编码主文件），不生成缩略图；
//   - 压缩开            → 水印（如需）+ 按 thumbnail_specs 生成缩略图。
//
// 压缩开关系 upload_compression_enabled（env 兜底 + DB 优先，见 compressionEnabled）；
// 水印是否需要沿用 watermarkOptions 的既有判定（开关 + 场景排除 + 模板替换后非空）。
//
// 写入字段：thumbnails / original / size / watermarked / processed。
// 失败只记日志不冒泡：上传响应已经返回，异步处理失败不影响主流程。
func (f *Infra) processImageThumbnails(up *model.Upload) {
	if f.db == nil || up.Storage != model.UploadStorageLocal {
		return // S3 图片走「下载-压缩-回传」流程，见 s3_image.go
	}
	compression := f.compressionEnabled()
	wmOpt, needWatermark := f.watermarkOptions(up)

	// ① 压缩关 + 水印不要：整体跳过。
	// processed 置 true 而非留 false —— processed 是「该图已决最终态」的补偿标记，
	// migration.go 按 processed=false 拣选补处理，留 false 会被反复挑中重跑。
	if !compression && !needWatermark {
		_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).
			Update("processed", true).Error
		return
	}

	updates := map[string]interface{}{"processed": true}

	// ② 水印：独立于压缩判定；avatar 场景已由 watermarkOptions 排除。
	// 先备份无水印原图再覆盖主文件（对齐原 watermark.service）。
	origPath, origURL, origSize := up.Path, up.URL, up.Size
	if needWatermark {
		if backup, werr := f.applyWatermarkToFile(up.Path, wmOpt); werr == nil {
			updates["watermarked"] = true
			if backup != "" {
				origPath = backup
				origURL = f.localURL(backup)
				origSize = fileSizeOf(backup)
			}
			// 水印后主文件尺寸变化：size 同步为处理后实际字节数
			//（original.size 保留原图大小，两者口径分开，前端按需取）
			if newSize := fileSizeOf(up.Path); newSize > 0 {
				updates["size"] = newSize
				up.Size = newSize
			}
		}
	}

	// 原图元数据（宽高），供前端预留占位与瀑布流布局。
	// 放在缩略图之前：压缩关闭时不做缩略图，但前端仍需要原图宽高排版。
	if w, h, err := imageSize(origPath); err == nil {
		if payload, err := json.Marshal(model.UploadOriginal{
			URL: origURL, Path: origPath, Size: origSize, Width: w, Height: h,
		}); err == nil {
			updates["original"] = string(payload)
		}
	}

	// ③ 缩略图：仅压缩开启时生成（对齐原实现的 if (compressionEnabled) 分支）。
	if compression {
		thumbs, err := f.ensureThumbnails(up)
		if err == nil && len(thumbs) > 0 {
			if payload, merr := json.Marshal(thumbs); merr == nil {
				updates["thumbnails"] = string(payload)
			}
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
	specs := f.thumbnailSpecs()
	// 像素处理在 image.go 中（imaging 实现，无 libvips 依赖）。
	return f.makeThumbnails(up.Path, specs)
}

// ---------- 校验 ----------

// checkSize 按类型校验大小（对齐 resolveUploadMaxSizeBytes）。
//
// 上限来源与 /upload/config 展示同源（uploadLimitMB：env 兜底 + DB 优先），
// 保证「前端看到的限制」与「后端实际拒绝的阈值」永远一致。
func (f *Infra) checkSize(size int64, mimeType string) error {
	kind := filecheck.Kind(mimeType)
	key := model.ConfigKeyUploadMaxOtherSize
	envMB, defMB := f.cfg.Upload.MaxOtherMB, DefaultMaxOtherMB
	switch kind {
	case filecheck.KindImage:
		key, envMB, defMB = model.ConfigKeyUploadMaxImageSize, f.cfg.Upload.MaxImageMB, DefaultMaxImageMB
	case filecheck.KindVideo:
		key, envMB, defMB = model.ConfigKeyUploadMaxVideoSize, f.cfg.Upload.MaxVideoMB, DefaultMaxVideoMB
	case filecheck.KindAudio:
		key, envMB, defMB = model.ConfigKeyUploadMaxAudioSize, f.cfg.Upload.MaxAudioMB, DefaultMaxAudioMB
	}
	maxMB := f.uploadLimitMB(key, envMB, defMB)
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
		if errors.Is(verr, filecheck.ErrMediaWithoutMagic) {
			return errcode.ErrBadRequest.Withf("文件内容不是有效的 %s 文件（文件头无法识别）", declared)
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
	// 本地根目录与写入同源（env 兜底 + DB 优先），否则改 DB 目录后 Rel 失败会拼出错误 URL。
	if dir := f.localDirRoot(); dir != "" {
		if r, err := filepath.Rel(dir, path); err == nil {
			rel = r
		}
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if cdn := f.cdnBaseURL(); cdn != "" {
		return strings.TrimRight(cdn, "/") + "/" + rel
	}
	// 无 CDN：走本服务内置静态文件路由（见 router/upload_routes.go 的 /uploads 静态挂载）
	return "/api/v1/uploads/" + rel
}

// LocalDir 本地存储根目录（静态文件路由用；非本地驱动返回空串）。
func (f *Infra) LocalDir() string {
	if f.cfg.Storage.Driver != model.UploadStorageLocal {
		return ""
	}
	return f.localDirRoot()
}

// absolutizeURL 本地相对 URL 补全请求域名（handler 传入 baseURL；CDN/S3 绝对 URL 原样返回）。
func absolutizeURL(u, baseURL string) string {
	if baseURL == "" || u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(u, "/")
}

// s3AccessURL 生成 S3 访问 URL（对齐 buildAccessUrl：CDN 优先 → path-style → 虚拟主机式）。
//
// 说明：cdn_base_url 走 DB 优先（纯 URL 前缀，改后立即生效）；
// endpoint/bucket/use_ssl/force_path_style 保持 env —— minio client 在启动期固定，
// 若此处改用 DB 值，URL 指向的桶/端点会与实际读写目标错位（改桶后老文件删除失败）。
func (f *Infra) s3AccessURL(key string) string {
	if cdn := f.cdnBaseURL(); cdn != "" {
		return strings.TrimRight(cdn, "/") + "/" + key
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
func (f *Infra) ConfirmS3Upload(req S3Confirm, userID string) (*model.Upload, error) {
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
		Scene:                  orDefaultStr(req.Scene, SceneImage),
		UserID:                 nullableID(userID),
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
//
// onlyUserID 非空时只返回该用户上传的文件——用于「无 upload:list 权限的登录用户
// 查看自己的上传」（普通用户视角），避免把全站文件暴露给普通账号。
func (f *Infra) ListUpload(page, limit int, sortBy, sortOrder, onlyUserID string) ([]model.Upload, int64, error) {
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
	q := f.db.Model(&model.Upload{})
	if onlyUserID != "" {
		q = q.Where("user_id = ?", onlyUserID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errcode.ErrInternal.Withf("统计上传文件失败: %v", err)
	}
	q2 := f.db.Order(order)
	if onlyUserID != "" {
		q2 = q2.Where("user_id = ?", onlyUserID)
	}
	if err := q2.Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
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
	if strings.HasPrefix(path, f.localDirRoot()) { // 防目录穿越（与写入目录同源）
		_ = os.Remove(path)
	}
	return nil
}

// GetUploadConfig 返回上传配置（对齐 /upload/config，NoAuth，供前端预压缩）。
//
// 所有运行期可调项均为「env 兜底 + DB 优先」（key 见 internal/model/config.go），
// 与真实校验/处理路径共用同一批辅助函数，保证「前端拿到的限制」= 「后端实际执行」。
func (f *Infra) GetUploadConfig(c *gin.Context) gin.H {
	// mb 每个类型只算一次：mb 与 bytes 必须同源，避免两处独立读取在并发改配置时不一致。
	imageMB := f.uploadLimitMB(model.ConfigKeyUploadMaxImageSize, f.cfg.Upload.MaxImageMB, DefaultMaxImageMB)
	videoMB := f.uploadLimitMB(model.ConfigKeyUploadMaxVideoSize, f.cfg.Upload.MaxVideoMB, DefaultMaxVideoMB)
	audioMB := f.uploadLimitMB(model.ConfigKeyUploadMaxAudioSize, f.cfg.Upload.MaxAudioMB, DefaultMaxAudioMB)
	otherMB := f.uploadLimitMB(model.ConfigKeyUploadMaxOtherSize, f.cfg.Upload.MaxOtherMB, DefaultMaxOtherMB)
	const mbBytes = int64(1024 * 1024)
	limits := gin.H{
		"image": gin.H{"mb": imageMB, "bytes": int64(imageMB) * mbBytes},
		"video": gin.H{"mb": videoMB, "bytes": int64(videoMB) * mbBytes},
		"audio": gin.H{"mb": audioMB, "bytes": int64(audioMB) * mbBytes},
		"other": gin.H{"mb": otherMB, "bytes": int64(otherMB) * mbBytes},
	}
	var s3Info interface{}
	// storage.driver 保持 env：minio client 启动期初始化，改了必须重启，不属于运行期可调项。
	if f.cfg.Storage.Driver == "s3" {
		// S3 五项（endpoint/bucket/region/use_ssl/force_path_style）**故意不读 DB**：
		// minio client 在启动期创建（见本文件 New，storage.go:97-100），对象读写全部走该
		// client，改了 env 必须重启才生效。若展示 DB 值而实际用 env 值，会出现「前端看到
		// 桶 B、实际写桶 A」，删老文件还会删错桶 —— 这类假配置比不迁移更危险，故连种子
		// 一起不落库（lead 2026-11-22 裁定：移出运行期可调清单）。
		// 只有 cdn_base_url 是真运行期生效的（只影响 URL 拼接，不碰 client），保留 DB 优先。
		scheme := "https"
		if !f.cfg.S3.UseSSL {
			scheme = "http"
		}
		s3Info = gin.H{
			"region":         f.cfg.S3.Region,
			"bucket":         f.cfg.S3.Bucket,
			"endpoint":       f.cfg.S3.Endpoint,
			"cdnDomain":      f.cdnBaseURL(), // DB 优先（cdn_base_url）
			"forcePathStyle": f.cfg.S3.ForcePathStyle,
			"scheme":         scheme,
		}
	}
	// 水印段与 watermark.go 的 watermarkOptions 同源（同 key 常量、同默认值），
	// 保证前端展示的开关/模板/位置/透明度与实际叠加行为一致。
	wmEnabled := f.cfg.Upload.WatermarkEnabled
	wmTemplate := wmDefaultTemplate
	wmPosition := wmDefaultPosition
	wmOpacity := wmDefaultOpacity
	if f.cfgSvc != nil {
		wmEnabled = f.cfgSvc.GetBool(model.ConfigKeyUploadWatermarkEnabled, wmEnabled)
		wmTemplate = f.cfgSvc.GetString(model.ConfigKeyUploadWatermarkTextTemplate, wmTemplate)
		wmPosition = f.cfgSvc.GetString(model.ConfigKeyUploadWatermarkPosition, wmPosition)
		if v := f.cfgSvc.GetInt(model.ConfigKeyUploadWatermarkOpacityPercent, int(wmDefaultOpacity*100)); v > 0 {
			wmOpacity = float64(v) / 100
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
			"maxFileCount": f.uploadMaxFileCount(),
		},
		"allowedMimeTypes": filecheck.AllMimeGroups(),
		"imageProcessing": gin.H{
			"compressionEnabled": f.compressionEnabled(),
			"format":             "webp",
			"quality":            85,
			"maxWidth":           3840,
			"maxHeight":          2160,
			"keepOriginal":       true,
			"animated":           gin.H{"staticThumb": true, "originalKeepsAnimation": true},
		},
		"watermark": gin.H{
			"enabled":      wmEnabled,
			"textTemplate": wmTemplate,
			"position":     wmPosition,
			"opacity":      wmOpacity,
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

// ---------- 运行期可调项（env 兜底 + DB 优先） ----------
//
// 统一范式对齐 watermark.go:183-196：先取 env/InfraConfig 值作默认，
// cfgSvc 非 nil 时用 DB config 表覆盖（后台改配置无需重启）。
// 所有读取路径（校验、落盘、URL 生成、删除、/upload/config）都必须走这里的
// 同一批辅助函数，否则 DB 改目录后「写入目录」与「删除前缀判断」会错位。

// uploadLimitMB 单类型上传上限（MB）：env 兜底 → DB 优先。
func (f *Infra) uploadLimitMB(key string, envMB, defMB int) int {
	mb := orDefault(envMB, defMB)
	if f.cfgSvc != nil {
		mb = f.cfgSvc.GetInt(key, mb)
	}
	if mb <= 0 {
		mb = defMB
	}
	return mb
}

// uploadMaxFileCount 单次上传文件数上限：env 兜底 → DB 优先。
func (f *Infra) uploadMaxFileCount() int {
	n := orDefault(f.cfg.Upload.MaxCount, DefaultMaxCount)
	if f.cfgSvc != nil {
		n = f.cfgSvc.GetInt(model.ConfigKeyUploadMaxFileCount, n)
	}
	if n <= 0 {
		n = DefaultMaxCount
	}
	return n
}

// compressionEnabled 图片压缩总开关：env 兜底 → DB 优先。
func (f *Infra) compressionEnabled() bool {
	enabled := f.cfg.Upload.CompressionEnabled
	if f.cfgSvc != nil {
		enabled = f.cfgSvc.GetBool(model.ConfigKeyUploadCompression, enabled)
	}
	return enabled
}

// localDirRoot 本地存储根目录：env 兜底 → DB 优先 → 空值兜底 ./uploads。
//
// 写入（uploadOne）、URL 生成（localURL）、删除（removeStored 的目录穿越前缀判断）、
// 静态路由（LocalDir）必须全部走本函数，保证同源。
func (f *Infra) localDirRoot() string {
	dir := f.cfg.Storage.LocalDir
	if f.cfgSvc != nil {
		dir = f.cfgSvc.GetString(model.ConfigKeyStorageLocalDir, dir)
	}
	if strings.TrimSpace(dir) == "" {
		return "./uploads"
	}
	return dir
}

// cdnBaseURL CDN 前缀：env 兜底 → DB 优先。
func (f *Infra) cdnBaseURL() string {
	u := f.cfg.CDN.BaseURL
	if f.cfgSvc != nil {
		u = f.cfgSvc.GetString(model.ConfigKeyCDNBaseURL, u)
	}
	return strings.TrimSpace(u)
}

// thumbnailSpecs 缩略图规格：env 兜底 → DB 优先（JSON 数组）→ 单档兜底。
//
// DB 值解析失败、为空、或全部 Size<=0 时回退 env 值，不因配置脏数据中断缩略图生成。
// 本地（ensureThumbnails）与 S3（s3_image.go）两条链路共用本函数，保证同源。
func (f *Infra) thumbnailSpecs() []config.ThumbSpec {
	specs := f.cfg.Upload.ThumbnailSpecs
	if f.cfgSvc != nil {
		if raw := f.cfgSvc.GetString(model.ConfigKeyUploadThumbnailSpecs, ""); strings.TrimSpace(raw) != "" {
			var parsed []config.ThumbSpec
			if err := json.Unmarshal([]byte(raw), &parsed); err == nil && len(parsed) > 0 {
				valid := make([]config.ThumbSpec, 0, len(parsed))
				for _, s := range parsed {
					if s.Size > 0 {
						valid = append(valid, s)
					}
				}
				if len(valid) > 0 {
					specs = valid
				}
			}
		}
	}
	if len(specs) == 0 {
		specs = []config.ThumbSpec{{Name: "thumb", Size: 200}}
	}
	return specs
}

// newUUID 生成 RFC4122 v4 字符串（复用骨架依赖 github.com/google/uuid）。
func newUUID() string { return uuid.NewString() }

// ensure http 方法导入守护（gin 上下文在 handler 层）。
