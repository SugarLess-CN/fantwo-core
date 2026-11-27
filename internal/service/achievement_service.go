// 成就服务：成就定义 CRUD、用户进度推进、奖励领取、事件订阅。
//
// 对齐原 achievement.service.ts + achievement-event.service.ts：
//   - 进度推进走进程内事件总线（internal/event.Bus，语义等同 NestJS
//     EventEmitter2 默认 emit：同步顺序调用订阅方）；
//   - 一次事件可同时推进多个成就（updateProgressBatch 语义），
//     成就定义与用户进度各查一次，避免逐条往返；
//   - 达成条件时发 achievement.completed，并自动授予成就勋章装饰品。
//
// 跨域边界：成就奖励里的「经验」「装饰品」通过事件交给对应用户域/装饰品域，
// 成就域只直接发积分（同属运营域内部的积分服务）。
package service

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// AchievementService 成就业务。
type AchievementService struct {
	DB *gorm.DB

	achievements repository.AchievementRepo
	userAch      repository.UserAchievementRepo
	points       *PointsService
	decorations  *DecorationService

	// bus 事件总线（可为 nil：单测/降级场景不发事件）。
	bus *event.Bus
}

// NewAchievementService 构造；bus 可传 event.Default()。
func NewAchievementService(db *gorm.DB, points *PointsService, decorations *DecorationService, bus *event.Bus) *AchievementService {
	return &AchievementService{
		DB:          db,
		points:      points,
		decorations: decorations,
		bus:         bus,
	}
}

// Create 创建成就（code 应用层唯一）。
func (s *AchievementService) Create(in model.AchievementInput) (*model.Achievement, error) {
	if in.Code == nil || *in.Code == "" {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonMissingField, "code")
	}
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonMissingField, "name")
	}
	exists, err := s.achievements.FindByCode(s.DB, *in.Code)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return nil, errcode.ErrBadRequest.WithKeyf(i18n.AchievementCodeExists, *in.Code)
	}

	a := &model.Achievement{
		Code:    *in.Code,
		Name:    *in.Name,
		Type:    model.AchievementTypeSpecial,
		Rarity:  model.AchievementRarityCommon,
		Enabled: true,
	}
	applyAchievementInput(a, in)
	if err := s.achievements.Create(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Update 更新成就。
func (s *AchievementService) Update(id string, in model.AchievementInput) (*model.Achievement, error) {
	a, err := s.achievements.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.AchievementNotFound)
	}
	if in.Code != nil && *in.Code != "" && *in.Code != a.Code {
		other, err := s.achievements.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if other != nil {
			return nil, errcode.ErrBadRequest.WithKeyf(i18n.AchievementCodeExists, *in.Code)
		}
	}
	applyAchievementInput(a, in)
	if err := s.achievements.Save(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Delete 删除成就（软删；用户进度保留以示历史）。
func (s *AchievementService) Delete(id string) error {
	a, err := s.achievements.FindByID(s.DB, id)
	if err != nil {
		return err
	}
	if a == nil {
		return errcode.ErrNotFound.WithKey(i18n.AchievementNotFound)
	}
	return s.achievements.Delete(s.DB, id)
}

// List 成就列表：合并当前用户进度；隐藏成就仅在有进度时可见。
// userID 为空表示未登录（只返回非隐藏成就）。
func (s *AchievementService) List(userID, keyword, sortBy, sortOrder string) ([]model.AchievementView, error) {
	list, err := s.achievements.List(s.DB, keyword, sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	if userID == "" {
		views := make([]model.AchievementView, 0, len(list))
		for _, a := range list {
			if a.Hidden {
				continue
			}
			views = append(views, model.AchievementView{Achievement: a})
		}
		return views, nil
	}

	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	uas, err := s.userAch.FindByUserAndAchievements(s.DB, userID, ids)
	if err != nil {
		return nil, err
	}
	byAch := make(map[string]model.UserAchievement, len(uas))
	for _, ua := range uas {
		byAch[ua.AchievementID] = ua
	}

	views := make([]model.AchievementView, 0, len(list))
	for _, a := range list {
		ua, ok := byAch[a.ID]
		if a.Hidden && !ok {
			continue // 隐藏且无进度：不展示
		}
		views = append(views, model.AchievementView{
			Achievement: a,
			Progress:    ua.Progress,
			Completed:   ua.Completed,
			CompletedAt: ua.CompletedAt,
			Claimed:     ua.Claimed,
			ClaimedAt:   ua.ClaimedAt,
		})
	}
	return views, nil
}

// Get 成就详情（含当前用户进度）。
func (s *AchievementService) Get(id, userID string) (*model.AchievementView, error) {
	a, err := s.achievements.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.AchievementNotFound)
	}
	view := &model.AchievementView{Achievement: *a}
	if userID == "" {
		return view, nil
	}
	ua, err := s.userAch.FindByUserAndAchievement(s.DB, userID, id)
	if err != nil {
		return nil, err
	}
	if ua != nil {
		view.Progress = ua.Progress
		view.Completed = ua.Completed
		view.CompletedAt = ua.CompletedAt
		view.Claimed = ua.Claimed
		view.ClaimedAt = ua.ClaimedAt
	}
	return view, nil
}

