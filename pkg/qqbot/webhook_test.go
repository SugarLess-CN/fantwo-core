package qqbot

// webhook.go 的单测：官方签名锚点 + 三态解析 + 验签 fail-closed。

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// 官方文档给定的标准答案 —— 能对上就证明签名算法（seed 填充 + 字段顺序）完全正确。
//
// 来源：docs/29 §5.2 / §5.3 的官方示例（appid=11111111 secret=DG5g3B4j9X2KOErG）。
const (
	officialAppID      = "11111111"
	officialBotSecret  = "DG5g3B4j9X2KOErG"
	officialPlainToken = "Arq0D5A61EgUu4OxUvOp"
	officialEventTs    = "1725442341"
	officialSignature  = "87befc99c42c651b3aac0278e71ada338433ae26fcb24307bdc5ad38c1adc2d01bcfcadc0842edac85e85205028a1132afe09280305f13aa6909ffc2d652c706"
)

func TestSignValidationOfficialAnchor(t *testing.T) {
	sig, err := SignValidation(officialBotSecret, officialEventTs, officialPlainToken)
	if err != nil {
		t.Fatalf("SignValidation: %v", err)
	}
	if sig != officialSignature {
		t.Fatalf("签名与官方标准答案不符：\n got=%s\nwant=%s", sig, officialSignature)
	}
	t.Logf("官方锚点逐字匹配：%s", sig)

	// 反面：字段顺序写反必然得到不同的签名（这条防止「顺序无所谓」的错误实现）
	var wrong bytes.Buffer
	wrong.WriteString(officialPlainToken) // 反序
	wrong.WriteString(officialEventTs)
	priv, err := derivePrivateKey(officialBotSecret)
	if err != nil {
		t.Fatalf("derivePrivateKey: %v", err)
	}
	if got := signHex(priv, wrong.Bytes()); got == officialSignature {
		t.Error("event_ts / plain_token 顺序反了仍得到官方签名 —— 说明「顺序有讲究」没有被验证")
	}
}

// seed 填充规则：短 secret 循环填到 32 字节，长 secret 截到 32 字节。
func TestDerivePrivateKeySeedPadding(t *testing.T) {
	// 16 字节 secret → 重复两次正好 32
	short := "DG5g3B4j9X2KOErG"
	privShort, err := derivePrivateKey(short)
	if err != nil {
		t.Fatalf("derivePrivateKey(short): %v", err)
	}

	// 直接把 16 字节重复两次得到的等价 secret（32 字节）应派生出**同一把**密钥
	privExpanded, err := derivePrivateKey("DG5g3B4j9X2KOErGDG5g3B4j9X2KOErG")
	if err != nil {
		t.Fatalf("derivePrivateKey(expanded): %v", err)
	}
	if !privShort.Equal(privExpanded) {
		t.Error("16 字节 secret 未按「循环填充到 32」派生（与直接给 32 字节的结果不同）")
	}

	// 超过 32 字节 → 截断到前 32
	long := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 39 字节
	privLong, err := derivePrivateKey(long)
	if err != nil {
		t.Fatalf("derivePrivateKey(long): %v", err)
	}
	priv32, err := derivePrivateKey(long[:32])
	if err != nil {
		t.Fatalf("derivePrivateKey(32): %v", err)
	}
	if !privLong.Equal(priv32) {
		t.Error("超长 secret 未截断到 32 字节")
	}

	// 空 secret → ErrNotConfigured
	if _, err := derivePrivateKey("  "); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("空 secret err = %v, want ErrNotConfigured", err)
	}
}

