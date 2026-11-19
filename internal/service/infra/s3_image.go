// 基础设施域：S3 图片的「下载 → 压缩 → 回传」链路。
//
// 背景：/upload/presign + /upload/confirm 让前端把原图直传对象存储（不经后端），
// 因此后端拿不到文件字节，压缩与缩略图只能在 confirm 之后异步补做。
//
// 流程（对齐原 upload.service 的 processS3ImageAsync 语义）：
//  1. GetObject 把原图下载到临时文件（不整读内存，避免大图 OOM）；
//  2. imaging 解码：超限尺寸按 maxWidth/maxHeight 等比缩放，编码 JPEG(q85)；
//  3. 生成四档缩略图（thumb/small/medium/large，等比长边缩放）；
//  4. 压缩图与缩略图分别 PutObject 回传，key 用同目录派生前缀（不覆盖原图）；
//  5. 更新记录：url/path/filename/size/mimeType 指向压缩图，
//     original 保留原图信息，thumbnails 落四档缩略图，processed=true。
//
// 失败处理：任一步失败只标记 processed=false（保留原图可用），由
// /upload/migration/process 或下次重试补齐；不删除已上传对象。
package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/filecheck"
	"github.com/disintegration/imaging"
	"github.com/minio/minio-go/v7"
)

// 压缩参数（与原实现的 upload.compression.* 默认值对齐）。
const (
	s3CompressMaxWidth  = 3840
	s3CompressMaxHeight = 2160
	s3CompressQuality   = 85
	// s3DownloadTimeout 单张原图下载超时。
	s3DownloadTimeout = 5 * time.Minute
	// 派生对象 key 前缀（不覆盖原图，便于回滚与排查）
	keyCompressedPrefix = "c_"
	keyThumbPrefix      = "t_"
)

// processS3Image 异步处理 S3 直传图片：下载 → 压缩/缩略图 → 回传 → 更新记录。
func (f *Infra) processS3Image(up *model.Upload) {
	if f.db == nil || f.client == nil || up.Storage != model.UploadStorageS3 {
		return
	}
	if !filecheck.IsProcessableImage(up.MimeType) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s3DownloadTimeout)
	defer cancel()

	// 1) 下载到临时文件
	tmpPath, err := f.downloadObject(ctx, up.Path)
	if err != nil {
		f.markProcessedFalse(up.ID)
		return
	}
	defer os.Remove(tmpPath)

	// 2) 解码 + 压缩
	src, err := imaging.Open(tmpPath, imaging.AutoOrientation(true))
	if err != nil {
		// 解码失败（heic/avif 等无解码器）：保持原图，标记 processed=true 避免反复重试
		_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Update("processed", true).Error
		return
	}
	bounds := src.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()

	compressed := src
	if origW > s3CompressMaxWidth || origH > s3CompressMaxHeight {
		compressed = imaging.Fit(src, s3CompressMaxWidth, s3CompressMaxHeight, imaging.Lanczos)
	}

	// 水印（avatar 场景跳过）：画在压缩后的图上，缩略图随之继承
	watermarked := false
	if opt, need := f.watermarkOptions(up); need {
		if marked, werr := drawWatermark(compressed, opt); werr == nil {
			compressed = marked
			watermarked = true
		}
	}
	var mainBuf bytes.Buffer
	if err := imaging.Encode(&mainBuf, compressed, imaging.JPEG, imaging.JPEGQuality(s3CompressQuality)); err != nil {
		f.markProcessedFalse(up.ID)
		return
	}

	// 3) 回传压缩图（新 key，不覆盖原图）
	mainKey := derivedKey(up.Path, keyCompressedPrefix, "jpg")
	if err := f.putObject(ctx, mainKey, mainBuf.Bytes(), "image/jpeg"); err != nil {
		f.markProcessedFalse(up.ID)
		return
	}

	// 4) 生成并回传缩略图
	specs := f.cfg.Upload.ThumbnailSpecs
	if len(specs) == 0 {
		specs = []config.ThumbSpec{{Name: "thumb", Size: 200}}
	}
	thumbs := make([]model.UploadThumbnail, 0, len(specs))
	for _, spec := range specs {
		if spec.Size <= 0 {
			continue
		}
		dst := imaging.Fit(compressed, spec.Size, spec.Size, imaging.Lanczos)
		var buf bytes.Buffer
		if err := imaging.Encode(&buf, dst, imaging.JPEG, imaging.JPEGQuality(s3CompressQuality)); err != nil {
			continue
		}
		thumbKey := derivedKey(up.Path, keyThumbPrefix+spec.Name+"_", "jpg")
		if err := f.putObject(ctx, thumbKey, buf.Bytes(), "image/jpeg"); err != nil {
			continue
		}
		thumbs = append(thumbs, model.UploadThumbnail{
			Name:   spec.Name,
			URL:    f.s3AccessURL(thumbKey),
			Path:   thumbKey,
			Size:   int64(buf.Len()),
			Width:  dst.Bounds().Dx(),
			Height: dst.Bounds().Dy(),
		})
	}

	// 5) 更新记录：original 留原图，主字段指向压缩图
	original := model.UploadOriginal{
		URL: up.URL, Path: up.Path, Size: up.Size, Width: origW, Height: origH,
	}
	updates := map[string]interface{}{
		"url":         f.s3AccessURL(mainKey),
		"path":        mainKey,
		"filename":    path.Base(mainKey),
		"size":        int64(mainBuf.Len()),
		"mime_type":   "image/jpeg",
		"processed":   true,
		"watermarked": watermarked,
		"original":    mustJSON(original),
		"thumbnails":  mustJSON(thumbs),
	}
	if err := f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Updates(updates).Error; err != nil {
		f.markProcessedFalse(up.ID)
	}
}

// downloadObject 把对象下载到本地临时文件，返回路径。
func (f *Infra) downloadObject(ctx context.Context, key string) (string, error) {
	obj, err := f.client.GetObject(ctx, f.cfg.S3.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	tmp, err := os.CreateTemp("", "s3img-*"+filepath.Ext(key))
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, obj); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// putObject 上传字节到对象存储。
func (f *Infra) putObject(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := f.client.PutObject(ctx, f.cfg.S3.Bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("上传 %s 失败: %w", key, err)
	}
	return nil
}

// markProcessedFalse 处理失败时保持 processed=false（原图仍可用，等待重试）。
func (f *Infra) markProcessedFalse(id string) {
	if f.db == nil {
		return
	}
	_ = f.db.Model(&model.Upload{}).Where("id = ?", id).Update("processed", false).Error
}

// derivedKey 由原 key 生成派生 key：同目录 + 前缀 + 新扩展名。
//
//	uploads/2026/11/19/ab/x.jpg + ("c_", jpg) → uploads/2026/11/19/ab/c_x.jpg
func derivedKey(origKey, prefix, ext string) string {
	dir := path.Dir(origKey)
	base := strings.TrimSuffix(path.Base(origKey), path.Ext(origKey))
	name := prefix + base + "." + ext
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// mustJSON 序列化（失败返回空串，交由 DB 按 NULL 处理）。
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
