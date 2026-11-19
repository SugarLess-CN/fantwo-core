// 基础设施域：图片文字水印绘制。
//
// 对齐原仓库 tmp/picart/src/modules/upload/watermark.service.ts：
//   - 上传场景为 avatar 时不加水印（skipScenes）；
//   - 水印文字来自模板（默认 `{nickname}`），取上传者昵称；
//   - 位置 / 透明度 / 字号可配，默认右下角、0.55 透明度、字号按图片短边自适应；
//   - 文字先画一层半透明黑色投影再画主体，保证在亮/暗背景上都可读。
//
// 字体：Noto Sans SC 子集（见 assets/LICENSE-NotoSansSC.md，OFL-1.1），
// 编译期 go:embed 进二进制；覆盖 ASCII + 中文标点 + GB2312 全部 6763 汉字。
//
// 实现：golang.org/x/image/font/opentype（v0.48.0 起提供完整字形光栅化）+
// font.Drawer 直接绘制到目标图层，无需额外依赖。
package infra

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/disintegration/imaging"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/watermark.otf
var watermarkFontBytes []byte

// 水印默认参数（config 表可覆盖，见 model.DefaultConfigSeeds 的 upload_watermark_*）。
const (
	wmDefaultPosition = "bottom-right"
	wmDefaultOpacity  = 0.55
	wmDefaultTemplate = "{nickname}"
	// wmAutoFontSizeDivisor 自适应字号：短边 / 该除数（下限 wmMinFontSize）
	wmAutoFontSizeDivisor = 30
	wmMinFontSize         = 16
	wmMaxFontSize         = 96
	wmDefaultMargin       = 0 // 0 = 按字号推算（字号/2）
)

// fontCache 解析后的字体（进程内单例；opentype.Font 不可变，可并发复用）。
var (
	watermarkFont     *opentype.Font
	watermarkFontErr  error
	watermarkFontOnce bool
)

// loadWatermarkFont 懒解析嵌入字体（失败缓存错误，避免反复重试）。
func loadWatermarkFont() (*opentype.Font, error) {
	if watermarkFontOnce {
		return watermarkFont, watermarkFontErr
	}
	f, err := opentype.Parse(watermarkFontBytes)
	watermarkFont, watermarkFontErr, watermarkFontOnce = f, err, true
	if err != nil {
		return nil, fmt.Errorf("解析水印字体失败: %w", err)
	}
	return f, nil
}

// WatermarkOptions 单次绘制参数。
type WatermarkOptions struct {
	Text     string  // 最终文字（模板已替换）
	Position string  // bottom-right / bottom-left / top-right / top-left / center
	Opacity  float64 // 0..1
	FontSize int     // 0 = 按图片短边自适应
	Margin   int     // 0 = 按字号推算
}

// drawWatermark 返回叠加水印后的新图；src 不被修改。
func drawWatermark(src image.Image, opt WatermarkOptions) (image.Image, error) {
	text := strings.TrimSpace(opt.Text)
	if text == "" {
		return src, nil
	}
	f, err := loadWatermarkFont()
	if err != nil {
		return src, err
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return src, fmt.Errorf("图片尺寸非法: %dx%d", w, h)
	}

	size := opt.FontSize
	if size <= 0 {
		size = minInt(w, h) / wmAutoFontSizeDivisor
	}
	size = clampInt(size, wmMinFontSize, wmMaxFontSize)

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return src, fmt.Errorf("创建水印字体面失败: %w", err)
	}
	defer face.Close()

	margin := opt.Margin
	if margin <= 0 {
		margin = size / 2
	}

	textWidth := font.MeasureString(face, text).Ceil()
	metrics := face.Metrics()
	textHeight := (metrics.Ascent + metrics.Descent).Ceil()

	x, y := wmAnchor(opt.Position, w, h, textWidth, textHeight, margin)
	// y 是文字基线：锚点给的是文字块左上角，需加 ascent
	baselineY := y + metrics.Ascent.Ceil()

	opacity := opt.Opacity
	if opacity <= 0 || opacity > 1 {
		opacity = wmDefaultOpacity
	}
	alpha := uint8(255 * opacity)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)

	// 投影：偏移 1px 的黑色半透明，提升复杂背景可读性
	shadow := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.RGBA{R: 0, G: 0, B: 0, A: uint8(float64(alpha) * 0.6)}),
		Face: face,
		Dot:  fixed.P(x+1, baselineY+1),
	}
	shadow.DrawString(text)

	main := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(color.RGBA{R: 255, G: 255, B: 255, A: alpha}),
		Face: face,
		Dot:  fixed.P(x, baselineY),
	}
	main.DrawString(text)

	return dst, nil
}