// op=13 地址验证：三态里的 (payload, validation, nil)，且不要求签名头。
func TestParseWebhookValidation(t *testing.T) {
	c := newTestClientForWebhook(t)

	body := []byte(`{"d":{"plain_token":"` + officialPlainToken + `","event_ts":"` + officialEventTs + `"},"op":13}`)
	payload, validation, err := c.ParseWebhook(body, http.Header{}) // 关键：没有签名头
	if err != nil {
		t.Fatalf("op=13 不该要求签名头：%v", err)
	}
	if payload == nil || payload.Op != OpValidation {
		t.Fatalf("payload 解析错误：%+v", payload)
	}
	if validation == nil {
		t.Fatal("validation 不应为 nil")
	}
	if validation.PlainToken != officialPlainToken {
		t.Errorf("PlainToken = %q", validation.PlainToken)
	}
	if validation.Signature != officialSignature {
		t.Errorf("Signature = %q, want 官方锚点", validation.Signature)
	}

	// 回写体必须只有这两个字段（平台按此结构校验）
	raw, err := json.Marshal(validation)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"plain_token":"` + officialPlainToken + `","signature":"` + officialSignature + `"}`
	if string(raw) != want {
		t.Errorf("回写体 = %s\nwant = %s", raw, want)
	}
}

// op=0 业务事件：正确解析 d.id 到 C2CMessage.ID（拿信封的 id 会得到 40034024）。
func TestParseWebhookDispatchC2C(t *testing.T) {
	c := newTestClientForWebhook(t)

	body := []byte(`{
	  "id":"EVENT-ID-SHOULD-NOT-BE-USED",
	  "op":0,
	  "t":"C2C_MESSAGE_CREATE",
	  "s":0,
	  "d":{
	    "id":"MSG-ID-CORRECT",
	    "content":"我要邀请码",
	    "timestamp":"2026-07-21T10:30:00+08:00",
	    "author":{"user_openid":"USER-OPENID-1","union_openid":"UNION-1"}
	  }
	}`)
	header := signHeader(t, officialBotSecret, body)

	payload, validation, err := c.ParseWebhook(body, header)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	if validation != nil {
		t.Errorf("业务事件不应返回 validation：%+v", validation)
	}
	if payload == nil {
		t.Fatal("payload 为 nil")
	}
	if payload.Op != OpDispatch {
		t.Errorf("Op = %d, want 0", int(payload.Op))
	}
	if payload.Type != EventC2CMessageCreate {
		t.Errorf("Type = %q", payload.Type)
	}

	var msg C2CMessage
	if err := payload.DecodeData(&msg); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	// 关键断言：被动回复要用的是 d.id，不是信封的 id
	if msg.ID != "MSG-ID-CORRECT" {
		t.Errorf("C2CMessage.ID = %q, want MSG-ID-CORRECT（d.id）", msg.ID)
	}
	if msg.ID == payload.ID {
		t.Error("拿成了信封 id —— 发给平台会得到 40034024「msg_id 无效或越权」")
	}
	if msg.Content != "我要邀请码" {
		t.Errorf("Content = %q", msg.Content)
	}
	if msg.Author.OpenID != "USER-OPENID-1" {
		t.Errorf("author.user_openid = %q", msg.Author.OpenID)
	}
}

// op=0 群 @ 消息解析。
func TestParseWebhookDispatchGroup(t *testing.T) {
	c := newTestClientForWebhook(t)

	body := []byte(`{"id":"E","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{
	  "id":"MSG-G","content":"/邀请码","group_openid":"GROUP-1","author":{"user_openid":"U2"}}}`)
	payload, _, err := c.ParseWebhook(body, signHeader(t, officialBotSecret, body))
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	var msg GroupAtMessage
	if err := payload.DecodeData(&msg); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if msg.ID != "MSG-G" || msg.GroupID != "GROUP-1" {
		t.Errorf("解析错误：%+v", msg)
	}
}

// 验签必须 fail-closed：签名错 / 缺头 / body 被改 / 长度不对 —— 全部拒绝。
func TestParseWebhookSignatureFailClosed(t *testing.T) {
	c := newTestClientForWebhook(t)
	body := []byte(`{"id":"E","op":0,"t":"C2C_MESSAGE_CREATE","d":{"id":"M"}}`)
	good := signHeader(t, officialBotSecret, body)

	t.Run("缺签名头", func(t *testing.T) {
		_, _, err := c.ParseWebhook(body, http.Header{})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("只缺 timestamp", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSignature, good.Get(HeaderSignature))
		_, _, err := c.ParseWebhook(body, h)
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("签名被篡改", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSignature, "00"+good.Get(HeaderSignature)[2:])
		h.Set(HeaderTimestamp, good.Get(HeaderTimestamp))
		_, _, err := c.ParseWebhook(body, h)
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("body 被改（签名对不上原始 body）", func(t *testing.T) {
		tampered := []byte(`{"id":"E","op":0,"t":"C2C_MESSAGE_CREATE","d":{"id":"HACKED"}}`)
		_, _, err := c.ParseWebhook(tampered, good)
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("签名不是 hex", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSignature, "zzzz")
		h.Set(HeaderTimestamp, "1725442341")
		_, _, err := c.ParseWebhook(body, h)
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("签名长度不对", func(t *testing.T) {
		h := http.Header{}
		h.Set(HeaderSignature, "abcd")
		h.Set(HeaderTimestamp, "1725442341")
		_, _, err := c.ParseWebhook(body, h)
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid", err)
		}
	})
	t.Run("用 AppSecret 当 BotSecret 验签", func(t *testing.T) {
		// 这是接线时最常见的错法：两个凭据不是一回事。
		other, err := newClient(Config{
			APIBase: "http://x", AppID: officialAppID,
			AppSecret: "DG5g3B4j9X2KOErG", BotSecret: "SOME-OTHER-SECRET",
		}, nil)
		if err != nil {
			t.Fatalf("newClient: %v", err)
		}
		if _, _, err := other.ParseWebhook(body, good); !errors.Is(err, ErrSignatureInvalid) {
			t.Errorf("err = %v, want ErrSignatureInvalid（BotSecret 与 AppSecret 不是同一个凭据）", err)
		}
	})
}

// 结构非法 / 未配置 / 未知 op 的处置。
func TestParseWebhookInvalidInputs(t *testing.T) {
	c := newTestClientForWebhook(t)

	t.Run("空 body", func(t *testing.T) {
		_, _, err := c.ParseWebhook(nil, http.Header{})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v, want ErrInvalidArgument", err)
		}
	})
	t.Run("非 JSON", func(t *testing.T) {
		_, _, err := c.ParseWebhook([]byte(`not json`), http.Header{})
		if err == nil {
			t.Fatal("应当报错")
		}
	})
	t.Run("op=13 但缺 d", func(t *testing.T) {
		_, _, err := c.ParseWebhook([]byte(`{"op":13}`), http.Header{})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v, want ErrInvalidArgument", err)
		}
	})
	t.Run("op=13 但 plain_token 空", func(t *testing.T) {
		_, _, err := c.ParseWebhook([]byte(`{"op":13,"d":{"event_ts":"1"}}`), http.Header{})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v, want ErrInvalidArgument", err)
		}
	})
	t.Run("未知 op 不放行", func(t *testing.T) {
		_, _, err := c.ParseWebhook([]byte(`{"op":7,"d":{}}`), http.Header{})
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v, want ErrInvalidArgument（未知 op 不能当业务事件放行）", err)
		}
	})
	t.Run("未配 BotSecret", func(t *testing.T) {
		noSecret, err := newClient(Config{APIBase: "http://x", AppID: "a", AppSecret: "b"}, nil)
		if err != nil {
			t.Fatalf("newClient: %v", err)
		}
		_, _, perr := noSecret.ParseWebhook([]byte(`{"op":13,"d":{"plain_token":"p","event_ts":"1"}}`), http.Header{})
		if !errors.Is(perr, ErrNotConfigured) {
			t.Errorf("err = %v, want ErrNotConfigured", perr)
		}
	})
	t.Run("DecodeData 空 d", func(t *testing.T) {
		var p Payload
		if err := p.DecodeData(&C2CMessage{}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("err = %v, want ErrInvalidArgument", err)
		}
	})
}

// ---- 测试辅助 ----

// newTestClientForWebhook 只做 webhook 的 client（不发网络，APIBase 随便写）。
func newTestClientForWebhook(t *testing.T) *client {
	t.Helper()
	c, err := newClient(Config{
		APIBase:   "http://unused.invalid",
		TokenURL:  "http://unused.invalid/token",
		AppID:     officialAppID,
		AppSecret: officialBotSecret,
		BotSecret: officialBotSecret,
	}, nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

// signHeader 按官方算法给 body 造一对合法签名头（业务事件的验签对象 = timestamp + 原始 body）。
func signHeader(t *testing.T, botSecret string, body []byte) http.Header {
	t.Helper()
	priv, err := derivePrivateKey(botSecret)
	if err != nil {
		t.Fatalf("derivePrivateKey: %v", err)
	}
	const ts = "1725442341"
	msg := append([]byte(ts), body...)
	h := http.Header{}
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderSignature, signHex(priv, msg))
	return h
}
