package i18n

import "testing"

// TestCatalogComplete 每个 key 常量都必须有英文文案。
// 防的 bug：加了 Key 常量忘了加文案 → 线上返回 "user.not_found" 这种裸 key。
func TestCatalogComplete(t *testing.T) {
	for _, k := range Keys() {
		if _, ok := enCatalog[k]; !ok {
			t.Errorf("key %q 在 enCatalog 里没有文案", k)
		}
	}
}

// TestNoEmptyText 文案不能是空串。
func TestNoEmptyText(t *testing.T) {
	for k, v := range enCatalog {
		if v == "" {
			t.Errorf("key %q 的英文文案是空串", k)
		}
	}
}

// TestNoChinese 所有文案必须是英文（不得残留中文）。
// 这是本次重构的硬要求：响应 message 全英文。
func TestNoChinese(t *testing.T) {
	for k, v := range enCatalog {
		for _, r := range v {
			if r >= 0x4e00 && r <= 0x9fff {
				t.Errorf("key %q 的文案含中文：%q", k, v)
				break
			}
		}
	}
}

// TestKnownKeysResolve 抽查若干关键 key 的实际取值，锁定对外文案。
func TestKnownKeysResolve(t *testing.T) {
	cases := []struct {
		key  Key
		want string
	}{
		{CommonOK, "Success"},
		{CommonInternal, "Internal server error"},
		{AuthBadCredentials, "Incorrect username or password"},
		{UploadNotFound, "Upload not found"},
	}
	for _, c := range cases {
		if got := T(c.key); got != c.want {
			t.Errorf("T(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestTf 带参数文案的格式化。
func TestTf(t *testing.T) {
	got := Tf(CommonAlreadyExists, "user")
	if got != "user already exists" {
		t.Errorf("Tf = %q, want %q", got, "user already exists")
	}
	// 无参数时不应被 Sprintf 误解析（文案里可能有 %）
	if got := Tf(CommonOK); got != "Success" {
		t.Errorf("Tf 无参数 = %q, want Success", got)
	}
}

// TestUnknownKeyReturnsKey 漏配 key 时返回 key 本身，便于定位。
func TestUnknownKeyReturnsKey(t *testing.T) {
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("未知 key 应原样返回，得到 %q", got)
	}
}

// TestParseLocale 语言协商：只支持 en，其他一律回退。
func TestParseLocale(t *testing.T) {
	cases := []struct {
		in   string
		want Locale
	}{
		{"", LocaleEN},
		{"en", LocaleEN},
		{"en-US", LocaleEN},
		{"en-US,en;q=0.9", LocaleEN},
		{"zh-CN", LocaleEN}, // 未支持，回退默认
		{"ja", LocaleEN},
	}
	for _, c := range cases {
		if got := ParseLocale(c.in); got != c.want {
			t.Errorf("ParseLocale(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