// wmAnchor 按位置算文字块左上角坐标（超出边界时贴边收敛）。
func wmAnchor(position string, w, h, textW, textH, margin int) (int, int) {
	var x, y int
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "bottom-left":
		x, y = margin, h-margin-textH
	case "top-right":
		x, y = w-margin-textW, margin
	case "top-left":
		x, y = margin, margin
	case "center":
		x, y = (w-textW)/2, (h-textH)/2
	default: // bottom-right
		x, y = w-margin-textW, h-margin-textH
	}
	return clampInt(x, 0, maxInt(0, w-textW)), clampInt(y, 0, maxInt(0, h-textH))
}

// watermarkOptions 根据 config 与上传记录决定是否加水印、加什么字。
//
// 返回 (参数, 是否需要水印)。任一条件不满足即跳过：
//   - 场景为 avatar（原 skipScenes）
//   - 全局开关关闭
//   - 模板替换后为空
func (f *Infra) watermarkOptions(up *model.Upload) (WatermarkOptions, bool) {
	if up == nil {
		return WatermarkOptions{}, false
	}
	if up.Scene == SceneAvatar {
		return WatermarkOptions{}, false
	}
	enabled := f.cfg.Upload.WatermarkEnabled
	position := wmDefaultPosition
	opacity := wmDefaultOpacity
	template := wmDefaultTemplate
	fontSize := 0
	if f.cfgSvc != nil {
		enabled = f.cfgSvc.GetBool("upload_watermark_enabled", enabled)
		position = f.cfgSvc.GetString("upload_watermark_position", position)
		template = f.cfgSvc.GetString("upload_watermark_text_template", template)
		if v := f.cfgSvc.GetInt("upload_watermark_opacity_percent", 0); v > 0 {
			opacity = float64(v) / 100
		}
		fontSize = f.cfgSvc.GetInt("upload_watermark_font_size", 0)
	}
	if !enabled {
		return WatermarkOptions{}, false
	}

	userID := ""
	if up.UserID != nil {
		userID = *up.UserID
	}
	text := f.resolveWatermarkText(template, userID)
	if text == "" {
		return WatermarkOptions{}, false
	}
	return WatermarkOptions{
		Text:     text,
		Position: position,
		Opacity:  opacity,
		FontSize: fontSize,
	}, true
}

// resolveWatermarkText 用上传者昵称替换模板占位符。
//
// 占位符：{nickname}（上传者昵称）、{username}（用户名）、{site}（站点名）。
// 昵称缺失时依次回退 username → 站点名 → 空（空则不加水印）。
func (f *Infra) resolveWatermarkText(template, userID string) string {
	nickname, username := f.lookupUserName(userID)
	site := "PicArt"
	if f.cfgSvc != nil {
		site = f.cfgSvc.GetString("site_name", site)
	}
	text := template
	text = strings.ReplaceAll(text, "{nickname}", nickname)
	text = strings.ReplaceAll(text, "{username}", username)
	text = strings.ReplaceAll(text, "{site}", site)
	text = strings.TrimSpace(text)
	if text == "" || text == template {
		// 模板没有占位符或替换后为空：按回退链给默认值
		switch {
		case strings.TrimSpace(nickname) != "":
			text = nickname
		case strings.TrimSpace(username) != "":
			text = username
		default:
			text = strings.TrimSpace(site)
		}
	}
	return text
}

// lookupUserName 查上传者的昵称与用户名（查不到返回空串）。
func (f *Infra) lookupUserName(userID string) (nickname, username string) {
	if f.db == nil || strings.TrimSpace(userID) == "" {
		return "", ""
	}
	var row struct {
		Nickname *string
		Username string
	}
	// 用户表名由 auth-user 域定义（TableName() = "user"，user 是 PG 保留字）
	if err := f.db.Table(model.User{}.TableName()).
		Select("nickname, username").
		Where("id = ?", userID).
		Scan(&row).Error; err != nil {
		return "", ""
	}
	if row.Nickname != nil {
		nickname = *row.Nickname
	}
	return nickname, row.Username
}

// applyWatermarkToFile 对本地文件原地加水印：先备份无水印原图，再覆盖写入。
//
// 备份文件名 `<base>.original<ext>`；返回备份路径（供 original 元数据引用）。
// 解码失败（heic/avif 无解码器）时原样返回，不视为错误。
func (f *Infra) applyWatermarkToFile(path string, opt WatermarkOptions) (backupPath string, err error) {
	src, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}
	marked, err := drawWatermark(src, opt)
	if err != nil {
		return "", err
	}

	// 备份原图（仅首次；重复处理时不覆盖已有备份）
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	backupPath = base + ".original" + ext
	if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
		if copyErr := copyFile(path, backupPath); copyErr != nil {
			return "", fmt.Errorf("备份原图失败: %w", copyErr)
		}
	}

	// 覆盖主文件为带水印版本（按原格式编码，保持 mime 不变）
	if err := imaging.Save(marked, path); err != nil {
		return backupPath, fmt.Errorf("写入水印图失败: %w", err)
	}
	return backupPath, nil
}

// copyFile 复制文件（备份原图用）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}

// minInt / maxInt / clampInt 小工具（避免引入泛型与额外依赖）。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
