package qqbot

// 官方「安全和授权」文档给出的可执行锚点。
//
// 为什么单独一个文件：这两条是**判断实现正确与否的唯一充分证据**。
// 签名算法「看起来对」和「真的对」之间差着字段顺序、seed 填充规则、
// 原始字节 vs 重新序列化 这些坑，只有和官方输出逐字比对才能证明。

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// 官方锚点 A：seed 派生（28 字节 secret，触发 repeat 后用前 32 字节）
const (
	anchorSeedSecret = "naOC0ocQE3shWLAfffVLB1rhYPG7"
	anchorSeedExpect = "naOC0ocQE3shWLAfffVLB1rhYPG7naOC"
)

// 官方锚点 A 的期望公钥（文档逐字节给出）。
var anchorPublicKey = []byte{
	215, 195, 98, 254, 120, 174, 248, 31, 242, 50, 135, 180, 147, 98, 139, 93,
	176, 42, 60, 79, 227, 11, 33, 94, 77, 25, 96, 155, 93, 118, 103, 58,
}

// TestOfficialAnchorSeedDerivation 锚点 A：seed 必须「循环填充后截断到 32 字节」。
//
// 28×2=56 > 32，这条会走到**截断**分支 —— 比 16 字节 secret 更能区分
// 「repeat 后截断」与「hash」两种实现（若是 sha256(secret)，公钥完全不同）。
func TestOfficialAnchorSeedDerivation(t *testing.T) {
	seed := anchorSeedSecret
	for len(seed) < ed25519.SeedSize {
		seed = strings.Repeat(seed, 2)
	}
	seed = seed[:ed25519.SeedSize]
	if seed != anchorSeedExpect {
		t.Fatalf("seed = %q, want %q", seed, anchorSeedExpect)
	}

	priv, err := derivePrivateKey(anchorSeedSecret)
	if err != nil {
		t.Fatalf("derivePrivateKey: %v", err)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("public key type assertion failed")
	}
	if len(pub) != len(anchorPublicKey) {
		t.Fatalf("publicKey len = %d, want %d", len(pub), len(anchorPublicKey))
	}
	for i := range anchorPublicKey {
		if pub[i] != anchorPublicKey[i] {
			t.Fatalf("publicKey[%d] = %d, want %d\n全量 got  = %v\n全量 want = %v",
				i, pub[i], anchorPublicKey[i], []byte(pub), anchorPublicKey)
		}
	}
	t.Logf("锚点 A 逐字节匹配：publicKey = %v", []byte(pub))
}

// 自洽锚点用的时间戳（取官方 DEMO 里的值，但它不参与「能不能对上」的判定，
// 只作为固定输入让签名可复现）。
const anchorEventTimestamp = "1725442341"

// ---------- 业务事件验签：为什么没有「官方数值锚点」 ----------

// 官方文档「安全和授权」页给出了第二段 DEMO（secret/body/timestamp → sig），
// 但我穷举 60 种合理解释（3 种 seed 派生 × 5 种 body 字节 × 4 种拼接顺序）
// **全部无法复现该 sig**。而同一页的第一段 DEMO（seed → publicKey）是逐字节匹配的，
// 说明密钥派生实现正确、问题在 DEMO 2 的 sig 本身（判定为文档占位值）。
//
// 因此这里**不拿 DEMO 2 当锚点**（拿一个错的锚点当验收标准，会让后来人
// 怀疑自己写对了的实现）。改为「自洽锚点」：
//   · 用本项目派生的私钥签 → 用官方文档明确规定的算法验 → 必须通过；
//   · 任何字节改动都必须验不过。
//
// 「与官方一致」由两件事共同保证，不依赖 DEMO 2：
//   1. 密钥派生 —— 锚点 A 逐字节匹配官方 publicKey（见 TestOfficialAnchorSeedDerivation）；
//   2. 签名对象与头名 —— 官方文档**文字**明确规定（非 DEMO）：
//      Header `X-Signature-Ed25519`（hex）+ `X-Signature-Timestamp`，
//      签名体 = `timestamp + 原始 body`，用同一 seed 派生的**公钥**验。

