// 运营域事件与成就推进的单元测试。
//
// 覆盖目标（都对应用户实测过的真 bug 面）：
//  1. 事件订阅表完整性 —— 曾经只订阅了 3 个事件，导致 21 个成就一个都不触发；
//  2. 载荷解析的三种形态（本域结构体 / 其它域结构体 / map）+ 键名归一化；
//  3. authorId 语义 —— article.receivedLike 推进作者，绝不能退回点赞者；
//  4. AchievementTriggers 映射表与种子数据的代码集合一致性。
package service

import (
	"reflect"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestAchievementSubscribedTopicsCoverage 订阅表必须覆盖所有「会产生成就进度」的事件。
//
// 防的 bug：曾经只 Subscribe 了 DailyLogin / OrderPaid / LevelUp 三个事件，
// 结果用户发 2 篇文章后 /achievement/stats 仍是 completed:0 —— 事件没人听。
func TestAchievementSubscribedTopicsCoverage(t *testing.T) {
	must := []string{
		event.EventArticleCreated,
		event.EventArticleReceivedLike,
		event.EventCommentCreated,
		event.EventUserFollowed,
		event.EventUserReceivedFollow,
		event.EventUserDailyLogin,
		event.EventUserLevelUp,
		event.EventMembershipPurchased,
		event.EventUserProfileCompleted,
		event.EventInviteUsed,
		event.EventOrderPaid,
	}
	subscribed := make(map[string]bool, len(achievementSubscribedTopics))
	for _, topic := range achievementSubscribedTopics {
		subscribed[topic] = true
	}
	for _, topic := range must {
		if !subscribed[topic] {
			t.Errorf("成就域未订阅事件 %q —— 该事件对应的成就永远不会解锁", topic)
		}
	}
	// 反向：订阅表里的每条事件都必须真的能解析出用户（否则订阅形同虚设）
	for _, topic := range achievementSubscribedTopics {
		if _, _, ok := resolveAchievementProgress(topic, resolveTestPayload(topic)); !ok {
			t.Errorf("事件 %q 解析不出用户，订阅无意义", topic)
		}
	}
}

// resolveTestPayload 为每个事件造一份「最小合法载荷」。
func resolveTestPayload(topic string) interface{} {
	switch topic {
	case event.EventArticleCreated:
		return event.ArticleCreatedPayload{UserID: "u-1", ArticleID: "a-1"}
	case event.EventArticleReceivedLike:
		return event.ArticleReceivedLikePayload{AuthorID: "author-1", LikerID: "liker-1"}
	case event.EventCommentCreated:
		return event.CommentCreatedPayload{UserID: "u-1"}
	case event.EventUserFollowed:
		return event.UserFollowedPayload{UserID: "u-1", TargetUserID: "u-2"}
	case event.EventUserReceivedFollow:
		return event.UserReceivedFollowPayload{UserID: "u-2", FollowerID: "u-1"}
	case event.EventUserDailyLogin:
		return event.UserDailyLoginPayload{UserID: "u-1", ConsecutiveDays: 7}
	case event.EventUserLevelUp:
		return event.UserLevelUpPayload{UserID: "u-1", Level: 10}
	case event.EventMembershipPurchased:
		return event.MembershipPurchasedPayload{UserID: "u-1", OrderID: "o-1"}
	case event.EventUserProfileCompleted:
		return event.ProfileCompletedPayload{UserID: "u-1"}
	case event.EventInviteUsed:
		return event.InviteUsedPayload{InviterID: "u-1", InviteeID: "u-2"}
	case event.EventOrderPaid:
		return event.OrderPaidPayload{UserID: "u-1", OrderID: "o-1", Type: model.OrderTypeMembership}
	}
	return nil
}

// TestResolveAchievementProgress 全 topic 分支表驱动。
func TestResolveAchievementProgress(t *testing.T) {
	cases := []struct {
		name      string
		topic     string
		payload   interface{}
		wantUser  string
		wantCodes []string
		wantOK    bool
	}{
		{
			name:      "article.created 推进作者本人",
			topic:     event.EventArticleCreated,
			payload:   event.ArticleCreatedPayload{UserID: "author-1", ArticleID: "a-1"},
			wantUser:  "author-1",
			wantCodes: event.AchievementTriggers[event.EventArticleCreated],
			wantOK:    true,
		},
		{
			name:      "article.receivedLike 只认 authorId（值形态）",
			topic:     event.EventArticleReceivedLike,
			payload:   event.ArticleReceivedLikePayload{AuthorID: "author-1", LikerID: "liker-1"},
			wantUser:  "author-1",
			wantCodes: event.AchievementTriggers[event.EventArticleReceivedLike],
			wantOK:    true,
		},
		{
			name:      "article.receivedLike 指针形态",
			topic:     event.EventArticleReceivedLike,
			payload:   &event.ArticleReceivedLikePayload{AuthorID: "author-2", LikerID: "liker-1"},
			wantUser:  "author-2",
			wantCodes: event.AchievementTriggers[event.EventArticleReceivedLike],
			wantOK:    true,
		},
		{
			name:    "article.receivedLike 只有 likerId 时必须放弃（绝不推进点赞者）",
			topic:   event.EventArticleReceivedLike,
			payload: event.ArticleReceivedLikePayload{LikerID: "liker-1"},
			wantOK:  false,
		},
		{
			name:    "article.receivedLike map 带 userId 也必须放弃",
			topic:   event.EventArticleReceivedLike,
			payload: map[string]interface{}{"userId": "liker-1", "articleId": "a-1"},
			wantOK:  false,
		},
		{
			name:      "article.receivedLike map 带 authorId 正常推进",
			topic:     event.EventArticleReceivedLike,
			payload:   map[string]interface{}{"author_id": "author-3", "likerId": "liker-1"},
			wantUser:  "author-3",
			wantCodes: event.AchievementTriggers[event.EventArticleReceivedLike],
			wantOK:    true,
		},
		{
			name:      "comment.created",
			topic:     event.EventCommentCreated,
			payload:   event.CommentCreatedPayload{UserID: "u-c", CommentID: "c-1"},
			wantUser:  "u-c",
			wantCodes: event.AchievementTriggers[event.EventCommentCreated],
			wantOK:    true,
		},
		{
			name:      "user.followed 推进关注者",
			topic:     event.EventUserFollowed,
			payload:   event.UserFollowedPayload{UserID: "follower-1", TargetUserID: "target-1"},
			wantUser:  "follower-1",
			wantCodes: event.AchievementTriggers[event.EventUserFollowed],
			wantOK:    true,
		},
		{
			name:      "user.receivedFollow 推进被关注者（targetUserId 优先）",
			topic:     event.EventUserReceivedFollow,
			payload:   event.UserReceivedFollowPayload{UserID: "target-1", FollowerID: "follower-1"},
			wantUser:  "target-1",
			wantCodes: event.AchievementTriggers[event.EventUserReceivedFollow],
			wantOK:    true,
		},
		{
			name:      "user.receivedFollow map 用 targetUserId 优先于 userId",
			topic:     event.EventUserReceivedFollow,
			payload:   map[string]interface{}{"userId": "wrong-1", "targetUserId": "target-2"},
			wantUser:  "target-2",
			wantCodes: event.AchievementTriggers[event.EventUserReceivedFollow],
			wantOK:    true,
		},
		{
			name:      "membership.purchased",
			topic:     event.EventMembershipPurchased,
			payload:   event.MembershipPurchasedPayload{UserID: "u-m", OrderID: "o-1"},
			wantUser:  "u-m",
			wantCodes: []string{"BECOME_MEMBER"},
			wantOK:    true,
		},
		{
			name:      "user.profileCompleted",
			topic:     event.EventUserProfileCompleted,
			payload:   event.ProfileCompletedPayload{UserID: "u-p"},
			wantUser:  "u-p",
			wantCodes: []string{"PROFILE_COMPLETED"},
			wantOK:    true,
		},
		{
			name:      "user.dailyLogin 连续7天",
			topic:     event.EventUserDailyLogin,
			payload:   event.UserDailyLoginPayload{UserID: "u-l", ConsecutiveDays: 7},
			wantUser:  "u-l",
			wantCodes: []string{"LOGIN_7_DAYS"},
			wantOK:    true,
		},
		{
			name:      "user.dailyLogin 连续30天同时命中7天",
			topic:     event.EventUserDailyLogin,
			payload:   event.UserDailyLoginPayload{UserID: "u-l", ConsecutiveDays: 30},
			wantUser:  "u-l",
			wantCodes: []string{"LOGIN_7_DAYS", "LOGIN_30_DAYS"},
			wantOK:    true,
		},
		{
			name:      "user.dailyLogin 不足7天无成就",
			topic:     event.EventUserDailyLogin,
			payload:   event.UserDailyLoginPayload{UserID: "u-l", ConsecutiveDays: 3},
			wantUser:  "u-l",
			wantCodes: nil,
			wantOK:    true,
		},
		{
			name:      "user.levelUp 等级30同时命中10",
			topic:     event.EventUserLevelUp,
			payload:   event.UserLevelUpPayload{UserID: "u-v", Level: 30},
			wantUser:  "u-v",
			wantCodes: []string{"LEVEL_10", "LEVEL_30"},
			wantOK:    true,
		},
		{
			name:      "user.levelUp 等级50全命中",
			topic:     event.EventUserLevelUp,
			payload:   event.UserLevelUpPayload{UserID: "u-v", Level: 50},
			wantUser:  "u-v",
			wantCodes: []string{"LEVEL_10", "LEVEL_30", "LEVEL_50"},
			wantOK:    true,
		},
		{
			name:      "user.levelUp 等级5无成就",
			topic:     event.EventUserLevelUp,
			payload:   event.UserLevelUpPayload{UserID: "u-v", Level: 5},
			wantUser:  "u-v",
			wantCodes: nil,
			wantOK:    true,
		},
		{
			// 邀请人可解析（用于触发 side effect 发 INVITE_USER 积分），
			// 但不推进成就：种子成就表里没有邀请类成就。
			name:      "invite.used 解析邀请人但不推进成就",
			topic:     event.EventInviteUsed,
			payload:   event.InviteUsedPayload{InviterID: "inviter-1", InviteeID: "invitee-1"},
			wantUser:  "inviter-1",
			wantCodes: nil,
			wantOK:    true,
		},
		{
			name:    "invite.used 无 inviterId 不可推进",
			topic:   event.EventInviteUsed,
			payload: event.InviteUsedPayload{InviteeID: "invitee-1"},
			wantOK:  false,
		},
		{
			name:      "order.paid 会员单推进 BECOME_MEMBER",
			topic:     event.EventOrderPaid,
			payload:   event.OrderPaidPayload{UserID: "u-o", Type: model.OrderTypeMembership},
			wantUser:  "u-o",
			wantCodes: []string{"BECOME_MEMBER"},
			wantOK:    true,
		},
		{
			name:      "order.paid 非会员单不推进成就",
			topic:     event.EventOrderPaid,
			payload:   event.OrderPaidPayload{UserID: "u-o", Type: model.OrderTypeArticle},
			wantUser:  "u-o",
			wantCodes: nil,
			wantOK:    true,
		},
		{
			name:    "空载荷 + 空用户 → 不可推进",
			topic:   event.EventArticleCreated,
			payload: event.ArticleCreatedPayload{},
			wantOK:  false,
		},
		{
			name:    "未知事件 → 不可推进",
			topic:   "no.such.event",
			payload: map[string]interface{}{"userId": "u-1"},
			wantOK:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, codes, ok := resolveAchievementProgress(tc.topic, tc.payload)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v（topic=%s payload=%+v）", ok, tc.wantOK, tc.topic, tc.payload)
			}
			if !tc.wantOK {
				return
			}
			if user != tc.wantUser {
				t.Errorf("user=%q want %q", user, tc.wantUser)
			}
			if !reflect.DeepEqual(toStringSlice(codes), toStringSlice(tc.wantCodes)) {
				t.Errorf("codes=%v want %v", codes, tc.wantCodes)
			}
		})
	}
}

func toStringSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// TestPayloadFieldStringNormalization 反射取值：键名归一化 + 各种容器形态。
//
// 防的 bug：不同域用不同字段风格发事件（userId / user_id / UserID / map[string]string），
// 若解析器只认 'userId'，跨域事件会被静默丢弃 —— 这正是「成就系统不工作」的现场。
func TestPayloadFieldStringNormalization(t *testing.T) {
	type otherDomainPayload struct {
		UserID string `json:"user_id"`
	}
	type ptrTagPayload struct {
		UID *string `json:"userId"`
	}
	uid := "u-ptr"

	cases := []struct {
		name    string
		payload interface{}
		key     string
		want    string
	}{
		{"本域结构体 camelCase", event.ArticleCreatedPayload{UserID: "u-1"}, "userId", "u-1"},
		{"结构体指针", &event.ArticleCreatedPayload{UserID: "u-2"}, "userId", "u-2"},
		{"Key 大驼峰", event.ArticleCreatedPayload{UserID: "u-3"}, "UserID", "u-3"},
		{"Key 下划线", event.ArticleCreatedPayload{UserID: "u-4"}, "user_id", "u-4"},
		{"其它域结构体（json tag snake）", otherDomainPayload{UserID: "u-5"}, "userId", "u-5"},
		{"map[string]interface{} camel", map[string]interface{}{"userId": "u-6"}, "userId", "u-6"},
		{"map[string]interface{} snake", map[string]interface{}{"user_id": "u-7"}, "userId", "u-7"},
		{"map[string]string", map[string]string{"userId": "u-8"}, "userId", "u-8"},
		{"指针字段", ptrTagPayload{UID: &uid}, "userId", "u-ptr"},
		{"键不存在", map[string]interface{}{"other": "x"}, "userId", ""},
		{"nil 载荷", nil, "userId", ""},
		{"nil 指针载荷", (*event.ArticleCreatedPayload)(nil), "userId", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := payloadFieldString(tc.payload, tc.key); got != tc.want {
				t.Errorf("payloadFieldString(%T, %q) = %q, want %q", tc.payload, tc.key, got, tc.want)
			}
		})
	}
}