// AchievementStats 用户成就统计（对齐原 getUserStats）。
type AchievementStats struct {
	Total          int64  `json:"total"`
	Completed      int64  `json:"completed"`
	Claimed        int64  `json:"claimed"`
	CompletionRate string `json:"completion_rate"`
}

// Stats 统计。
func (s *AchievementService) Stats(userID string) (*AchievementStats, error) {
	total, err := s.achievements.CountEnabled(s.DB)
	if err != nil {
		return nil, err
	}
	completed, err := s.userAch.Count(s.DB, userID, true, false)
	if err != nil {
		return nil, err
	}
	claimed, err := s.userAch.Count(s.DB, userID, false, true)
	if err != nil {
		return nil, err
	}
	return &AchievementStats{
		Total:          total,
		Completed:      completed,
		Claimed:        claimed,
		CompletionRate: rate(completed, total),
	}, nil
}

// UpdateProgress 按成就代码推进进度（单条）。
func (s *AchievementService) UpdateProgress(userID, code string, increment int) error {
	return s.UpdateProgressBatch(userID, []string{code}, increment)
}

// UpdateProgressBatch 批量推进（同一事件推进多个成就，对齐原实现）。
func (s *AchievementService) UpdateProgressBatch(userID string, codes []string, increment int) error {
	if userID == "" || len(codes) == 0 {
		return nil
	}
	if increment <= 0 {
		increment = 1
	}
	// 去重并保序
	seen := make(map[string]struct{}, len(codes))
	unique := make([]string, 0, len(codes))
	for _, c := range codes {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		unique = append(unique, c)
	}
	if len(unique) == 0 {
		// 全是空串：等价于没有要推进的成就，直接短路（不查库）
		return nil
	}
	if s.DB == nil {
		// 降级启动 / 单测环境：不 panic，返回明确错误（调用方只记日志）
		return errcode.ErrInternal.WithKey(i18n.CommonDependencyUnavailable)
	}

	defs, err := s.achievements.FindByCodes(s.DB, unique)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(defs))
	byCode := make(map[string]model.Achievement, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
		byCode[d.Code] = d
	}
	uas, err := s.userAch.FindByUserAndAchievements(s.DB, userID, ids)
	if err != nil {
		return err
	}
	byAch := make(map[string]*model.UserAchievement, len(uas))
	for i := range uas {
		byAch[uas[i].AchievementID] = &uas[i]
	}

	for _, code := range unique {
		def, ok := byCode[code]
		if !ok {
			continue
		}
		if err := s.applyProgress(userID, def, byAch[def.ID], increment); err != nil {
			return err
		}
	}
	return nil
}

