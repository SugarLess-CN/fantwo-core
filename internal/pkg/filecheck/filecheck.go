// Package filecheck 上传文件校验：MIME 白名单 + 扩展名映射 + 魔数（magic number）嗅探。
//
// 对齐原仓库 tmp/picart/src/common/utils/file-type.util.ts（file-type-checker 语义）
// 与 src/common/constants/media.constants.ts（唯一媒体类型清单）：
//   - 先按声明的 MIME 查出「允许的扩展名集合」，MIME 不在白名单直接拒绝；
//   - 再读文件头做魔数嗅探，得到真实扩展名；
//   - 真实扩展名不在允许集合内即判为伪造 MIME，拒绝。
//
// 不引入第三方 file-type 库的原因：本项目只需 20 余种格式的头部签名，
// 自带一张表即可覆盖且零依赖、可审计；生态库 file-type 仅支持部分容器格式，
// 对 heic/avif/7z 等仍需自行补表（见 docs/03 第三方优先结论的例外说明）。
package filecheck

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// 媒体大类（对齐原 resolveUploadSizeKind）。
const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"
	KindOther = "other"
)

// ErrUnsupportedMime 声明类型不在白名单。
var ErrUnsupportedMime = errors.New("不支持的文件类型")

// ErrMimeMismatch 魔数与声明类型不匹配。
var ErrMimeMismatch = errors.New("文件真实类型与声明不匹配")

// mimeExtensions MIME → 允许的扩展名（不区分大小写）。
// 对齐 media.constants 的 UPLOAD_* 清单，并补上同族的二进制压缩包别名。
var mimeExtensions = map[string][]string{
	// 图片
	"image/jpeg": {"jpg", "jpeg", "jpe"},
	"image/jpg":  {"jpg", "jpeg", "jpe"},
	"image/png":  {"png"},
	"image/gif":  {"gif"},
	"image/webp": {"webp"},
	"image/heic": {"heic", "heif"},
	"image/heif": {"heif", "heic"},
	"image/tiff": {"tif", "tiff"},
	"image/avif": {"avif"},
	"image/bmp":  {"bmp"},

	// 视频
	"video/mp4":        {"mp4", "m4v"},
	"video/webm":       {"webm"},
	"video/ogg":        {"ogv", "ogg"},
	"video/quicktime":  {"mov"},
	"video/x-msvideo":  {"avi"},
	"video/avi":        {"avi"},
	"video/x-matroska": {"mkv"},
	"video/mpeg":       {"mpeg", "mpg"},
	"video/flv":        {"flv"},
	"video/x-flv":      {"flv"},
	"video/x-m4v":      {"m4v", "mp4"},
	"video/3gpp":       {"3gp"},

	// 音频
	"audio/mpeg":  {"mp3", "mpeg"},
	"audio/wav":   {"wav"},
	"audio/x-wav": {"wav"},
	"audio/ogg":   {"ogg", "oga"},
	"audio/aac":   {"aac"},
	"audio/flac":  {"flac"},
	"audio/x-m4a": {"m4a"},
	"audio/mp4":   {"m4a", "mp4"},

	// 文档 / 压缩包
	"application/pdf":              {"pdf"},
	"application/zip":              {"zip"},
	"application/x-zip-compressed": {"zip"},
	"application/vnd.rar":          {"rar"},
	"application/x-rar-compressed": {"rar"},
	"application/x-7z-compressed":  {"7z"},
}

// presignExtraBlock 预签名直传额外放行（原 PRESIGN_ALLOWED_MIME_TYPES 语义：
// 是网关白名单的子集，此处不额外放行，仅保留函数名说明差异）。
var presignExtraBlock = map[string]bool{}

