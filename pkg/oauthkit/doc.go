// Package oauthkit 第三方登录统一抽象（微信 / QQ / GitHub）。
//
// 一个零业务耦合的纯协议层：只负责「拼授权 URL、code 换 token、token 换用户、
// 刷新 token」这四件事，不持有 Redis / DB / 日志，也不做账号归属判断
// （谁是同一个人、要不要合并账号，属于接入方业务）。
//
// 唯一的第三方依赖是 golang.org/x/oauth2（BSD-3，仅 GitHub 实现使用）。
//
// # 快速开始
//
//	p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelWeb, oauthkit.Config{
//		ClientID:     "wx1234567890abcdef",
//		ClientSecret: "your-app-secret",
//		RedirectURI:  "https://api.example.com/api/v1/user/oauth/wechat/web/callback",
//	})
//	if err != nil {
//		return err // 组合不支持，或 github scope 为空
//	}
//	if !p.Configured() {
//		return errors.New("第三方登录未配置") // 千万别在这里发授权链接
//	}
//
//	// ① 生成授权链接（state 由调用方生成并自行存取，SDK 不碰 Redis）
//	url, err := p.AuthURL(state)
//
//	// ② 回调：code 换 token
//	tok, err := p.Exchange(ctx, code)
//
//	// ③ token 换用户
//	u, err := p.UserInfo(ctx, tok)
//	// u.Provider / u.ID(openid) / u.UnionID / u.Nickname / u.AvatarURL / u.Email
//
// # 渠道矩阵
//
// 按 (provider, channel) 实例化 —— 微信「网站应用」与「移动应用」是两个独立 appid，
// 同一用户在两渠道 openid 不同、unionid 相同，所以 Name() 给平台、Channel() 给渠道：
//
//	provider   channel  授权发起方      后端换 token  后端拿 openid        后端必须校验
//	wechat     web      后端给 URL       是            换 token 时返回      否（自己换的）
//	wechat     app      客户端 SDK       是（客户端交 code）  同上          否（自己换的）
//	qq         web      后端给 URL       是            额外调 oauth2.0/me   否
//	qq         app      客户端 SDK       否（SDK 直接给 token） SDK 返回     是（ValidateToken）
//	github     web      后端给 URL       是            /user 的 id          否
//
// 两个不对称必须保留，不许为「接口好看」抹平：
//   - 微信 App 交的是 code（后端自己换 token，可信）；
//   - QQ App 交的是 access_token（客户端可信度为零，必须 ValidateToken 校验 openid 一致性）。
//
// 渠道不支持的方法返回对应哨兵错误而不 panic：
// AuthURL → ErrAuthURLUnsupported / Exchange → ErrExchangeUnsupported /
// Refresh → ErrRefreshUnsupported。
//
// # 错误处理
//
// 所有失败都是**哨兵错误**，用 errors.Is 判断，不要比对字符串：
//
//	tok, err := p.Exchange(ctx, code)
//	switch {
//	case errors.Is(err, oauthkit.ErrInvalidArgument):
//		// 我的错（空 code / 空 state）—— 重试无用，改代码
//	case errors.Is(err, oauthkit.ErrCodeInvalid):
//		// 平台说 code 无效/过期/已用 —— 引导用户重新授权
//	case errors.Is(err, oauthkit.ErrUpstream):
//		// 上游异常（网络 / 5xx / 解析失败）—— 可重试
//	}
//
// 这个区分（ErrInvalidArgument vs 平台侧错误）是刻意做的：混用会让调用方
// 朝相反方向处理（例如对一个必失败的空参数反复请用户重新授权）。
//
// # 安全约束（运行时硬约束，不是注释）
//
//   - 微信 App / QQ App 的 token 若 Channel == "app" 且未 Verified，UserInfo 直接
//     返回 ErrTokenNotVerified —— 防止客户端伪造 openid + token 组合建号。
//   - 「校验类端点必须双向验证」：sns/auth / oauth2.0/me 的响应必须既能解析、
//     又确实含该平台的标志字段；只有「能解析」等于没验（网关错误页 / 空 body
//     会被当成校验通过，从而给伪造 token 盖上 Verified 章）。
//   - Token.Raw 保存原始响应体（可能含 token 明文），**不要整体打进日志**。
//
// # 不做什么
//
//   - 不存 state（调用方自己存取并校验，且必须 fail-closed）；
//   - 不存 access_token / refresh_token（平台 token 只在登录那一刻有用）；
//   - 不做账号绑定 / 合并 / 封禁判断；
//   - 不读配置（Config 由调用方填充）。
package oauthkit