// applyProgress 推进单个成就进度，达成时发事件并授予勋章。
func (s *AchievementService) applyProgress(userID string, def model.Achievement, existing *model.UserAchievement, increment int) error {
	ua := existing
	if ua == nil {
		ua = &model.UserAchievement{UserID: userID, AchievementID: def.ID}
	}
	if ua.Completed {
		return nil // 已完成不再推进
	}

	ua.Progress += increment
	if ua.Progress >= def.Condition.TargetValue() {
		ua.Completed = true
		now := time.Now()
		ua.CompletedAt = &now
	}
	if err := s.userAch.Save(s.DB, ua); err != nil {
		return err
	}

	if ua.Completed && ua.CompletedAt != nil {
		// 授予勋章装饰品（运营域内部调用；失败不阻断进度落地）
		if s.decorations != nil {
			if _, err := s.decorations.GrantAchievementBadge(userID, def); err != nil {
				log.Printf("[WARN] 授予成就勋章失败 user=%s achievement=%s err=%v", userID, def.Code, err)
			}
		}
		s.publish(event.EventAchievementCompleted, event.AchievementCompletedPayload{
			UserID:          userID,
			AchievementID:   def.ID,
			AchievementCode: def.Code,
		})
	}
	return nil
}

// ClaimReward 领取单个成就奖励（积分立即发放；经验/装饰品走事件）。
func (s *AchievementService) ClaimReward(userID, achievementID string) (*ClaimResult, error) {
	ua, err := s.userAch.FindByUserAndAchievement(s.DB, userID, achievementID)
	if err != nil {
		return nil, err
	}
	if reject := achievementClaimRejection(ua); reject != nil {
		return nil, reject
	}
	def, err := s.achievements.FindByID(s.DB, achievementID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.AchievementNotFound)
	}

	// 积分奖励：同域直接发放
	if def.RewardPoints > 0 && s.points != nil {
		if _, err := s.points.Add(userID, AddPointsInput{
			Amount:      def.RewardPoints,
			Source:      "ACHIEVEMENT",
			Description: "Achievement reward: " + def.Name,
			RelatedType: "achievement",
			RelatedID:   def.ID,
		}); err != nil {
			return nil, err
		}
	}
	// 经验奖励：交给用户域
	if def.RewardExp > 0 {
		s.publish(event.EventUserGainExp, event.UserGainExpPayload{
			UserID: userID,
			Exp:    def.RewardExp,
			Reason: "Achievement reward: " + def.Name,
		})
	}
	// 装饰品奖励：交给装饰品域
	if def.RewardDecorationID != "" {
		s.publish(event.EventDecorationGrant, event.DecorationGrantPayload{
			UserID:       userID,
			DecorationID: def.RewardDecorationID,
			ObtainMethod: model.DecorationObtainAchievement,
		})
	}

	now := time.Now()
	ua.Claimed = true
	ua.ClaimedAt = &now
	if err := s.userAch.Save(s.DB, ua); err != nil {
		return nil, err
	}
	return &ClaimResult{
		Points:       def.RewardPoints,
		Exp:          def.RewardExp,
		DecorationID: def.RewardDecorationID,
	}, nil
}

// achievementClaimRejection 成就奖励领取的前置判定，返回 nil 表示可领。
//
// 幂等三态（与任务奖励一致）：无进度记录 → 不存在；未完成 → 拒绝；
// 已领取 → 拒绝（重复领取不会重复发积分/经验/装饰品）。
func achievementClaimRejection(ua *model.UserAchievement) *errcode.Err {
	if ua == nil {
		return errcode.ErrNotFound.WithKey(i18n.AchievementNotAvailable)
	}
	if !ua.Completed {
		return errcode.ErrBadRequest.WithKey(i18n.AchievementNotCompleted)
	}
	if ua.Claimed {
		return errcode.ErrBadRequest.WithKey(i18n.CommonRewardAlreadyClaimed)
	}
	return nil
}