// imageProcessable 可被图像库处理（解码/压缩/缩略图）的 MIME。
//
// 与原 IMAGE_PROCESSABLE_MIME_TYPES 的差异：heic/heif/avif 需要额外解码器，
// Go 标准库与 imaging 都不支持，这里剔除；tiff 由 imaging 支持保留。
var imageProcessable = map[string]bool{
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true, // 仅解码（golang.org/x/image/webp），编码走 JPEG
	"image/tiff": true,
	"image/bmp":  true,
}

// signature 魔数签名。
type signature struct {
	offset int
	magic  []byte
	ext    string
	mime   string
}

// signatures 魔数表（顺序即匹配优先级：越具体的放前面）。
var signatures = []signature{
	{0, []byte{0xFF, 0xD8, 0xFF}, "jpg", "image/jpeg"},
	{0, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "png", "image/png"},
	{0, []byte("GIF87a"), "gif", "image/gif"},
	{0, []byte("GIF89a"), "gif", "image/gif"},
	{0, []byte("II*\x00"), "tif", "image/tiff"},
	{0, []byte("MM\x00*"), "tif", "image/tiff"},
	{0, []byte("BM"), "bmp", "image/bmp"},
	{0, []byte("%PDF"), "pdf", "application/pdf"},
	{0, []byte("PK\x03\x04"), "zip", "application/zip"},
	{0, []byte("PK\x05\x06"), "zip", "application/zip"},
	{0, []byte("PK\x07\x08"), "zip", "application/zip"},
	{0, []byte("Rar!\x1A\x07"), "rar", "application/vnd.rar"},
	{0, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}, "7z", "application/x-7z-compressed"},
	{0, []byte{0x1A, 0x45, 0xDF, 0xA3}, "webm", "video/webm"}, // matroska 族，扩展名再由 ftyp 细分
	{0, []byte("OggS"), "ogg", "audio/ogg"},
	{0, []byte("fLaC"), "flac", "audio/flac"},
	{0, []byte("ID3"), "mp3", "audio/mpeg"},
	{0, []byte("FLV"), "flv", "video/flv"},
	{0, []byte{0x00, 0x00, 0x01, 0xBA}, "mpeg", "video/mpeg"},
	{0, []byte{0x00, 0x00, 0x01, 0xB3}, "mpeg", "video/mpeg"},
	{0, []byte{0xFF, 0xFB}, "mp3", "audio/mpeg"},
	{0, []byte{0xFF, 0xF3}, "mp3", "audio/mpeg"},
	{0, []byte{0xFF, 0xF1}, "aac", "audio/aac"},
	{0, []byte{0xFF, 0xF9}, "aac", "audio/aac"},
}

// RIFF 容器（offset 0 = "RIFF"，offset 8 = 子类型）单独处理。
const riffPrefix = "RIFF"

// maxProbe 魔数嗅探需要读的头部字节数（ftyp 品牌在 offset 8..32，rar5/7z 在 6 字节内）。
const maxProbe = 64

// Detect 从头部字节嗅探真实格式，返回 (扩展名, MIME)。无法识别时返回空串。
//
// 覆盖 RIFF（webp/avi/wav）、ISO-BMFF（mp4/mov/m4a/heic/avif/3gp）两类容器。
func Detect(head []byte) (ext, mime string) {
	if len(head) < 4 {
		return "", ""
	}

	// RIFF 容器
	if len(head) >= 12 && string(head[0:4]) == riffPrefix {
		sub := string(head[8:12])
		switch sub {
		case "WEBP":
			return "webp", "image/webp"
		case "AVI ":
			return "avi", "video/x-msvideo"
		case "WAVE":
			return "wav", "audio/wav"
		}
	}

	// ISO-BMFF：offset 4 = "ftyp"，offset 8 = 主品牌
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		brand := strings.TrimSpace(string(head[8:12]))
		switch brand {
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1":
			return "heic", "image/heic"
		case "avif", "avis":
			return "avif", "image/avif"
		case "qt  ":
			return "mov", "video/quicktime"
		case "3gp4", "3gp5", "3gp6", "3gpp":
			return "3gp", "video/3gpp"
		case "M4A ", "M4A\x00", "m4a ":
			return "m4a", "audio/x-m4a"
		case "isom", "mp41", "mp42", "avc1", "dash", "iso2", "mmp4":
			return "mp4", "video/mp4"
		default:
			return "mp4", "video/mp4"
		}
	}

	for _, sig := range signatures {
		if len(head) >= sig.offset+len(sig.magic) &&
			bytes.Equal(head[sig.offset:sig.offset+len(sig.magic)], sig.magic) {
			return sig.ext, sig.mime
		}
	}
	return "", ""
}

