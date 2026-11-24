package i18n

import "testing"

// TestCommerceCatalogComplete 交易/装扮域补充 key 的完整性与取值抽查。
func TestCommerceCatalogComplete(t *testing.T) {
	// 全部 key 必须有英文文案（TestCatalogComplete 已做全局校验，这里抽查语义）
	cases := []struct {
		key  Key
		want string
	}{
		{PaymentAlipaySDKInitFailed, "Failed to initialize Alipay SDK: %s"},
		{PaymentRecordNotFound, "Payment record not found"},
		{OrderNotPending, "Order is not in pending status"},
		{ArticleAlreadyPurchased, "You have already purchased this article"},
		{MembershipLifetimeActive, "You are already a lifetime member"},
		{DecorationNameRequired, "Decoration name is required"},
		{DecorationActivityNotFound, "Activity not found"},
		{EmojiUploadNotAvailable, "Emoji upload persistence is handled by the upload service and is not integrated yet; upload via POST /upload first, then create the emoji with the returned URL"},
	}
	for _, c := range cases {
		if got := T(c.key); got != c.want {
			t.Errorf("T(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestCommerceWithKeyf 带参数的 key 在 errcode 侧格式化正确（配合 errcode 使用）。
func TestCommerceWithKeyf(t *testing.T) {
	got := Tf(PaymentAlipaySDKInitFailed, "invalid private key")
	if got != "Failed to initialize Alipay SDK: invalid private key" {
		t.Errorf("Tf = %q", got)
	}
}