// ClaimResult 领取结果。
type ClaimResult struct {
	Points       int    `json:"points"`
	Exp          int    `json:"exp"`
	DecorationID string `json:"decoration_id,omitempty"`
}

// ClaimAllResult 一键领取结果。
type ClaimAllResult struct {
	Claimed int `json:"claimed"`
	Total   int `json:"total"`
}

// ClaimAll 一键领取全部可领成就奖励（单条失败不影响其余，对齐原实现）。
func (s *AchievementService) ClaimAll(userID string) (*ClaimAllResult, error) {
	list, err := s.userAch.ListClaimable(s.DB, userID)
	if err != nil {
		return nil, err
	}
	result := &ClaimAllResult{Total: len(list)}
	for _, ua := range list {
		if _, err := s.ClaimReward(userID, ua.AchievementID); err != nil {
			log.Printf("[WARN] 领取成就奖励失败 user=%s achievement=%s err=%v", userID, ua.AchievementID, err)
			continue
		}
		result.Claimed++
	}
	return result, nil
}

// achievementSubscribedTopics 成就域订阅的全部事件（唯一事实来源，单测据此断言覆盖）。
var achievementSubscribedTopics = []string{
	// 内容域发布
	event.EventArticleCreated,
	event.EventArticleReceivedLike,
	event.EventCommentCreated,
	// 用户域发布
	event.EventUserFollowed,
	event.EventUserReceivedFollow,
	event.EventUserDailyLogin,
	event.EventUserLevelUp,
	event.EventUserProfileCompleted,
	// 交易域发布
	event.EventMembershipPurchased,
	event.EventOrderPaid,
	// 运营域自身（邀请码被使用 → 邀请达人成就 + 邀请积分）
	event.EventInviteUsed,
}

// Subscribe 注册成就事件订阅（对齐原 achievement-event.service.ts 的 @OnEvent）。
// 由装配处（NewOps 的 sync.Once）调用一次；重复调用会重复推进，调用方需保证只调一次。
//
// 载荷兼容性（关键）：发布方可能用本域结构体、自己的结构体、map[string]interface{}、
// map[string]string（audit_service 就这么发）—— 取值统一走反射 + 键名归一化，
// 「userId / user_id / UserID」等价，避免跨域载荷形态差异导致事件静默丢失。
func (s *AchievementService) Subscribe(bus *event.Bus) {
	if bus == nil {
		return
	}
	s.bus = bus

	for _, topic := range achievementSubscribedTopics {
		t := topic
		bus.Subscribe(t, func(payload interface{}) {
			userID, codes, ok := resolveAchievementProgress(t, payload)
			if !ok || userID == "" {
				// 解析失败要显式报出来：静默丢弃正是「成就系统不工作」的现场
				log.Printf("[WARN] 成就事件载荷无法解析 topic=%s type=%T payload=%+v", t, payload, payload)
				return
			}
			if len(codes) > 0 {
				if err := s.UpdateProgressBatch(userID, codes, 1); err != nil {
					log.Printf("[WARN] 推进成就失败 topic=%s user=%s codes=%v err=%v", t, userID, codes, err)
				}
			}
			s.onAchievementEventSideEffect(t, userID, payload)
		})
	}
}

// onAchievementEventSideEffect 事件附带动作（除成就进度外的副作用）。
func (s *AchievementService) onAchievementEventSideEffect(topic, userID string, payload interface{}) {
	if s.points == nil {
		return
	}
	switch topic {
	case event.EventOrderPaid:
		// 支付成功送积分：数据驱动，活动表存在启用的 ORDER_PAID 才发（未配置静默跳过）
		orderID := payloadFieldString(payload, "orderId")
		if _, _, err := s.points.TryAddByRule(userID, "ORDER_PAID", "order", orderID); err != nil {
			log.Printf("[WARN] 支付成功积分发放失败 user=%s order=%s err=%v", userID, orderID, err)
		}
	case event.EventInviteUsed:
		// 邀请成功给邀请人加积分（活动 INVITE_USER），与「邀请达人」成就同源不同奖励
		inviteID := payloadFieldString(payload, "inviteId")
		if _, _, err := s.points.TryAddByRule(userID, "INVITE_USER", "invite", inviteID); err != nil {
			log.Printf("[WARN] 邀请成功积分发放失败 user=%s invite=%s err=%v", userID, inviteID, err)
		}
	}
}