// selfSigned 用官方锚点 secret 的派生私钥对 body 签名，返回 hex。
func selfSigned(timestamp, body string) string {
	priv, err := derivePrivateKey(anchorSeedSecret)
	if err != nil {
		panic(err)
	}
	// 签名体 = timestamp + 原始 body 字节（官方文档规定的顺序，不可颠倒）
	return signHex(priv, append([]byte(timestamp), []byte(body)...))
}

// TestWebhookSignatureRoundTrip 自洽锚点：自己签 → 自己验必须通过。
func TestWebhookSignatureRoundTrip(t *testing.T) {
	const body = `{"op":0,"d":{"id":"ROBOT1.0_x"},"t":"C2C_MESSAGE_CREATE"}`
	sig := selfSigned(anchorEventTimestamp, body)

	if err := VerifyEventSignature(anchorSeedSecret, sig, anchorEventTimestamp, []byte(body)); err != nil {
		t.Fatalf("自签自验失败: %v", err)
	}
	t.Logf("自洽锚点通过：sig = %s", sig)
}

// TestWebhookSignatureRawBytesOnly 反向证明：签名对象必须是**原始 body 字节**。
//
// 三个变体都必须验签失败，否则说明实现偷偷做了归一化（trim / 重新序列化），
// 那会让「官方按原始字节签名」的真实请求验不过。
func TestWebhookSignatureRawBytesOnly(t *testing.T) {
	const body = `{"op":0,"d":{},"t":"GATEWAY_EVENT_NAME"}`
	sig := selfSigned(anchorEventTimestamp, body)

	cases := []struct {
		name string
		body string
	}{
		{"尾部多空格", body + " "},
		{"头部多空格", " " + body},
		{"重新序列化(带空格)", `{ "op": 0,"d": {}, "t": "GATEWAY_EVENT_NAME"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := VerifyEventSignature(anchorSeedSecret, sig, anchorEventTimestamp, []byte(c.body)); err == nil {
				t.Fatal("变体 body 竟然验签通过 —— 说明实现做了归一化，违反「原始字节」契约")
			}
		})
	}
}

// TestWebhookSignatureTimestampBound 时间戳参与签名：换时间戳必须验不过。
func TestWebhookSignatureTimestampBound(t *testing.T) {
	const body = `{"op":0,"d":{},"t":"GATEWAY_EVENT_NAME"}`
	sig := selfSigned(anchorEventTimestamp, body)

	if err := VerifyEventSignature(anchorSeedSecret, sig, "1725442342", []byte(body)); err == nil {
		t.Fatal("换了 timestamp 还能验过 —— 说明 timestamp 没参与签名体")
	}
}

// TestSignatureSHighBitsRejected sig[63]&224 != 0 必须被拒（官方示例的严格校验）。
//
// 这条防的是 Ed25519 延展性攻击面：S 值必须 < L。恶意构造的高位签名
// 不该进入椭圆曲线运算（Go 标准库内部也会拒，但官方要求提前快速失败）。
func TestSignatureSHighBitsRejected(t *testing.T) {
	const body = `{"op":0,"d":{},"t":"GATEWAY_EVENT_NAME"}`
	good := selfSigned(anchorEventTimestamp, body)

	raw, err := hex.DecodeString(good)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[63] |= 0x80
	badSig := hex.EncodeToString(raw)

	err = VerifyEventSignature(anchorSeedSecret, badSig, anchorEventTimestamp, []byte(body))
	if err == nil {
		t.Fatal("sig[63]&224 != 0 竟然通过 —— 严格校验没生效")
	}
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
	if !strings.Contains(err.Error(), "sig[63]&224") {
		t.Fatalf("错误信息未指明 S 值越界: %v", err)
	}
}

// TestKindWakeupExhausted 40034122 → KindWakeupExhausted，且不可重试。
//
// 这条守的是池子语义：召回额度用尽时「重试同一条召回」必然一直失败，
// 正确动作是换模式（去掉 is_wakeup 走普通主动）。
func TestKindWakeupExhausted(t *testing.T) {
	k := KindOf(40034122)
	if k != KindWakeupExhausted {
		t.Fatalf("KindOf(40034122) = %v, want %v", k, KindWakeupExhausted)
	}
	if k.Retryable() {
		t.Fatal("KindWakeupExhausted 不该 Retryable —— 重试召回必然失败到 4 周期结束")
	}
	if k.String() != "wakeup_exhausted" {
		t.Fatalf("String() = %q, want %q", k.String(), "wakeup_exhausted")
	}
}
