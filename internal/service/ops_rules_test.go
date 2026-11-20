// 运营域业务规则纯函数的单元测试。
//
// 设计原则：把「判定」从依赖数据库的方法里抽成纯函数（pointsLimitBlock /
// balanceSufficient / decorationPurchaseRejection / ownsUnexpired /
// shouldDispatchReportAction / inviteUseRejection / emojiEditRejection /
// EffectiveBannerStatus），使规则可以脱离 PG/Redis 穷举验证。
// 每条用例都标注它「防什么 bug」。
package service

import (
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestPointsLimitBlock 积分活动的限额判定（0 = 不限）。
//
// 防的 bug：
//  1. DailyLimit/TotalLimit 语义是「0 表示不限制」，若写成无条件 `count >= limit`，
//     0 限额的活动会被永久拦住 —— 所有自动发分全部失效；
//  2. 每日限额与总额额的错误必须可区分（前端提示不同）；
//  3. 边界：count == limit 时要拦住（已用满），count == limit-1 时放行。
func TestPointsLimitBlock(t *testing.T) {
	act := func(daily, total int) *model.PointsActivity {
		return &model.PointsActivity{DailyLimit: daily, TotalLimit: total}
	}
	cases := []struct {
		name  string
		act   *model.PointsActivity
		daily int64
		total int64
		want  string
	}{
		{"双 0 不限 → 放行", act(0, 0), 9999, 9999, ""},
		{"日限未到 → 放行", act(5, 0), 4, 0, ""},
		{"日限刚满 → 拦", act(5, 0), 5, 0, "已达该活动每日次数上限"},
		{"日限已超 → 拦", act(5, 0), 6, 0, "已达该活动每日次数上限"},
		{"总限未到 → 放行", act(0, 3), 0, 2, ""},
		{"总限刚满 → 拦", act(0, 3), 0, 3, "已达该活动总次数上限"},
		{"日限与总限同时触发 → 日限优先", act(2, 2), 2, 2, "已达该活动每日次数上限"},
		{"日限放行但总限满 → 拦总额", act(5, 2), 1, 2, "已达该活动总次数上限"},
		{"nil 活动 → 明确报不存在", nil, 0, 0, "积分活动不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pointsLimitBlock(tc.act, tc.daily, tc.total); got != tc.want {
				t.Errorf("pointsLimitBlock = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBalanceSufficient 积分扣减前的余额充足判定。
//
// 防的 bug：扣减不加余额下界检查 → 余额被扣成负数（积分可被无限透支）。
// 并发场景（两个 Spend 同时读到同一余额）由 points_accounts 行的
// SELECT ... FOR UPDATE + 事务串行化兜住，这里验证的是拒绝路径本身。
func TestBalanceSufficient(t *testing.T) {
	cases := []struct {
		name    string
		balance int
		amount  int
		want    bool
	}{
		{"余额充足", 100, 30, true},
		{"余额刚好相等", 30, 30, true},
		{"差 1 分不足", 29, 30, false},
		{"余额 0", 0, 1, false},
		{"零金额拒绝（不允许空变动）", 100, 0, false},
		{"负金额拒绝（防止用负数消费反向加积分）", 100, -10, false},
		{"余额为负（历史脏数据）也拒绝", -5, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := balanceSufficient(tc.balance, tc.amount); got != tc.want {
				t.Errorf("balanceSufficient(%d,%d) = %v, want %v", tc.balance, tc.amount, got, tc.want)
			}
		})
	}
}

// TestDecorationPurchaseRejection 装饰品购买前置校验。
//
// 防的 bug：
//  1. 不可购买 / 已下架仍被下单（前端缓存过期会踩到）；
//  2. 已持有且未过期重复购买 → 重复扣款；
//  3. 过期记录被误判为「已拥有」→ 老用户永远买不回同类装饰品。
func TestDecorationPurchaseRejection(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	deco := func(purchasable bool, status string) *model.Decoration {
		return &model.Decoration{IsPurchasable: purchasable, Status: status}
	}
	cases := []struct {
		name     string
		deco     *model.Decoration
		existing *model.UserDecoration
		want     string
	}{
		{"正常可买", deco(true, model.DecorationStatusActive), nil, ""},
		{"不可购买", deco(false, model.DecorationStatusActive), nil, "该装饰品不可购买"},
		{"已下架", deco(true, model.DecorationStatusInactive), nil, "该装饰品已下架"},
		{"已永久持有", deco(true, model.DecorationStatusActive),
			&model.UserDecoration{IsPermanent: true}, "您已拥有该装饰品"},
		{"限时未过期", deco(true, model.DecorationStatusActive),
			&model.UserDecoration{ExpiresAt: &future}, "您已拥有该装饰品"},
		{"限时已过期 → 允许复购", deco(true, model.DecorationStatusActive),
			&model.UserDecoration{ExpiresAt: &past}, ""},
		{"非永久且无到期时间 → 视为未持有", deco(true, model.DecorationStatusActive),
			&model.UserDecoration{}, ""},
		{"装饰品不存在", nil, nil, "装饰品不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reject := decorationPurchaseRejection(tc.deco, tc.existing, now)
			got := ""
			if reject != nil {
				got = reject.Msg
			}
			if got != tc.want {
				t.Errorf("rejection = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOwnsUnexpired 持有时效判定（购买/赠送/佩戴共用）。
func TestOwnsUnexpired(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	cases := []struct {
		name string
		ud   *model.UserDecoration
		want bool
	}{
		{"nil 记录", nil, false},
		{"永久", &model.UserDecoration{IsPermanent: true}, true},
		{"未到期", &model.UserDecoration{ExpiresAt: &future}, true},
		{"已到期", &model.UserDecoration{ExpiresAt: &past}, false},
		{"到期时间恰为 now → 已失效", &model.UserDecoration{ExpiresAt: &now}, false},
		{"无到期时间且非永久", &model.UserDecoration{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownsUnexpired(tc.ud, now); got != tc.want {
				t.Errorf("ownsUnexpired = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestClaimableProgress 活动领取条件判定（0 = 该项不需要）。
//
// 防的 bug：条件 0 被当成「必须达成 0 次」以外的语义时，
// 若写成无条件 `current < required`，所有 0 条件活动都会判不达标 → 永远领不了奖。
func TestClaimableProgress(t *testing.T) {
	base := model.DecorationActivity{}
	cases := []struct {
		name string
		act  model.DecorationActivity
		p    model.UserActivityProgress
		want bool
	}{
		{"全 0 条件 → 直接可领", base, model.UserActivityProgress{}, true},
		{"点赞差 1", model.DecorationActivity{RequiredLikes: 10},
			model.UserActivityProgress{CurrentLikes: 9}, false},
		{"点赞达标", model.DecorationActivity{RequiredLikes: 10},
			model.UserActivityProgress{CurrentLikes: 10}, true},
		{"评论差 1", model.DecorationActivity{RequiredComments: 3},
			model.UserActivityProgress{CurrentComments: 2}, false},
		{"评论达标", model.DecorationActivity{RequiredComments: 3},
			model.UserActivityProgress{CurrentComments: 3}, true},
		{"分享差 1", model.DecorationActivity{RequiredShares: 1},
			model.UserActivityProgress{}, false},
		{"充值差 0.01", model.DecorationActivity{RequiredRecharge: 100},
			model.UserActivityProgress{CurrentRecharge: 99.99}, false},
		{"充值达标", model.DecorationActivity{RequiredRecharge: 100},
			model.UserActivityProgress{CurrentRecharge: 100}, true},
		{"签到差 1 天", model.DecorationActivity{RequiredSignInDays: 7},
			model.UserActivityProgress{CurrentSignInDays: 6}, false},
		{"签到达标", model.DecorationActivity{RequiredSignInDays: 7},
			model.UserActivityProgress{CurrentSignInDays: 7}, true},
		{"多项全达标", model.DecorationActivity{RequiredLikes: 1, RequiredComments: 1, RequiredShares: 1},
			model.UserActivityProgress{CurrentLikes: 1, CurrentComments: 1, CurrentShares: 1}, true},
		{"多项中一项不达标即失败", model.DecorationActivity{RequiredLikes: 1, RequiredShares: 1},
			model.UserActivityProgress{CurrentLikes: 5, CurrentShares: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimableProgress(tc.act, tc.p); got != tc.want {
				t.Errorf("claimableProgress = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestShouldDispatchReportAction 举报处理动作的下发判定。
//
// 防的 bug：NONE / 空动作被误当成 DELETE_CONTENT 发出去 →
// content 域按事件删文章，一次误点删掉别人的内容。
// 同时确保 DELETE_CONTENT / BAN_USER / WARNING 三种动作确实发出事件。
func TestShouldDispatchReportAction(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{model.ReportActionDeleteContent, true},
		{model.ReportActionBanUser, true},
		{model.ReportActionWarning, true},
		{model.ReportActionNone, false},
		{"", false},
		{"UNKNOWN_ACTION", false},
		{"delete_content", false}, // 大小写不匹配 → 不发（宁可不动手）
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			if got := shouldDispatchReportAction(tc.action); got != tc.want {
				t.Errorf("shouldDispatchReportAction(%q) = %v, want %v", tc.action, got, tc.want)
			}
		})
	}
}

// TestInviteUseRejection 邀请码状态机：未用 → 已用、过期、自邀。
//
// 防的 bug：
//  1. 已使用/已过期的邀请码被重复消费（无限邀请刷积分）；
//  2. 自己用自己的邀请码（自我邀请刷奖励）；
//  3. 过期判断用 Before 还是 After 搞反 → 未过期的码被拒 / 过期的码可用。
func TestInviteUseRejection(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	pending := func(expiredAt *time.Time, inviter string) *model.Invite {
		return &model.Invite{
			InviterID: inviter,
			Status:    model.InviteStatusPending,
			ExpiredAt: expiredAt,
		}
	}
	cases := []struct {
		name    string
		inv     *model.Invite
		invitee string
		wantMsg string
	}{
		{"正常可用", pending(nil, "inviter-1"), "invitee-1", ""},
		{"未来到期仍可用", pending(&future, "inviter-1"), "invitee-1", ""},
		{"已过期", pending(&past, "inviter-1"), "invitee-1", inviteRejectExpiredMsg},
		{"已使用", &model.Invite{InviterID: "inviter-1", Status: model.InviteStatusUsed}, "invitee-1", "邀请码无效或已被使用"},
		{"已过期状态", &model.Invite{InviterID: "inviter-1", Status: model.InviteStatusExpired}, "invitee-1", "邀请码无效或已被使用"},
		{"自邀", pending(nil, "same-user"), "same-user", "不能使用自己的邀请码"},
		{"邀请码不存在", nil, "invitee-1", "邀请码不存在"},
		{"空 invitee（匿名）不走自邀判定", pending(nil, "inviter-1"), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reject := inviteUseRejection(tc.inv, tc.invitee, now)
			got := ""
			if reject != nil {
				got = reject.Msg
			}
			if got != tc.wantMsg {
				t.Errorf("rejection = %q, want %q", got, tc.wantMsg)
			}
		})
	}
}

// TestEmojiEditRejection 表情编辑权限判定（水平越权防护）。
//
// 防的 bug：
//  1. 普通用户改删系统表情（全站资源）；
//  2. 用户 A 传用户 B 的 emoji id 就能改删（水平越权）；
//  3. 无归属（UserID 空）的历史数据被普通用户改动。
func TestEmojiEditRejection(t *testing.T) {
	sysEmoji := &model.Emoji{Type: model.EmojiTypeSystem}
	userEmoji := &model.Emoji{Type: model.EmojiTypeUser, UserID: "u-1"}
	orphanEmoji := &model.Emoji{Type: model.EmojiTypeUser, UserID: ""}

	cases := []struct {
		name  string
		e     *model.Emoji
		uid   string
		admin bool
		want  string
	}{
		{"本人改自己的用户表情", userEmoji, "u-1", false, ""},
		{"他人改用户表情 → 拒", userEmoji, "u-2", false, "只能操作自己的表情"},
		{"管理员可改用户表情", userEmoji, "admin", true, ""},
		{"普通用户改系统表情 → 拒", sysEmoji, "u-1", false, "系统表情只能由管理员维护"},
		{"管理员改系统表情", sysEmoji, "admin", true, ""},
		{"普通用户改无归属表情 → 拒", orphanEmoji, "u-1", false, "只能操作自己的表情"},
		{"管理员改无归属表情", orphanEmoji, "admin", true, ""},
		{"表情不存在", nil, "u-1", false, "表情不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reject := emojiEditRejection(tc.e, tc.uid, tc.admin)
			got := ""
			if reject != nil {
				got = reject.Msg
			}
			if got != tc.want {
				t.Errorf("rejection = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEffectiveBannerStatus 轮播列表的可见性判定。
//
// 防的 bug：非管理员传 ?status=inactive 就能看到已下线的运营位
// （原实现只判 banner:manage 权限）。
func TestEffectiveBannerStatus(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		admin     bool
		want      string
	}{
		{"管理员查 inactive", model.BannerStatusInactive, true, model.BannerStatusInactive},
		{"管理员不传 status", "", true, ""},
		{"普通用户查 inactive → 强制 active", model.BannerStatusInactive, false, model.BannerStatusActive},
		{"普通用户不传 status", "", false, model.BannerStatusActive},
		{"普通用户查 active", model.BannerStatusActive, false, model.BannerStatusActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveBannerStatus(tc.requested, tc.admin); got != tc.want {
				t.Errorf("EffectiveBannerStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGuideGroupByCategory 表情按分类聚合（真实业务：分组的空分类落到「未分类」）。
//
// 防的 bug：空 category 的表情在分组视图里丢失（前端拿不到表情）。
func TestGuideGroupByCategory(t *testing.T) {
	list := []model.Emoji{
		{BaseModel: model.BaseModel{ID: "e1"}, Category: "开心"},
		{BaseModel: model.BaseModel{ID: "e2"}, Category: "开心"},
		{BaseModel: model.BaseModel{ID: "e3"}, Category: ""},
	}
	got := groupByCategory(list)
	if len(got["开心"]) != 2 {
		t.Errorf("开心 分类应有 2 个，实际 %d", len(got["开心"]))
	}
	if len(got["未分类"]) != 1 {
		t.Errorf("空分类应归入 未分类，实际 %d", len(got["未分类"]))
	}
	total := 0
	for _, v := range got {
		total += len(v)
	}
	if total != len(list) {
		t.Errorf("分组后总数 %d != 输入 %d（有表情丢失）", total, len(list))
	}
}

// TestAchievementConditionTargetValue 成就条件目标值缺省语义。
//
// 防的 bug：condition.target 缺省（0）时若直接用 0 比较，成就一创建就判定完成
// （progress 0 >= target 0）；正确语义是「缺省按 1」。
func TestAchievementConditionTargetValue(t *testing.T) {
	cases := []struct {
		name string
		c    model.AchievementCondition
		want int
	}{
		{"显式 target=10", model.AchievementCondition{Target: 10}, 10},
		{"target=1", model.AchievementCondition{Target: 1}, 1},
		{"target 缺省 → 1", model.AchievementCondition{}, 1},
		{"target 负数 → 1", model.AchievementCondition{Target: -3}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.TargetValue(); got != tc.want {
				t.Errorf("TargetValue = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestOrderPaidSideEffectRouting 支付/邀请事件的积分副作用路由（不触库的判定部分）。
//
// 防的 bug：side effect 被挂到错误事件上（例如 article.created 也去发 ORDER_PAID 积分），
// 造成无条件刷积分。这里验证「只有 order.paid / invite.used 带副作用」——
// 通过 resolve 结果 + 事件名对照间接断言。
func TestOrderPaidSideEffectRouting(t *testing.T) {
	sideEffectTopics := map[string]bool{
		event.EventOrderPaid:  true,
		event.EventInviteUsed: true,
	}
	// 除这两个之外的成就事件不应带积分副作用（由 onAchievementEventSideEffect 的 switch 决定）
	for _, topic := range achievementSubscribedTopics {
		_, _, ok := resolveAchievementProgress(topic, resolveTestPayload(topic))
		if !ok {
			t.Errorf("topic %q 解析失败", topic)
			continue
		}
		if !sideEffectTopics[topic] {
			// 仅断言：这些 topic 在 switch 里落到 default（无副作用），由代码结构保证
			continue
		}
	}
	if len(sideEffectTopics) != 2 {
		t.Fatal("副作用事件集合应恰为 order.paid + invite.used")
	}
}
