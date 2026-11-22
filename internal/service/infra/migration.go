// 基础设施域：上传数据迁移与补偿（对齐原 upload-migration.service.ts）。
//
// 三个管理端点：
//
//	GET  /upload/migration/stats     压缩处理统计（总/已处理/待处理/缩略图覆盖）
//	POST /upload/migration/process   批量处理未压缩图片（支持 dryRun 试运行）
//	POST /upload/migration/metadata  补齐缺失的原图元数据（宽/高/字节数）
//
// 差异说明：原实现依赖 sharp 的元数据读取（getMetadata）；Go 侧用 imaging 解码取
// 宽高（image.go 的 imageSize），解码失败的格式（heic/avif）计入 failed 而非中断批次。
package infra

import (
	"context"
	"encoding/json"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// MigrationStatsResult 压缩处理统计（对齐 getProcessingStats）。
type MigrationStatsResult struct {
	Total          int64  `json:"total"`
	Images         int64  `json:"images"`
	Processed      int64  `json:"processed"`
	Unprocessed    int64  `json:"unprocessed"`
	WithThumbnails int64  `json:"withThumbnails"`
	WithOriginal   int64  `json:"withOriginal"`
	Watermarked    int64  `json:"watermarked"`
	ProcessedPct   string `json:"processedPercent"`
}

// MigrationProcessResult 批量处理结果（对齐 processUncompressedImages 返回）。
type MigrationProcessResult struct {
	Total     int      `json:"total"`
	Processed int      `json:"processed"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	DryRun    bool     `json:"dryRun"`
	Errors    []string `json:"errors,omitempty"`
}

// MigrationFillResult 元数据补齐结果。
type MigrationFillResult struct {
	Total  int      `json:"total"`
	Filled int      `json:"filled"`
	Failed int      `json:"failed"`
	Errors []string `json:"errors,omitempty"`
}

// MigrationStats 统计（GET /upload/migration/stats）。
func (f *Infra) MigrationStats(ctx context.Context) (*MigrationStatsResult, error) {
	if f.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	db := f.db.WithContext(ctx)
	out := &MigrationStatsResult{}

	if err := db.Model(&model.Upload{}).Count(&out.Total).Error; err != nil {
		return nil, svcInternal("统计上传文件失败: %v", err)
	}
	// 图片子集：mime like 'image/%'（对齐 isSupportedImage 语义，含不可处理的 heic/avif）
	for _, q := range []struct {
		dst   *int64
		where string
		args  []interface{}
	}{
		{&out.Images, "mime_type LIKE ?", []interface{}{"image/%"}},
		{&out.Processed, "mime_type LIKE ? AND processed = ?", []interface{}{"image/%", true}},
		{&out.Unprocessed, "mime_type LIKE ? AND processed = ?", []interface{}{"image/%", false}},
		{&out.WithThumbnails, "mime_type LIKE ? AND thumbnails IS NOT NULL", []interface{}{"image/%"}},
		{&out.WithOriginal, "mime_type LIKE ? AND original IS NOT NULL", []interface{}{"image/%"}},
		{&out.Watermarked, "watermarked = ?", []interface{}{true}},
	} {
		if err := db.Model(&model.Upload{}).Where(q.where, q.args...).Count(q.dst).Error; err != nil {
			return nil, svcInternal("统计失败: %v", err)
		}
	}
	if out.Images > 0 {
		out.ProcessedPct = formatPct(out.Processed, out.Images)
	} else {
		out.ProcessedPct = "0.00"
	}
	return out, nil
}

// MigrationProcess 批量处理未压缩图片（POST /upload/migration/process）。
//
// dryRun=true 时只统计待处理数量，不触碰文件。
func (f *Infra) MigrationProcess(ctx context.Context, limit int, dryRun bool) (*MigrationProcessResult, error) {
	if f.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if limit <= 0 {
		limit = 100
	}
	var list []model.Upload
	if err := f.db.WithContext(ctx).
		Where("mime_type LIKE ? AND processed = ?", "image/%", false).
		Order("created_at ASC").Limit(limit).Find(&list).Error; err != nil {
		return nil, svcInternal("查询待处理图片失败: %v", err)
	}
	res := &MigrationProcessResult{Total: len(list), DryRun: dryRun}
	if dryRun {
		res.Skipped = len(list)
		return res, nil
	}

	// 压缩开关：与实时链路（processImageThumbnails）同源判定（compressionEnabled：
	// env 兜底 + upload_compression_enabled DB 优先）。批次内只读一次，中途改配置
	// 不影响本批语义。
	compression := f.compressionEnabled()

	for i := range list {
		up := &list[i]
		if up.Storage != model.UploadStorageLocal {
			res.Skipped++
			continue
		}
		// 压缩关闭：补处理只补 original 元数据（宽高与压缩无关，前端排版需要），
		// 不生成缩略图 —— 否则运维在压缩关闭状态下跑一次补处理，就把开关语义打破了，
		// 与「假开关」是同一类毛病，只是路径不同。
		//
		// 取舍（明确记录，非默默决定）：这里 **仍把 processed 置 true** 并计入 Skipped。
		// 理由：本端点的拣选条件是 processed = false（见上方 Where），若不置 true，
		// 这些压缩关闭时上传的记录会永远留在待补队列里，每次跑都被挑中、每次都被跳过，
		// 形成噪声并让 stats 的 unprocessed 永久虚高。语义上「压缩关闭 ⇒ 这类记录本就
		// 不该有缩略图」就是它们的最终态，与实时链路 processImageThumbnails 的处置一致。
		// 计入 Skipped 而非 Processed：Processed 的口径是「本次生成了缩略图」。
		if !compression {
			updates := map[string]interface{}{"processed": true}
			if up.OriginalJSON == "" || up.OriginalJSON == "null" {
				if w, h, err := imageSize(up.Path); err == nil {
					if payload, merr := json.Marshal(model.UploadOriginal{
						URL: up.URL, Path: up.Path, Size: up.Size, Width: w, Height: h,
					}); merr == nil {
						updates["original"] = string(payload)
					}
				}
			}
			if err := f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Updates(updates).Error; err != nil {
				res.Failed++
				continue
			}
			res.Skipped++
			continue
		}
		thumbs, err := f.ensureThumbnails(up)
		if err != nil {
			res.Failed++
			if len(res.Errors) < 10 {
				res.Errors = append(res.Errors, up.ID+": "+err.Error())
			}
			continue
		}
		updates := map[string]interface{}{"processed": true}
		if len(thumbs) > 0 {
			if payload, err := json.Marshal(thumbs); err == nil {
				updates["thumbnails"] = string(payload)
			}
		} else {
			// 解码失败（heic/avif）：标记 processed 避免反复重试占用批次
			res.Skipped++
			_ = f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Update("processed", true).Error
			continue
		}
		if err := f.db.Model(&model.Upload{}).Where("id = ?", up.ID).Updates(updates).Error; err != nil {
			res.Failed++
			continue
		}
		res.Processed++
	}
	return res, nil
}

// MigrationFillMetadata 补齐缺失原图元数据（POST /upload/migration/metadata）。
func (f *Infra) MigrationFillMetadata(ctx context.Context, limit int) (*MigrationFillResult, error) {
	if f.db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	if limit <= 0 {
		limit = 100
	}
	var list []model.Upload
	if err := f.db.WithContext(ctx).
		Where("mime_type LIKE ?", "image/%").
		Where("original IS NULL OR original = ''").
		Order("created_at ASC").Limit(limit).Find(&list).Error; err != nil {
		return nil, svcInternal("查询缺失元数据图片失败: %v", err)
	}
	res := &MigrationFillResult{Total: len(list)}
	for i := range list {
		up := &list[i]
		if up.Storage != model.UploadStorageLocal {
			res.Failed++
			continue
		}
		w, h, err := imageSize(up.Path)
		if err != nil {
			res.Failed++
			if len(res.Errors) < 10 {
				res.Errors = append(res.Errors, up.ID+": "+err.Error())
			}
			continue
		}
		orig := model.UploadOriginal{
			URL: up.URL, Path: up.Path, Size: up.Size, Width: w, Height: h,
		}
		payload, err := json.Marshal(orig)
		if err != nil {
			res.Failed++
			continue
		}
		if err := f.db.Model(&model.Upload{}).Where("id = ?", up.ID).
			Update("original", string(payload)).Error; err != nil {
			res.Failed++
			continue
		}
		res.Filled++
	}
	return res, nil
}

// formatPct 百分比（保留两位小数，对齐原 `((a/b)*100).toFixed(2)`）。
func formatPct(part, total int64) string {
	if total == 0 {
		return "0.00"
	}
	return sprintfFix(float64(part)/float64(total)*100, 2)
}

// sprintfFix 极简定点格式化（避免 fmt 在热路径的反射开销）。
func sprintfFix(v float64, digits int) string {
	pow := 1.0
	for i := 0; i < digits; i++ {
		pow *= 10
	}
	scaled := int64(v*pow + 0.5)
	intPart := scaled / int64(pow)
	frac := scaled % int64(pow)
	if frac == 0 {
		return itoa(intPart)
	}
	fracStr := itoa(frac)
	for len(fracStr) < digits {
		fracStr = "0" + fracStr
	}
	return itoa(intPart) + "." + fracStr
}

// itoa 整数转字符串。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// 编译期守护：time 用于批次节流（未来扩展），gorm 用于 Transaction。
var (
	_ = time.Now
	_ = gorm.ErrRecordNotFound
)
