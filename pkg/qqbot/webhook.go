package qqbot

// 入站：webhook 事件解析与 ed25519 验签。
//
// # 两类请求，处理方式不同（docs/29 §5.2 / §5.3）
//
//	op=13 地址验证  平台首次配置回调地址时调用，**不带签名头** → 必须算出签名回写
//	op=0  业务事件  带签名头 → 必须验签，防伪造
//
// # 签名算法（官方 Golang 示例逐字照搬，两个坑都在这里）
//
//	seed := botSecret
//	for len(seed) < ed25519.SeedSize {   // 32
//	    seed = strings.Repeat(seed, 2)   // ★ 循环填充，不是 hash
//	}
//	seed = seed[:ed25519.SeedSize]
//	_, privateKey, _ := ed25519.GenerateKey(strings.NewReader(seed))  // 确定性派生
//
//	msg := eventTs + plainToken          // ★ 顺序：event_ts 在前
//	signature := hex(ed25519.Sign(privateKey, msg))
//
// 把顺序写反、或对 secret 做 md5/sha256 再当 seed，都会验不过（且报错只有「校验失败」四个字，
// 极难查）。本文件把派生与签名拆成可单测的纯函数，并用官方给出的标准答案做锚点。

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// webhook 请求头（签名验证用）。
const (
	// HeaderSignature 业务事件的签名头（hex 编码的 ed25519 签名）。
	//
	// ⚠️ docs/29 §5.3 只记录了 op=13 的算法，**没有记录业务事件的验签头**。
	// 这里采用官方 webhook 文档的 Discord 风格约定（QQ Bot 沿用）：
	// 签名对象 = `X-Signature-Timestamp` + **原始 body**（不是重新序列化的 JSON）。
	HeaderSignature = "X-Signature-Ed25519"
	// HeaderTimestamp 签名时间戳头（与 body 拼接后参与验签）。
	HeaderTimestamp = "X-Signature-Timestamp"
	// HeaderBotAppID 平台在回调里带的 appid（可用于核对是否发给了本机器人）。
	HeaderBotAppID = "X-Bot-Appid"
)

// SignValidation 计算回调地址验证的签名（op=13 必须回写的 signature）。
//
// 返回 hex 字符串。botSecret 为空 → ErrNotConfigured；
// eventTs / plainToken 为空 → ErrInvalidArgument（算出来的签名一定不对，早失败）。
func SignValidation(botSecret, eventTs, plainToken string) (string, error) {
	if strings.TrimSpace(eventTs) == "" || strings.TrimSpace(plainToken) == "" {
		return "", &Error{Kind: KindInvalidArgument, Op: opParseWebhook,
			Err: fmt.Errorf("event_ts and plain_token are required: %w", ErrInvalidArgument)}
	}
	priv, err := derivePrivateKey(botSecret)
	if err != nil {
		return "", err
	}

	// ★ 顺序是 event_ts 在前、plain_token 在后 —— 反了验不过（docs/29 §5.3）。
	var msg bytes.Buffer
	msg.WriteString(eventTs)
	msg.WriteString(plainToken)

	return signHex(priv, msg.Bytes()), nil
}

// signHex 用私钥签名并 hex 编码（SignValidation 与单测共用，避免两处各写一遍算法）。
func signHex(priv ed25519.PrivateKey, msg []byte) string {
	return hex.EncodeToString(ed25519.Sign(priv, msg))
}

// VerifyEventSignature 校验业务事件的签名。
//
// 签名对象 = timestamp + **原始 body 字节**，用 BotSecret 派生的密钥对公钥验。
// 必须传原始 body：重新 json.Marshal 会改变字节序，签名必然对不上。
//
// 失败一律返回包着 ErrSignatureInvalid 的 *Error（**fail-closed**）：
// 伪造请求的代价远高于「少收几条事件」，而少收的那几条平台会重推。
func VerifyEventSignature(botSecret, sigHex, timestamp string, body []byte) error {
	if strings.TrimSpace(botSecret) == "" {
		return &Error{Kind: KindConfig, Op: opParseWebhook, Err: ErrNotConfigured}
	}
	if sigHex == "" || timestamp == "" {
		// 缺头也当验签失败：业务事件必须带签名，缺了就无从判断来源。
		return &Error{Kind: KindPermanent, Op: opParseWebhook,
			Err: fmt.Errorf("missing %s / %s header: %w", HeaderSignature, HeaderTimestamp, ErrSignatureInvalid)}
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return &Error{Kind: KindPermanent, Op: opParseWebhook,
			Err: fmt.Errorf("signature is not hex: %w: %v", ErrSignatureInvalid, err)}
	}
	if len(sig) != ed25519.SignatureSize {
		return &Error{Kind: KindPermanent, Op: opParseWebhook,
			Err: fmt.Errorf("signature length %d != %d: %w", len(sig), ed25519.SignatureSize, ErrSignatureInvalid)}
	}
	// 官方「安全和授权」示例明确要求：最后一字节高 3 位必须为 0
	// （Ed25519 的 S 值必须 < L，这是防延展性攻击的 Discord 风格预筛）。
	//
	// Go 标准库 ed25519.Verify 内部也会拒绝非规范 S，但官方示例要求在这里
	// 快速失败 —— 恶意构造的签名不该进入椭圆曲线运算。
	if sig[63]&224 != 0 {
		return &Error{Kind: KindPermanent, Op: opParseWebhook,
			Err: fmt.Errorf("signature S value out of range (sig[63]&224 != 0): %w", ErrSignatureInvalid)}
	}

	priv, err := derivePrivateKey(botSecret)
	if err != nil {
		return err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return &Error{Kind: KindConfig, Op: opParseWebhook, Err: ErrNotConfigured}
	}

	msg := make([]byte, 0, len(timestamp)+len(body))
	msg = append(msg, timestamp...)
	msg = append(msg, body...)
	if !ed25519.Verify(pub, msg, sig) {
		return &Error{Kind: KindPermanent, Op: opParseWebhook, Err: ErrSignatureInvalid}
	}
	return nil
}