// resolveAchievementProgress 把「事件 + 载荷」解析成 (要推进的用户, 成就代码集, 是否可推进)。
//
// 语义要点（对齐原 achievement-event.service.ts）：
//   - article.receivedLike 推进的是**文章作者**（AuthorID），不是点赞者；
//   - user.receivedFollow 推进的是**被关注者**（优先 targetUserId，退回 userId）；
//   - user.followed 推进的是**关注者**（userId）；
//   - 其余事件推进的都是 payload.userId 本人。
func resolveAchievementProgress(topic string, payload interface{}) (string, []string, bool) {
	switch topic {
	case event.EventArticleCreated:
		uid := payloadFieldString(payload, "userId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventArticleReceivedLike:
		// 只认 authorId：取不到就放弃，绝不退回 userId（那会推进点赞者，成就归属错人）
		uid := payloadFieldString(payload, "authorId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventCommentCreated:
		uid := payloadFieldString(payload, "userId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventUserFollowed:
		uid := payloadFieldString(payload, "userId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventUserReceivedFollow:
		// 被关注者：优先显式 targetUserId，退回 userId
		uid := payloadFieldString(payload, "targetUserId")
		if uid == "" {
			uid = payloadFieldString(payload, "userId")
		}
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventMembershipPurchased:
		uid := payloadFieldString(payload, "userId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventUserProfileCompleted:
		uid := payloadFieldString(payload, "userId")
		return uid, event.AchievementTriggers[topic], uid != ""

	case event.EventUserDailyLogin:
		uid := payloadFieldString(payload, "userId")
		days := payloadFieldInt(payload, "consecutiveDays")
		var codes []string
		if days >= 7 {
			codes = append(codes, "LOGIN_7_DAYS")
		}
		if days >= 30 {
			codes = append(codes, "LOGIN_30_DAYS")
		}
		return uid, codes, uid != ""

	case event.EventUserLevelUp:
		uid := payloadFieldString(payload, "userId")
		level := payloadFieldInt(payload, "level")
		var codes []string
		if level >= 10 {
			codes = append(codes, "LEVEL_10")
		}
		if level >= 30 {
			codes = append(codes, "LEVEL_30")
		}
		if level >= 50 {
			codes = append(codes, "LEVEL_50")
		}
		return uid, codes, uid != ""

	case event.EventInviteUsed:
		// 邀请成功：种子成就表里没有「邀请类」成就（INVITE_5_USERS_ACHIEVEMENT 是
		// 积分活动的 code，不是成就），所以这里不推进成就，只有 side effect 发积分。
		// 若日后补了邀请成就，在这里返回其 code 即可。
		uid := payloadFieldString(payload, "inviterId")
		return uid, nil, uid != ""

	case event.EventOrderPaid:
		uid := payloadFieldString(payload, "userId")
		var codes []string
		if payloadFieldString(payload, "type") == model.OrderTypeMembership {
			codes = []string{"BECOME_MEMBER"}
		}
		return uid, codes, uid != ""
	}
	return "", nil, false
}

// payloadFieldString 从任意事件载荷取值（string）。
// 支持：本域结构体、其它域结构体、struct 指针、map[string]interface{}、map[string]string、
// json.RawMessage 之外的常见形态。键名归一化后比较（去下划线 + 转小写），
// 因此 userId / user_id / UserID / USER_ID 等价。
func payloadFieldString(payload interface{}, names ...string) string {
	v, ok := payloadFieldValue(payload, names...)
	if !ok {
		return ""
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Interface:
		if s, ok := v.Interface().(string); ok {
			return s
		}
	case reflect.Slice: // []byte
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return string(v.Bytes())
		}
	}
	// 兜底：实现 fmt.Stringer
	if str, ok := v.Interface().(fmt.Stringer); ok {
		return str.String()
	}
	return ""
}

// payloadFieldInt 从任意事件载荷取值（int）。
func payloadFieldInt(payload interface{}, names ...string) int {
	v, ok := payloadFieldValue(payload, names...)
	if !ok {
		return 0
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(v.Uint())
	case reflect.Float32, reflect.Float64:
		return int(v.Float())
	case reflect.String:
		n := 0
		for _, r := range v.String() {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int(r-'0')
		}
		return n
	}
	return 0
}

// payloadFieldValue 反射取值：指针/接口解引用 → map 按键 → struct 按字段名或 json tag。
func payloadFieldValue(payload interface{}, names ...string) (reflect.Value, bool) {
	if payload == nil || len(names) == 0 {
		return reflect.Value{}, false
	}
	want := make(map[string]struct{}, len(names))
	for _, n := range names {
		want[normFieldKey(n)] = struct{}{}
	}

	v := reflect.ValueOf(payload)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, false
		}
		iter := v.MapRange()
		for iter.Next() {
			if _, ok := want[normFieldKey(iter.Key().String())]; ok {
				return unwrapValue(iter.Value()), true
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if _, ok := want[normFieldKey(f.Name)]; ok {
				return unwrapValue(v.Field(i)), true
			}
			if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
				if comma := strings.IndexByte(tag, ','); comma >= 0 {
					tag = tag[:comma]
				}
				if tag != "" {
					if _, ok := want[normFieldKey(tag)]; ok {
						return unwrapValue(v.Field(i)), true
					}
				}
			}
		}
	}
	return reflect.Value{}, false
}

// unwrapValue 解包 interface / 指针包装（map 取值与 *string 字段都受益）。
func unwrapValue(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}
	return v
}

// normFieldKey 键名归一化：去下划线/连字符 + 转小写，使 snake_case 与 camelCase 等价。
func normFieldKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// batchSafe 容错的批量推进（订阅侧统一入口，错误只记日志）。
func (s *AchievementService) batchSafe(userID string, codes []string, topic string) {
	if userID == "" || len(codes) == 0 {
		return
	}
	if err := s.UpdateProgressBatch(userID, codes, 1); err != nil {
		log.Printf("[WARN] 推进成就失败 topic=%s user=%s err=%v", topic, userID, err)
	}
}

// publish 事件发布（bus 为 nil 时静默跳过）。
func (s *AchievementService) publish(topic string, payload interface{}) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(topic, payload)
}

// applyAchievementInput 非 nil 字段写入（PATCH 语义）。
func applyAchievementInput(a *model.Achievement, in model.AchievementInput) {
	if in.Code != nil {
		a.Code = *in.Code
	}
	if in.Name != nil {
		a.Name = *in.Name
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if in.Icon != nil {
		a.Icon = *in.Icon
	}
	if in.Type != nil {
		a.Type = *in.Type
	}
	if in.Rarity != nil {
		a.Rarity = *in.Rarity
	}
	if in.Condition != nil {
		a.Condition = *in.Condition
	}
	if in.RewardPoints != nil {
		a.RewardPoints = *in.RewardPoints
	}
	if in.RewardExp != nil {
		a.RewardExp = *in.RewardExp
	}
	if in.RewardDecorationID != nil {
		a.RewardDecorationID = *in.RewardDecorationID
	}
	if in.Hidden != nil {
		a.Hidden = *in.Hidden
	}
	if in.Sort != nil {
		a.Sort = *in.Sort
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
}