// DetectReader 从 io.Reader 读最大 64 字节做嗅探（不消耗调用方对后续内容的读取时请用 DetectHead）。
func DetectReader(r io.Reader) (ext, mime string, err error) {
	head := make([]byte, maxProbe)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", "", err
	}
	ext, mime = Detect(head[:n])
	return ext, mime, nil
}

// NormalizeMime 归一化声明的 MIME：小写、去参数（; charset=...）、去空白。
func NormalizeMime(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return m
}

// AllowedExtensions 返回该 MIME 允许的扩展名集合；MIME 不在白名单返回 nil。
func AllowedExtensions(mime string) []string {
	return mimeExtensions[NormalizeMime(mime)]
}

// IsAllowedMime 声明类型是否在白名单内。
func IsAllowedMime(mime string) bool { return len(AllowedExtensions(mime)) > 0 }

// IsAllowedPresignMime 预签名直传是否放行该类型（与网关同口径，见 presignExtraBlock 说明）。
func IsAllowedPresignMime(mime string) bool {
	m := NormalizeMime(mime)
	if presignExtraBlock[m] {
		return false
	}
	return IsAllowedMime(m)
}

// IsProcessableImage 是否可做压缩/缩略图处理。
func IsProcessableImage(mime string) bool { return imageProcessable[NormalizeMime(mime)] }

// Kind 媒体大类（对齐原 resolveUploadSizeKind）。
func Kind(mime string) string {
	m := NormalizeMime(mime)
	switch {
	case strings.HasPrefix(m, "image/"):
		return KindImage
	case strings.HasPrefix(m, "video/"):
		return KindVideo
	case strings.HasPrefix(m, "audio/"):
		return KindAudio
	default:
		return KindOther
	}
}

// ExtOf 取文件名扩展名（小写，不含点）。
func ExtOf(filename string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
}

// Verify 按「声明 MIME 白名单 → 魔数嗅探 → 扩展名比对」三步校验。
//
// head 为文件头（最多 64 字节）；无法嗅探出格式时按原实现的宽松策略放行
// （原 FileTypeUtil 嗅探失败只 warn 不拒绝），避免小众格式被误杀。
func Verify(declaredMime string, head []byte) (ext string, err error) {
	declared := NormalizeMime(declaredMime)
	allowed := AllowedExtensions(declared)
	if len(allowed) == 0 {
		return "", ErrUnsupportedMime
	}

	detectedExt, _ := Detect(head)
	if detectedExt == "" {
		return "", nil // 嗅探不出：放行（对齐原实现）
	}
	for _, e := range allowed {
		if strings.EqualFold(e, detectedExt) {
			return detectedExt, nil
		}
	}
	return detectedExt, ErrMimeMismatch
}

// AllMimeGroups 返回白名单分组（供 /upload/config 端点返回 allowedMimeTypes）。
func AllMimeGroups() map[string][]string {
	out := map[string][]string{
		KindImage: {}, KindVideo: {}, KindAudio: {}, KindOther: {},
	}
	seen := map[string]map[string]bool{}
	for mime := range mimeExtensions {
		k := Kind(mime)
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		if seen[k][mime] {
			continue
		}
		seen[k][mime] = true
		out[k] = append(out[k], mime)
	}
	return out
}