// derivePrivateKey 由 BotSecret 派生出 ed25519 私钥。
//
// 官方算法（docs/29 §5.3）：把 secret 重复填充到 32 字节当 seed，再用它喂 GenerateKey。
// 注意三个「不是」：
//   - 不是 hash（md5/sha256 都会得到另一个密钥，验签永远失败）；
//   - 不是截断（secret 短于 32 时必须先 Repeat 再截断）；
//   - 不是 PKCS/随机（GenerateKey 从 reader 读取确定性 seed，不是 crypto/rand）。
func derivePrivateKey(botSecret string) (ed25519.PrivateKey, error) {
	seed := strings.TrimSpace(botSecret)
	if seed == "" {
		return nil, &Error{Kind: KindConfig, Op: opParseWebhook,
			Err: fmt.Errorf("%w: bot secret is empty", ErrNotConfigured)}
	}
	for len(seed) < ed25519.SeedSize {
		seed = strings.Repeat(seed, 2)
	}
	seed = seed[:ed25519.SeedSize]

	_, priv, err := ed25519.GenerateKey(strings.NewReader(seed))
	if err != nil {
		return nil, &Error{Kind: KindConfig, Op: opParseWebhook,
			Err: fmt.Errorf("%w: derive key: %v", ErrNotConfigured, err)}
	}
	return priv, nil
}

// DecodeData 把信封的 d 字段解析到 out（如 *C2CMessage / *GroupAtMessage）。
//
// 事件体结构由 Type 决定，SDK 不做类型分派 —— 那是调用方的 switch 语句，
// 因为「哪些事件要落库」属于业务判断。
func (p *Payload) DecodeData(out any) error {
	if p == nil {
		return &Error{Kind: KindInvalidArgument, Op: opParseWebhook,
			Err: fmt.Errorf("nil payload: %w", ErrInvalidArgument)}
	}
	if len(p.Data) == 0 {
		return &Error{Kind: KindInvalidArgument, Op: opParseWebhook,
			Err: fmt.Errorf("payload has no d field: %w", ErrInvalidArgument)}
	}
	if err := json.Unmarshal(p.Data, out); err != nil {
		return wrapErr(opParseWebhook, KindInvalidArgument, fmt.Errorf("decode d: %v", err))
	}
	return nil
}

// ParseWebhook 解析并校验 webhook 请求体（三态语义见 types.go 的 Client 注释）。
//
// 处理顺序刻意是「先看 op 再决定要不要验签」：
// op=13 的地址验证请求**不带签名头**，若统一要求签名，平台配置回调地址时必然失败。
func (c *client) ParseWebhook(body []byte, header http.Header) (*Payload, *ValidationResponse, error) {
	if c == nil || c.cfg.BotSecret == "" {
		return nil, nil, &Error{Kind: KindConfig, Op: opParseWebhook, Err: ErrNotConfigured}
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil, invalidArg(opParseWebhook, "empty webhook body")
	}

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, wrapErr(opParseWebhook, KindInvalidArgument, fmt.Errorf("decode payload: %v", err))
	}

	switch payload.Op {
	case OpValidation:
		// 地址验证：不要求签名（平台此时还没有/不使用签名头），但要算出签名回写。
		var vr ValidationRequest
		if len(payload.Data) == 0 {
			return nil, nil, invalidArg(opParseWebhook, "validation payload has no d field")
		}
		if err := json.Unmarshal(payload.Data, &vr); err != nil {
			return nil, nil, wrapErr(opParseWebhook, KindInvalidArgument, fmt.Errorf("decode validation: %v", err))
		}
		sig, err := SignValidation(c.cfg.BotSecret, vr.EventTs, vr.PlainToken)
		if err != nil {
			return nil, nil, err
		}
		return &payload, &ValidationResponse{PlainToken: vr.PlainToken, Signature: sig}, nil

	case OpDispatch:
		// 业务事件：必须验签（header 为 nil 时按缺头处理 → fail-closed）。
		var h http.Header
		if header != nil {
			h = header
		}
		if err := VerifyEventSignature(c.cfg.BotSecret, h.Get(HeaderSignature), h.Get(HeaderTimestamp), body); err != nil {
			return nil, nil, err
		}
		return &payload, nil, nil

	default:
		// types.go 只定义了 0 / 13 两种 op。其余 op 不在官方 webhook 契约内，
		// 明确报错而不是当业务事件放行 —— 静默放过未知 op 会让调用方误处理数据。
		return nil, nil, invalidArg(opParseWebhook, "unsupported op %d", int(payload.Op))
	}
}
