// Package service 用户域经验/等级与事件发布（T-32 事件契约补齐）。
//
// 等级规则对齐原 level.service.ts 的 LEVEL_CONFIG（0-6 档，累计经验门槛）：
//
//	0:0  1:100  2:300  3:600  4:1000  5:1500  6:2100
//
// 原仓库上限即 6 级，此处不擅自延伸档位（成就 LEVEL_10/30/50 在原项目同样不可达）。
package service

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"gorm.io/gorm"
)

// levelStep 原 LEVEL_CONFIG 的一档（累计经验门槛）。
type levelStep struct {
	Level    int
	TotalExp int
}

// levelConfig 对齐原 LEVEL_CONFIG（升序遍历）。
var levelConfig = []levelStep{
	{0, 0}, {1, 100}, {2, 300}, {3, 600}, {4, 1000}, {5, 1500}, {6, 2100},
}

// calculateLevel 由累计经验算等级（对齐原 calculateLevel：取命中最高档）。
func calculateLevel(totalExp int) int {
	for i := len(levelConfig) - 1; i >= 0; i-- {
		if totalExp >= levelConfig[i].TotalExp {
			return levelConfig[i].Level
		}
	}
	return 0
}

// AddExperienceInput 经验增加入参。
type AddExperienceInput struct {
	UserID string
	Amount int
	Source string
}

// AddExperience 增加经验并在事务提交后发布升级事件。
// 返回旧/新等级与是否升级。
func (s *UserService) AddExperience(ctx context.Context, in AddExperienceInput) (oldLevel, newLevel int, leveledUp bool, e *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return 0, 0, false, e
	}
	if in.UserID == "" || in.Amount <= 0 {
		return 0, 0, false, errcode.ErrInvalidParam
	}
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		user, err := s.d.Users.FindPlainByID(tx, in.UserID)
		if err != nil {
			return err
		}
		oldLevel = user.Level
		totalExp := user.Experience + in.Amount
		newLevel = calculateLevel(totalExp)
		fields := map[string]interface{}{
			"experience": gorm.Expr("experience + ?", in.Amount),
		}
		if newLevel != oldLevel {
			fields["level"] = newLevel
		}
		return s.d.Users.UpdateFields(tx, in.UserID, fields)
	}); err != nil {
		if IsNotFound(err) {
			return 0, 0, false, errcode.ErrUserNotExist
		}
		s.d.Logger.Warn("增加经验失败 user=" + in.UserID)
		return 0, 0, false, errcode.ErrInternal
	}
	leveledUp = newLevel > oldLevel
	if leveledUp {
		// 事务已提交，发布升级事件（成就域据 Level 阈值推进 LEVEL_10/30/50）。
		publishLevelUp(in.UserID, newLevel)
	}
	return oldLevel, newLevel, leveledUp, nil
}

// ---------- 事件发布（均在业务事务提交之后调用） ----------

// publishLevelUp 发布 user.levelUp。
func publishLevelUp(userID string, level int) {
	if userID == "" {
		return
	}
	event.Default().Publish(event.EventUserLevelUp, event.UserLevelUpPayload{
		UserID: userID, Level: level,
	})
}

// publishDailyLogin 发布 user.dailyLogin（积分活动 DAILY_LOGIN / 连续登录成就的触发源）。
func publishDailyLogin(userID string, consecutiveDays int) {
	if userID == "" {
		return
	}
	event.Default().Publish(event.EventUserDailyLogin, event.UserDailyLoginPayload{
		UserID: userID, ConsecutiveDays: consecutiveDays,
	})
}

// publishFollowed 发布关注双向事件：
// user.followed（关注者 A 的「首次关注/关注 N 人」成就）+
// user.receivedFollow（被关注者 B 的「首个粉丝/粉丝 N 人」成就）。
func publishFollowed(followerID, targetID string) {
	if followerID == "" || targetID == "" {
		return
	}
	bus := event.Default()
	bus.Publish(event.EventUserFollowed, event.UserFollowedPayload{
		UserID: followerID, TargetUserID: targetID,
	})
	bus.Publish(event.EventUserReceivedFollow, event.UserReceivedFollowPayload{
		UserID: targetID, FollowerID: followerID,
	})
}

// publishProfileCompleted 发布 user.profileCompleted（仅在「不完整 → 完整」跃迁时发一次）。
func publishProfileCompleted(userID string) {
	if userID == "" {
		return
	}
	event.Default().Publish(event.EventUserProfileCompleted, event.GenericPayload{UserID: userID})
}

// isProfileComplete 资料是否补全（昵称 + 头像 + 简介三项关键字段齐备）。
func isProfileComplete(u *model.User) bool {
	if u == nil {
		return false
	}
	hasNickname := u.Nickname != nil && *u.Nickname != ""
	hasAvatar := u.Avatar != nil && *u.Avatar != ""
	hasDescription := u.Description != nil && *u.Description != ""
	return hasNickname && hasAvatar && hasDescription
}
