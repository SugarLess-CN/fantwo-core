// 运营域 JSON 字段命名回归测试：对外 JSON 一律 camelCase（AGENTS.md 全局决策）。
//
// 两类检查：
//  1. 结构体序列化：把我域全部对外结构体零值序列化，若出现含下划线的 JSON 键即失败
//     —— 覆盖 json tag，发现 "user_id" 这类漏改；
//  2. 源码静态扫描：扫我域 handler 里的 gin.H{"..."} 字面量键
//     —— gin.H 的键不受 struct tag 保护，只有扫描能兜住。
//
// 注意：DB 列名（gorm column / clause.Column / Update("col", ...)）保持 snake_case，
// 不在本测试范围内。
package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
)

// snakeJSONKey 匹配形如 "user_id": 的 JSON 键。
var snakeJSONKey = regexp.MustCompile(`"([a-zA-Z0-9]+_[a-zA-Z0-9_]*)":`)

// TestOpsJSONKeysCamelCase 检查我域全部对外结构体的 JSON 键。
func TestOpsJSONKeysCamelCase(t *testing.T) {
	samples := map[string]interface{}{
		// model 实体
		"PointsAccount":        model.PointsAccount{},
		"PointsActivity":       model.PointsActivity{},
		"PointsTaskRecord":     model.PointsTaskRecord{},
		"PointsTransaction":    model.PointsTransaction{},
		"Achievement":          model.Achievement{},
		"UserAchievement":      model.UserAchievement{},
		"AchievementView":      model.AchievementView{},
		"AchievementCondition": model.AchievementCondition{},
		"Emoji":                model.Emoji{},
		"EmojiFavorite":        model.EmojiFavorite{},
		"EmojiPack":            model.EmojiPack{},
		"EmojiPackItem":        model.EmojiPackItem{},
		"Decoration":           model.Decoration{},
		"UserDecoration":       model.UserDecoration{},
		"DecorationActivity":   model.DecorationActivity{},
		"UserActivityProgress": model.UserActivityProgress{},
		"Banner":               model.Banner{},
		"Report":               model.Report{},
		"Invite":               model.Invite{},
		"InviteCommission":     model.InviteCommission{},
		// 入参
		"PointsActivityInput":     model.PointsActivityInput{},
		"AchievementInput":        model.AchievementInput{},
		"EmojiInput":              model.EmojiInput{},
		"EmojiPackInput":          model.EmojiPackInput{},
		"DecorationInput":         model.DecorationInput{},
		"DecorationActivityInput": model.DecorationActivityInput{},
		"BannerInput":             model.BannerInput{},
		"ReportCreateInput":       model.ReportCreateInput{},
		"ReportUpdateInput":       model.ReportUpdateInput{},
		"InviteCreateInput":       model.InviteCreateInput{},
		"InviteUseInput":          model.InviteUseInput{},
		// service 响应结构
		"PointsChangeResult": service.PointsChangeResult{},
		"PointsStatistics":   service.PointsStatistics{},
		"SourceStat":         service.SourceStat{},
		"AchievementStats":   service.AchievementStats{},
		"ClaimResult":        service.ClaimResult{},
		"ClaimAllResult":     service.ClaimAllResult{},
		"InviteStats":        service.InviteStats{},
		"ReportStatistics":   service.ReportStatistics{},
		"GroupCount":         service.GroupCount{},
		// 对外契约结构（已从 service 收归 internal/dto，docs/18）
		"CurrentDecorations": dto.CurrentDecorationsDto{},
		"EmojiPackDetail":    dto.EmojiPackDetailDto{},
		// emoji 域 dto（本轮 upload/emoji/decoration 批次新增）
		"EmojiDto":          dto.EmojiDto{},
		"EmojiPackDto":      dto.EmojiPackDto{},
		"EmojiPackItemDto":  dto.EmojiPackItemDto{},
		"EmojiInputReq":     dto.EmojiInputRequest{},
		"EmojiPackInputReq": dto.EmojiPackInputRequest{},
		// decoration 域 dto
		"DecorationDto":              dto.DecorationDto{},
		"UserDecorationDto":          dto.UserDecorationDto{},
		"DecorationActivityDto":      dto.DecorationActivityDto{},
		"UserActivityProgressDto":    dto.UserActivityProgressDto{},
		"DecorationInputReq":         dto.DecorationInputRequest{},
		"DecorationActivityInputReq": dto.DecorationActivityInputRequest{},
	}

	for name, v := range samples {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s 序列化失败: %v", name, err)
		}
		if m := snakeJSONKey.FindAllStringSubmatch(string(b), -1); len(m) > 0 {
			t.Errorf("%s 存在 snake_case JSON 键 %v（对外 JSON 必须 camelCase）：%s", name, m, string(b))
		}
	}
}

// TestOpsGinHKeysCamelCase 扫描我域 handler 源码里 gin.H 字面量的键。
func TestOpsGinHKeysCamelCase(t *testing.T) {
	files := []string{
		"../handler/ops.go",
		"../handler/points.go",
		"../handler/achievement.go",
		"../handler/emoji.go",
		"../handler/decoration.go",
		"../handler/banner.go",
		"../handler/report.go",
		"../handler/invite.go",
	}
	// gin.H{"key": ...} / gin.H{"key": }
	ginHKey := regexp.MustCompile(`gin\.H\{"([a-zA-Z0-9_]+)"`)

	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatalf("解析路径失败 %s: %v", f, err)
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		for _, m := range ginHKey.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			if regexp.MustCompile(`^[a-zA-Z0-9]+_[a-zA-Z0-9_]+$`).MatchString(key) && key != "auth_user" {
				t.Errorf("%s 中 gin.H 键 %q 为 snake_case，应改 camelCase", f, key)
			}
		}
	}
}
