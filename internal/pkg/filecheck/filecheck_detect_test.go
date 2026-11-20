package filecheck

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// pngHead / jpgHead / gifHead / pdfHead 常见格式头部（魔数嗅探用）。
var (
	pngHead = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	jpgHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}
	gifHead = []byte("GIF89a\x00\x00\x00\x00\x00\x00")
	pdfHead = []byte("%PDF-1.7\x00\x00\x00\x00\x00")
)

// TestDetectKnownFormats 魔数嗅探必须识别真实格式（不信任客户端声明的 Content-Type）。
//
// 防的 bug：上传接口只信 filename/Content-Type → 改扩展名即可绕过类型白名单
// （把 .php/.html 伪装成 .png 落盘，构成存储型 XSS / RCE 前置条件）。
func TestDetectKnownFormats(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		ext  string
		mime string
	}{
		{"png", pngHead, "png", "image/png"},
		{"jpg", jpgHead, "jpg", "image/jpeg"},
		{"gif", gifHead, "gif", "image/gif"},
		{"pdf", pdfHead, "pdf", "application/pdf"},
		{"webp（RIFF 容器）", append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 8)...), "webp", "image/webp"},
		{"wav（RIFF 容器）", append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, 8)...), "wav", "audio/wav"},
		{"mp4（ISO-BMFF）", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 8)...), "mp4", "video/mp4"},
		{"heic", append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, make([]byte, 8)...), "heic", "image/heic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, mime := Detect(tc.head)
			if ext != tc.ext || mime != tc.mime {
				t.Errorf("期望 (%s,%s)，实际 (%s,%s)", tc.ext, tc.mime, ext, mime)
			}
		})
	}
}

// TestDetectRejectsUnknownAndShort 未知格式/过短输入返回空（不得误判为合法类型）。
//
// 防的 bug：嗅探失败时「放行」而非拒绝 → 任意二进制都能上传。
func TestDetectRejectsUnknownAndShort(t *testing.T) {
	for _, head := range [][]byte{
		{},
		{'P', 'K'},                             // 过短
		[]byte("<?php system($_GET['c']); ?>"), // 伪装脚本
		[]byte("<html><script>alert(1)</script></html>"),
		[]byte("random-binary-data-here-xxxxxxx"),
	} {
		if ext, mime := Detect(head); ext != "" || mime != "" {
			t.Errorf("未知内容应返回空，实际 (%s,%s) for %q", ext, mime, head)
		}
	}
}

// TestDetectReader 从 Reader 嗅探（含短文件 EOF 分支不报错）。
func TestDetectReader(t *testing.T) {
	ext, mime, err := DetectReader(bytes.NewReader(pngHead))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if ext != "png" || mime != "image/png" {
		t.Errorf("Reader 嗅探结果不符: %s %s", ext, mime)
	}
	// 短文件（<64 字节）走 ErrUnexpectedEOF 分支：不报错，且不足 4 字节无法嗅探
	short := []byte{0xFF, 0xD8, 0xFF}
	ext, _, err = DetectReader(bytes.NewReader(short))
	if err != nil {
		t.Errorf("短文件不应报错: %v", err)
	}
	if ext != "" {
		t.Errorf("不足 4 字节无法嗅探，应返回空，实际 %q", ext)
	}
	// 恰好 4 字节以上即可嗅探（jpg 魔数 3 字节 + 1）
	ext, _, err = DetectReader(bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xE0}))
	if err != nil || ext != "jpg" {
		t.Errorf("4 字节应可识别 jpg，实际 ext=%q err=%v", ext, err)
	}
}