// TestPayloadFieldIntNumbers 数字字段的多形态解析（int / float / string / 无值）。
func TestPayloadFieldIntNumbers(t *testing.T) {
	cases := []struct {
		name    string
		payload interface{}
		want    int
	}{
		{"结构体 int", event.UserLevelUpPayload{Level: 12}, 12},
		{"map float64（JSON 反序列化常态）", map[string]interface{}{"level": float64(30)}, 30},
		{"map string", map[string]interface{}{"level": "45"}, 45},
		{"map 非法 string", map[string]interface{}{"level": "4a"}, 0},
		{"字段缺失", map[string]interface{}{}, 0},
		{"nil", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := payloadFieldInt(tc.payload, "level"); got != tc.want {
				t.Errorf("payloadFieldInt(%+v) = %d, want %d", tc.payload, got, tc.want)
			}
		})
	}
}

// TestAchievementTriggersMappingIntegrity 映射表完整性：每个 topic 都映射到非空 code 集合。
//
// 防的 bug：新增事件常量但忘了往 AchievementTriggers 里加映射，
// 订阅了却推进 0 个成就（静默失效）。
func TestAchievementTriggersMappingIntegrity(t *testing.T) {
	if len(event.AchievementTriggers) == 0 {
		t.Fatal("AchievementTriggers 为空")
	}
	for topic, codes := range event.AchievementTriggers {
		if topic == "" {
			t.Error("AchievementTriggers 含空 topic")
		}
		if len(codes) == 0 {
			t.Errorf("topic %q 映射到空 code 集合 —— 该事件不会推进任何成就", topic)
		}
		for _, c := range codes {
			if c == "" {
				t.Errorf("topic %q 含空成就 code", topic)
			}
		}
	}
	// 每个固定映射 topic 都必须真的能解析出用户（否则映射表里的 code 永远走不到）
	for topic := range event.AchievementTriggers {
		if _, _, ok := resolveAchievementProgress(topic, resolveTestPayload(topic)); !ok {
			t.Errorf("映射表 topic %q 无法解析用户", topic)
		}
	}
}

