// 基础设施域：缩略图生成（disintegration/imaging）。
//
// 选型说明（第三方优先结论，落盘备查）：
//   - libvips 绑定（bimg / govips）性能最好，但需要 CGO + 系统库 libvips，
//     本工作区无该环境，编译即失败——排除；
//   - imaging（MIT，纯 Go）：覆盖 png/jpg/gif/tiff/bmp，webp 可经
//     golang.org/x/image/webp 解码，满足骨架内上传白名单全部可处理格式——采用；
//   - webp 编码 imaging 不支持，缩略图统一输出 JPEG（原实现 webp 输出是
//     sharp 的能力，此处为已知降级，写入响应的扩展名/类型同步为 jpg）。
package infra

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 注册 gif 解码
	_ "image/jpeg" // 注册 jpeg 解码
	_ "image/png"  // 注册 png 解码
	"os"
	"path/filepath"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // 注册 webp 解码（image/webp 无标准库解码器）
	// tiff / bmp 解码器由 imaging 包自身导入 golang.org/x/image/tiff|bmp 完成注册，
	// 这里无需重复导入（导入不存在于 x/image 之外的 image/tiff 会编译失败）。
)

// makeThumbnails 按规格生成等比缩略图（长边缩放到 spec.Size，不裁剪），
// 返回缩略图信息并写回 upload 记录的 thumbnails jsonb。
//
// 输出文件：与原图同目录，文件名 `<hash>-<spec>.jpg`。
func (f *Infra) makeThumbnails(srcPath string, specs []config.ThumbSpec) ([]model.UploadThumbnail, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("打开原图失败: %w", err)
	}
	defer file.Close()
	src, err := imaging.Decode(file)
	if err != nil {
		// 解码失败（含 heic/avif 等不支持格式）：静默放弃缩略图，不影响主流程
		return nil, nil
	}
	bounds := src.Bounds()

	thumbs := make([]model.UploadThumbnail, 0, len(specs))
	dir := filepath.Dir(srcPath)
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	for _, spec := range specs {
		if spec.Size <= 0 {
			continue
		}
		dst := imaging.Fit(src, spec.Size, spec.Size, imaging.Lanczos)
		name := fmt.Sprintf("%s-%s.jpg", base, spec.Name)
		outPath := filepath.Join(dir, name)
		if err := imaging.Save(dst, outPath, imaging.JPEGQuality(85)); err != nil {
			return nil, fmt.Errorf("保存缩略图失败: %w", err)
		}
		thumbs = append(thumbs, model.UploadThumbnail{
			Name:   spec.Name,
			URL:    f.localURL(outPath),
			Path:   outPath,
			Size:   fileSizeOf(outPath),
			Width:  dst.Bounds().Dx(),
			Height: dst.Bounds().Dy(),
		})
	}
	_ = bounds
	return thumbs, nil
}

// urlFor 缩略图访问 URL：cdn.base_url 优先，否则请求相对路径。
// fileSizeOf 文件字节数（不存在返回 0）。
func fileSizeOf(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// ensure 图片库可用性自检（编译期约束：imaging.Decode/Save 在链路上）。
var (
	_ = imaging.Fit
	_ = imaging.Decode
	_ = imaging.Save
	_ = image.Config{}
	_ = errors.New
)

// imageSize 读取图片宽高（解码头部，不解码整幅——imaging 的 DecodeConfig 语义）。
//
// 供 /upload/migration/metadata 补齐原图元数据使用；不支持的格式返回错误，
// 由调用方计入 failed 而不中断批次。
func imageSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("打开图片失败: %w", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, fmt.Errorf("解析图片尺寸失败: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}
