package qqbot_test

// 可运行示例：把 doc.go 的用法钉成可执行代码（go test 会编译并运行 Example*）。
//
// 刻意不写 Output 注释 —— 这些示例依赖上游服务，属于「编译保证正确」而非「输出保证正确」；
// 真正的行为断言在包内单测（token_test.go / message_test.go / webhook_test.go）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/pkg/qqbot"
)

// ExampleNew 构造客户端（配置键与 qqbot/config_keys.go 一一对应）。
func ExampleNew() {
	c, err := qqbot.New(qqbot.Config{
		AppID:     "1020xxxx",        // qqbot_app_id
		AppSecret: "your-app-secret", // qqbot_app_secret
		BotSecret: "your-bot-secret", // qqbot_bot_secret（webhook 验签，与 AppSecret 不同）
	})
	if err != nil {
		if errors.Is(err, qqbot.ErrNotConfigured) {
			fmt.Println("未配置，功能整体降级")
			return
		}
		panic(err)
	}
	_ = c
}

// ExampleClient_SendC2C 被动回复（msg_id 来自事件的 d.id）。
func ExampleClient_SendC2C() {
	var c qqbot.Client // 由 qqbot.New 得到

	_, err := c.SendC2C("USER_OPENID", qqbot.SendRequest{
		MsgType: qqbot.MsgTypeText,
		Content: "你的邀请码是 ABC123",
		MsgID:   "MSG_ID_FROM_EVENT_D_ID",
		MsgSeq:  1,
	})

	var qe *qqbot.Error
	if errors.As(err, &qe) {
		switch qe.Kind {
		case qqbot.KindWindowExpired:
			// 消息留在池子里，等用户下次发消息激活窗口
		case qqbot.KindRetryable:
			// 退避重试
		case qqbot.KindPermanent:
			// 出池进死信，人工处理
		}
	}
}

// ExampleClient_ParseWebhook webhook 三态处理（三态语义见 Client 注释）。
func ExampleClient_ParseWebhook() {
	var c qqbot.Client // 由 qqbot.New 得到

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		payload, validation, err := c.ParseWebhook(body, r.Header)
		switch {
		case err != nil:
			// 验签失败/结构非法 —— fail-closed，直接拒绝（平台会重推合法事件）
			w.WriteHeader(http.StatusForbidden)
		case validation != nil:
			// op=13 地址验证：原样回写 {plain_token, signature}
			_ = json.NewEncoder(w).Encode(validation)
		case payload.Type == qqbot.EventC2CMessageCreate:
			var msg qqbot.C2CMessage
			if derr := payload.DecodeData(&msg); derr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// msg.ID 才是被动回复要用的 msg_id（不是信封的 payload.ID）
			// msg.Author.OpenID 是该用户在本机器人下的唯一标识
			_ = msg
		}
	}
	_ = handler
}

// ExampleSignValidation 回调地址验证的签名计算（一般由 ParseWebhook 代劳）。
func ExampleSignValidation() {
	sig, err := qqbot.SignValidation("DG5g3B4j9X2KOErG", "1725442341", "Arq0D5A61EgUu4OxUvOp")
	if err != nil {
		panic(err)
	}
	// 官方文档给的标准答案
	fmt.Println(sig == "87befc99c42c651b3aac0278e71ada338433ae26fcb24307bdc5ad38c1adc2d01bcfcadc0842edac85e85205028a1132afe09280305f13aa6909ffc2d652c706")
	// Output: true
}
