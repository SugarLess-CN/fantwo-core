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
	"log"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
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
		return nil, errcode.ErrBadRequest.With("成就代码不能为空")
	}
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.With("成就名称不能为空")
	}
	exists, err := s.achievements.FindByCode(s.DB, *in.Code)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return nil, errcode.ErrBadRequest.With("成就代码已存在")
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
		return nil, errcode.ErrNotFound.With("成就不存在")
	}
	if in.Code != nil && *in.Code != "" && *in.Code != a.Code {
		other, err := s.achievements.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if other != nil {
			return nil, errcode.ErrBadRequest.With("成就代码已存在")
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
		return errcode.ErrNotFound.With("成就不存在")
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
		return nil, errcode.ErrNotFound.With("成就不存在")
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
		return nil
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
	if ua == nil {
		return nil, errcode.ErrNotFound.With("成就不存在或尚未开始")
	}
	if !ua.Completed {
		return nil, errcode.ErrBadRequest.With("成就尚未完成")
	}
	if ua.Claimed {
		return nil, errcode.ErrBadRequest.With("奖励已领取")
	}
	def, err := s.achievements.FindByID(s.DB, achievementID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, errcode.ErrNotFound.With("成就不存在")
	}

	// 积分奖励：同域直接发放
	if def.RewardPoints > 0 && s.points != nil {
		if _, err := s.points.Add(userID, AddPointsInput{
			Amount:      def.RewardPoints,
			Source:      "ACHIEVEMENT",
			Description: "完成成就：" + def.Name,
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
			Reason: "完成成就：" + def.Name,
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

// Subscribe 注册成就事件订阅（对齐原 achievement-event.service.ts 的 @OnEvent）。
// 由路由/启动装配处调用一次；重复调用会产生重复推进，调用方需保证只调一次。
func (s *AchievementService) Subscribe(bus *event.Bus) {
	if bus == nil {
		return
	}
	s.bus = bus

	// 固定映射类事件：直接用代码表推进
	for topic, codes := range event.AchievementTriggers {
		topicCopy, codesCopy := topic, codes
		bus.Subscribe(topicCopy, func(payload interface{}) {
			userID := payloadUserID(topicCopy, payload)
			if userID == "" {
				return
			}
			if err := s.UpdateProgressBatch(userID, codesCopy, 1); err != nil {
				log.Printf("[WARN] 推进成就失败 topic=%s user=%s err=%v", topicCopy, userID, err)
			}
		})
	}

	// 条件类事件：按 payload 阈值决定推进哪些成就
	bus.Subscribe(event.EventUserDailyLogin, func(payload interface{}) {
		p, ok := asDailyLogin(payload)
		if !ok {
			return
		}
		var codes []string
		if p.ConsecutiveDays >= 7 {
			codes = append(codes, "LOGIN_7_DAYS")
		}
		if p.ConsecutiveDays >= 30 {
			codes = append(codes, "LOGIN_30_DAYS")
		}
		s.batchSafe(p.UserID, codes, "user.dailyLogin")
	})

	bus.Subscribe(event.EventUserLevelUp, func(payload interface{}) {
		p, ok := asLevelUp(payload)
		if !ok {
			return
		}
		var codes []string
		if p.Level >= 10 {
			codes = append(codes, "LEVEL_10")
		}
		if p.Level >= 30 {
			codes = append(codes, "LEVEL_30")
		}
		if p.Level >= 50 {
			codes = append(codes, "LEVEL_50")
		}
		s.batchSafe(p.UserID, codes, "user.levelUp")
	})
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

// payloadUserID 从各事件载荷里取「要推进成就的用户」。
// 语义对齐原实现：文章被点赞/被关注推进的是内容的作者（被赞/被关注方）。
func payloadUserID(topic string, payload interface{}) string {
	switch topic {
	case event.EventArticleCreated:
		if p, ok := payload.(event.ArticleCreatedPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.ArticleCreatedPayload); ok && p != nil {
			return p.UserID
		}
	case event.EventArticleReceivedLike:
		if p, ok := payload.(event.ArticleReceivedLikePayload); ok {
			return p.AuthorID
		}
		if p, ok := payload.(*event.ArticleReceivedLikePayload); ok && p != nil {
			return p.AuthorID
		}
	case event.EventCommentCreated:
		if p, ok := payload.(event.CommentCreatedPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.CommentCreatedPayload); ok && p != nil {
			return p.UserID
		}
	case event.EventUserFollowed:
		if p, ok := payload.(event.UserFollowedPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.UserFollowedPayload); ok && p != nil {
			return p.UserID
		}
	case event.EventUserReceivedFollow:
		if p, ok := payload.(event.UserReceivedFollowPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.UserReceivedFollowPayload); ok && p != nil {
			return p.UserID
		}
	case event.EventMembershipPurchased:
		if p, ok := payload.(event.MembershipPurchasedPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.MembershipPurchasedPayload); ok && p != nil {
			return p.UserID
		}
	case event.EventUserProfileCompleted:
		if p, ok := payload.(event.GenericPayload); ok {
			return p.UserID
		}
		if p, ok := payload.(*event.GenericPayload); ok && p != nil {
			return p.UserID
		}
	}
	return ""
}

// asDailyLogin 载荷断言。
func asDailyLogin(payload interface{}) (event.UserDailyLoginPayload, bool) {
	if p, ok := payload.(event.UserDailyLoginPayload); ok {
		return p, true
	}
	if p, ok := payload.(*event.UserDailyLoginPayload); ok && p != nil {
		return *p, true
	}
	return event.UserDailyLoginPayload{}, false
}

// asLevelUp 载荷断言。
func asLevelUp(payload interface{}) (event.UserLevelUpPayload, bool) {
	if p, ok := payload.(event.UserLevelUpPayload); ok {
		return p, true
	}
	if p, ok := payload.(*event.UserLevelUpPayload); ok && p != nil {
		return *p, true
	}
	return event.UserLevelUpPayload{}, false
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