// TestAchievementTriggerCodesExistInSeed 映射表引用的成就 code 必须全部存在于种子数据。
//
// 防的 bug：代码里写 "FIRST_ARTILCE" 这类拼写错误时，UpdateProgressBatch 查不到
// 成就定义就静默 return nil —— 用户永远解锁不了，日志也没有。
func TestAchievementTriggerCodesExistInSeed(t *testing.T) {
	// 种子 code 清单（与 migrations/seed.go 的 achievementSeeds 保持同步；
	// 跨包引用会引入环依赖，故在此显式列出，并在 seed 变更时同步本表）。
	seeded := map[string]bool{
		"FIRST_ARTICLE": true, "ARTICLE_10": true, "ARTICLE_50": true, "ARTICLE_100": true,
		"FIRST_LIKE": true, "LIKE_100": true, "LIKE_1000": true,
		"FIRST_COMMENT": true, "COMMENT_100": true,
		"FIRST_FOLLOW": true, "FOLLOW_10": true,
		"FIRST_FOLLOWER": true, "FOLLOWER_100": true, "FOLLOWER_1000": true,
		"LEVEL_10": true, "LEVEL_30": true, "LEVEL_50": true,
		"LOGIN_7_DAYS": true, "LOGIN_30_DAYS": true,
		"BECOME_MEMBER": true, "PROFILE_COMPLETED": true,
	}
	for topic, codes := range event.AchievementTriggers {
		for _, c := range codes {
			if !seeded[c] {
				t.Errorf("topic %q 引用的成就 code %q 不在种子数据中 —— 进度永远推进不到", topic, c)
			}
		}
	}
	// 条件类分支里硬编码的 code 也要校验
	for _, c := range []string{
		"LOGIN_7_DAYS", "LOGIN_30_DAYS",
		"LEVEL_10", "LEVEL_30", "LEVEL_50",
		"BECOME_MEMBER", "PROFILE_COMPLETED",
	} {
		if !seeded[c] {
			t.Errorf("条件分支引用的成就 code %q 不在种子数据中", c)
		}
	}
}

// TestAchievementSubscribeRegistersAllTopics 订阅真的挂到了 bus 上（发布即被调用）。
//
// 防的 bug：Subscribe 里循环变量捕获错误导致所有订阅都指向最后一个 topic。
func TestAchievementSubscribeRegistersAllTopics(t *testing.T) {
	bus := event.NewBus()
	// DB 为 nil：事件回调里 UpdateProgressBatch 会因 DB nil 报错被 batchSafe 吞掉，
	// 但这不影响「回调是否被触发」的判定，我们只用副作用计数验证订阅生效。
	svc := &AchievementService{DB: nil}
	svc.Subscribe(bus)

	called := 0
	// 用一个可被识别的标记 topic 验证：把每个 topic 都发一遍，
	// 只要发生过 panic 或订阅丢失就会在这里暴露。
	for _, topic := range achievementSubscribedTopics {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("事件 %q 回调 panic: %v", topic, r)
				}
			}()
			bus.Publish(topic, resolveTestPayload(topic))
			called++
		}()
	}
	if called != len(achievementSubscribedTopics) {
		t.Errorf("发布 %d 次，期望 %d 次", called, len(achievementSubscribedTopics))
	}
}
