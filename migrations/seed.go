// 运营域种子数据：积分活动 + 成就定义。
//
// 数据来源：原仓库
//
//	tmp/picart/src/modules/points/points-activities.seed.ts（14 条活动）
//	tmp/picart/src/modules/achievement/achievement.seed.ts（21 条成就）
//
// 幂等：按业务唯一键 code 判断，已存在即跳过（对齐原 onModuleInit 的初始化语义）。
// 调用方式：由 migrate.go 的 Run 末尾在 AutoMigrate 之后调用 SeedOps（可选择跳过）。
package migrations

import (
	"log"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// SeedOps 写入运营域种子数据（幂等，重复调用不产生重复行）。
func SeedOps(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := seedPointsActivities(db); err != nil {
		return err
	}
	if err := seedAchievements(db); err != nil {
		return err
	}
	return nil
}

// pointsActivitySeed 积分活动种子。
type pointsActivitySeed struct {
	Code         string
	Name         string
	Description  string
	Type         string
	RewardPoints int
	TargetCount  int
	DailyLimit   int
	TotalLimit   int
	ValidDays    int
	Icon         string
	Link         string
	Sort         int
}

// pointsActivitySeeds 14 条积分活动（顺序与原 seed 文件一致）。
var pointsActivitySeeds = []pointsActivitySeed{
	{"DAILY_LOGIN", "每日登录", "每天首次登录获得积分", model.PointsActivityInstant, 10, 1, 1, 0, 365, "🎯", "", 1},
	{"PUBLISH_ARTICLE", "发布文章", "发布一篇文章获得积分", model.PointsActivityInstant, 50, 1, 5, 0, 0, "📝", "", 2},
	{"PUBLISH_COMMENT", "发表评论", "发表评论获得积分", model.PointsActivityInstant, 5, 1, 10, 0, 0, "💬", "", 3},
	{"LIKE_COMMENT", "点赞评论", "点赞评论获得积分", model.PointsActivityInstant, 1, 1, 20, 0, 0, "👍", "", 4},
	{"LIKE_ARTICLE", "点赞文章", "点赞文章获得积分", model.PointsActivityInstant, 2, 1, 20, 0, 0, "👍", "", 5},
	{"ARTICLE_RECEIVED_LIKE", "文章被点赞", "发布的文章被他人点赞获得积分", model.PointsActivityInstant, 1, 1, 0, 0, 0, "❤️", "", 6},
	{"ARTICLE_RECEIVED_COMMENT", "文章被评论", "发布的文章被他人评论获得积分", model.PointsActivityInstant, 3, 1, 0, 0, 0, "💭", "", 7},
	{"COMMENT_RECEIVED_LIKE", "评论被点赞", "发表的评论被他人点赞获得积分", model.PointsActivityInstant, 1, 1, 0, 0, 0, "👏", "", 8},
	{"SHARE_ARTICLE", "分享文章", "分享文章获得积分", model.PointsActivityInstant, 10, 1, 5, 0, 0, "📤", "", 9},
	{"INVITE_USER", "邀请用户", "成功邀请一位用户注册", model.PointsActivityInstant, 100, 1, 0, 0, 0, "👥", "/invite", 10},
	{"COMPLETE_PROFILE", "完善资料", "完善个人资料获得积分", model.PointsActivityOnce, 50, 1, 0, 1, 0, "👤", "/profile", 11},
	{"PUBLISH_3_ARTICLES_WEEKLY", "每周发布3篇文章", "本周发布3篇文章获得额外奖励", model.PointsActivityWeekly, 200, 3, 0, 0, 0, "✍️", "/article/create", 21},
	{"COMMENT_10_TIMES_WEEKLY", "每周评论10次", "本周评论10次获得额外奖励", model.PointsActivityWeekly, 100, 10, 0, 0, 0, "💬", "", 22},
	{"INVITE_5_USERS_ACHIEVEMENT", "邀请达人", "累计邀请5位好友注册", model.PointsActivityOnce, 500, 5, 0, 1, 0, "🏆", "/invite", 23},
}

// seedPointsActivities 幂等写入积分活动。
func seedPointsActivities(db *gorm.DB) error {
	for _, s := range pointsActivitySeeds {
		var n int64
		if err := db.Model(&model.PointsActivity{}).Where("code = ?", s.Code).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		a := &model.PointsActivity{
			Code:         s.Code,
			Name:         s.Name,
			Description:  s.Description,
			Type:         s.Type,
			RewardPoints: s.RewardPoints,
			TargetCount:  s.TargetCount,
			DailyLimit:   s.DailyLimit,
			TotalLimit:   s.TotalLimit,
			ValidDays:    s.ValidDays,
			Icon:         s.Icon,
			Link:         s.Link,
			IsActive:     true,
			Sort:         s.Sort,
		}
		if err := db.Create(a).Error; err != nil {
			return err
		}
		log.Printf("[seed] 积分活动已创建: %s %s", s.Code, s.Name)
	}
	return nil
}

// achievementSeed 成就种子。
type achievementSeed struct {
	Code          string
	Name          string
	Description   string
	Type          string
	Rarity        string
	ConditionType string
	Target        int
	Event         string
	RewardPoints  int
	RewardExp     int
	Sort          int
}

// achievementSeeds 21 条成就（顺序与原 seed 文件一致）。
var achievementSeeds = []achievementSeed{
	{"FIRST_ARTICLE", "初出茅庐", "发布第一篇文章", model.AchievementTypeArticle, model.AchievementRarityCommon, "count", 1, "article.created", 10, 50, 1},
	{"ARTICLE_10", "笔耕不辍", "发布10篇文章", model.AchievementTypeArticle, model.AchievementRarityCommon, "count", 10, "article.created", 50, 200, 2},
	{"ARTICLE_50", "著作等身", "发布50篇文章", model.AchievementTypeArticle, model.AchievementRarityRare, "count", 50, "article.created", 200, 1000, 3},
	{"ARTICLE_100", "文坛巨匠", "发布100篇文章", model.AchievementTypeArticle, model.AchievementRarityEpic, "count", 100, "article.created", 500, 3000, 4},
	{"FIRST_LIKE", "初获认可", "文章获得第一个点赞", model.AchievementTypeArticle, model.AchievementRarityCommon, "count", 1, "article.receivedLike", 5, 20, 10},
	{"LIKE_100", "人气作者", "文章累计获得100个点赞", model.AchievementTypeArticle, model.AchievementRarityRare, "count", 100, "article.receivedLike", 100, 500, 11},
	{"LIKE_1000", "万众瞩目", "文章累计获得1000个点赞", model.AchievementTypeArticle, model.AchievementRarityEpic, "count", 1000, "article.receivedLike", 500, 2000, 12},
	{"FIRST_COMMENT", "初次发声", "发表第一条评论", model.AchievementTypeComment, model.AchievementRarityCommon, "count", 1, "comment.created", 5, 20, 20},
	{"COMMENT_100", "热心网友", "发表100条评论", model.AchievementTypeComment, model.AchievementRarityRare, "count", 100, "comment.created", 100, 500, 21},
	{"FIRST_FOLLOW", "结识好友", "关注第一个用户", model.AchievementTypeSocial, model.AchievementRarityCommon, "count", 1, "user.followed", 5, 20, 30},
	{"FOLLOW_10", "社交达人", "关注10个用户", model.AchievementTypeSocial, model.AchievementRarityCommon, "count", 10, "user.followed", 30, 100, 31},
	{"FIRST_FOLLOWER", "初获关注", "获得第一个粉丝", model.AchievementTypeSocial, model.AchievementRarityCommon, "count", 1, "user.receivedFollow", 10, 50, 32},
	{"FOLLOWER_100", "小有名气", "获得100个粉丝", model.AchievementTypeSocial, model.AchievementRarityRare, "count", 100, "user.receivedFollow", 200, 1000, 33},
	{"FOLLOWER_1000", "名声在外", "获得1000个粉丝", model.AchievementTypeSocial, model.AchievementRarityEpic, "count", 1000, "user.receivedFollow", 1000, 5000, 34},
	{"LEVEL_10", "初窥门径", "达到10级", model.AchievementTypeLevel, model.AchievementRarityCommon, "threshold", 10, "user.levelUp", 50, 0, 40},
	{"LEVEL_30", "登堂入室", "达到30级", model.AchievementTypeLevel, model.AchievementRarityRare, "threshold", 30, "user.levelUp", 200, 0, 41},
	{"LEVEL_50", "炉火纯青", "达到50级", model.AchievementTypeLevel, model.AchievementRarityEpic, "threshold", 50, "user.levelUp", 500, 0, 42},
	{"LOGIN_7_DAYS", "坚持不懈", "连续登录7天", model.AchievementTypeSpecial, model.AchievementRarityCommon, "consecutive", 7, "user.dailyLogin", 50, 200, 50},
	{"LOGIN_30_DAYS", "持之以恒", "连续登录30天", model.AchievementTypeSpecial, model.AchievementRarityRare, "consecutive", 30, "user.dailyLogin", 300, 1000, 51},
	{"BECOME_MEMBER", "尊贵会员", "成为会员", model.AchievementTypeSpecial, model.AchievementRarityRare, "once", 1, "membership.purchased", 100, 500, 52},
	{"PROFILE_COMPLETED", "完美档案", "完善个人资料", model.AchievementTypeSpecial, model.AchievementRarityCommon, "once", 1, "user.profileCompleted", 20, 100, 53},
}

// seedAchievements 幂等写入成就定义。
func seedAchievements(db *gorm.DB) error {
	for _, s := range achievementSeeds {
		var n int64
		if err := db.Model(&model.Achievement{}).Where("code = ?", s.Code).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		a := &model.Achievement{
			Code:        s.Code,
			Name:        s.Name,
			Description: s.Description,
			Type:        s.Type,
			Rarity:      s.Rarity,
			Condition: model.AchievementCondition{
				Type:   s.ConditionType,
				Target: s.Target,
				Extra:  map[string]interface{}{"event": s.Event},
			},
			RewardPoints: s.RewardPoints,
			RewardExp:    s.RewardExp,
			Hidden:       false,
			Sort:         s.Sort,
			Enabled:      true,
		}
		if err := db.Create(a).Error; err != nil {
			return err
		}
		log.Printf("[seed] 成就已创建: %s %s", s.Code, s.Name)
	}
	return nil
}