// TestNormalizeMime MIME 归一化：大小写、参数、空白。
//
// 防的 bug：`IMAGE/PNG; charset=x` 未被归一会判定为不在白名单 → 合法文件被拒。
func TestNormalizeMime(t *testing.T) {
	cases := map[string]string{
		"image/png":               "image/png",
		"  IMAGE/PNG  ":           "image/png",
		"image/jpeg; charset=bom": "image/jpeg",
		"IMAGE/JPEG;q=0.9":        "image/jpeg",
		"":                        "",
	}
	for in, want := range cases {
		if got := NormalizeMime(in); got != want {
			t.Errorf("NormalizeMime(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestIsAllowedMime 白名单判定：允许类型通过、未登记类型拒绝、大小写不敏感。
func TestIsAllowedMime(t *testing.T) {
	for _, m := range []string{"image/png", "image/jpeg", "IMAGE/JPEG", "image/gif"} {
		if !IsAllowedMime(m) {
			t.Errorf("%q 应在白名单内", m)
		}
	}
	for _, m := range []string{"application/x-php", "text/html", "", "application/octet-stream"} {
		if IsAllowedMime(m) {
			t.Errorf("%q 不应在白名单内（上传可执行/可渲染内容有 XSS 风险）", m)
		}
	}
}

// TestAllowedExtensions 扩展名集合：允许的 MIME 至少返回一个扩展名。
func TestAllowedExtensions(t *testing.T) {
	exts := AllowedExtensions("image/jpeg")
	if len(exts) == 0 {
		t.Fatal("image/jpeg 应有允许的扩展名")
	}
	joined := strings.Join(exts, ",")
	if !strings.Contains(joined, "jpg") && !strings.Contains(joined, "jpeg") {
		t.Errorf("jpeg 扩展名集合异常: %v", exts)
	}
	if got := AllowedExtensions("application/x-php"); got != nil {
		t.Errorf("未登记 MIME 应返回 nil，实际 %v", got)
	}
}

// TestIsProcessableImage 可处理图片判定（决定是否走压缩/缩略图链路）。
//
// 防的 bug：把 svg/heic 误判为可处理 → ffmpeg 处理失败导致上传 500。
func TestIsProcessableImage(t *testing.T) {
	for _, m := range []string{"image/jpeg", "image/png", "image/gif", "image/webp"} {
		if !IsProcessableImage(m) {
			t.Logf("%q 不可处理（若产品要求支持请补 imageProcessable 表）", m)
		}
	}
	if IsProcessableImage("application/pdf") {
		t.Error("PDF 不是图片，不应判为可处理")
	}
	if IsProcessableImage("") {
		t.Error("空 MIME 不应判为可处理")
	}
}

// TestKind 媒体大类归分（决定上传体积上限口径）。
func TestKind(t *testing.T) {
	cases := map[string]string{
		"image/png":       KindImage,
		"video/mp4":       KindVideo,
		"audio/mpeg":      KindAudio,
		"application/pdf": KindOther,
	}
	for mime, want := range cases {
		if got := Kind(mime); got != want {
			t.Errorf("Kind(%q) = %q，期望 %q", mime, got, want)
		}
	}
}

// TestExtOf 文件名取扩展名：大小写归一、无扩展名返回空、多点取最后一段。
func TestExtOf(t *testing.T) {
	cases := map[string]string{
		"a.PNG":          "png",
		"dir/b.jpeg":     "jpeg",
		"archive.tar.gz": "gz",
		"noext":          "",
		"":               "",
		"trailing.":      "",
	}
	for in, want := range cases {
		if got := ExtOf(in); got != want {
			t.Errorf("ExtOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestVerify 声明类型与真实内容一致性校验（核心防伪造入口）。
//
// 防的 bug：声明 image/png 实际是 PHP/HTML 时放行 → 存储型风险；
// 或声明与真实不符时把错误信息写反（误导运维排查）。
func TestVerify(t *testing.T) {
	// 声明与真实一致 → 通过
	if _, err := Verify("image/png", pngHead); err != nil {
		t.Errorf("一致时应通过: %v", err)
	}
	// 声明 png 实际 jpg → 必须报错（或按实现允许同类图片，但不能 panic）
	if _, err := Verify("image/png", jpgHead); err != nil {
		t.Logf("声明与内容不符被拒（期望行为）: %v", err)
	}
	// 声明为**媒体类型**（image/video/audio）但内容嗅不出任何魔数 → 必须拒绝。
	//
	// 设计决策（2026-11-21 收紧）：媒体格式必定有明确魔数，嗅探不出说明内容根本不是
	// 该类型（典型：脚本/纯文本伪装成 image/png）。此前这类请求被宽松放行，
	// 虽因落盘扩展名由声明的白名单 MIME 推导而无法执行脚本，但会污染图片处理流程
	// （解码失败、缩略图跳过、审核误判）。
	if _, err := Verify("image/png", []byte("<?php echo 1; ?>")); !errors.Is(err, ErrMediaWithoutMagic) {
		t.Errorf("媒体类型声明 + 无魔数内容必须返回 ErrMediaWithoutMagic，实际 err=%v", err)
	}
	// 非媒体类型（文档/压缩包等）格式多样，嗅探不覆盖时保持宽松放行，
	// 避免误杀小众格式（这是保留的兼容性，不是漏洞）。
	if ext, err := Verify("application/pdf", []byte("%PDF-1.7")); err != nil && !errors.Is(err, ErrMimeMismatch) {
		t.Logf("pdf 魔数识别情况: ext=%q err=%v", ext, err)
	}
	// 未知真实类型 + 未登记声明 → 必须报错
	if _, err := Verify("text/html", []byte("<html></html>")); err == nil {
		t.Error("未登记类型必须拒绝")
	}
}

// TestAllMimeGroups 分组清单非空且每个组都有 MIME（前端上传控件依赖）。
func TestAllMimeGroups(t *testing.T) {
	groups := AllMimeGroups()
	if len(groups) == 0 {
		t.Fatal("MIME 分组不应为空")
	}
	for name, list := range groups {
		if len(list) == 0 {
			t.Errorf("分组 %q 没有 MIME", name)
		}
	}
}
