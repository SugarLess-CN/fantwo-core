// 装饰品视图组装层的单元测试（前端交接单第三版 §二.4 / §二.5）。
//
// 只测不依赖 PG 的纯组装逻辑：*bool 的「false 必须出现」与「无语境不传」、
// 空列表必须序列化成 [] 而不是 null。需要库的批量预取（ownedMap /
// unlockLimitsMap / articleBriefsMap / decoBriefsMap）在 DB 为 nil 时有短路，
// 这里正好用来验证「无 DB 也不 panic、退化成空 map」。
package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestAttachOwnershipFalseNotSwallowed 防的 bug：把 is_owned 写成 bool + omitempty，
// false 会被吃掉 → 前端永远看不到「未拥有」→ 装饰页所有卡片都显示不出状态（本次要修的缺陷）。
func TestAttachOwnershipFalseNotSwallowed(t *testing.T) {
	var v dto.DecorationDto

	// 已登录 + 没有持有记录 → 明确未拥有、未装备
	attachOwnership(&v, model.UserDecoration{}, true)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"is_owned":false`) {
		t.Errorf("未拥有时 is_owned 必须是 false 而不是被省略：%s", got)
	}
	if !strings.Contains(got, `"is_using":false`) {
		t.Errorf("未装备时 is_using 必须是 false 而不是被省略：%s", got)
	}
}

// TestAttachOwnershipOwned 已拥有 + 装备中 → 两个 true。
func TestAttachOwnershipOwned(t *testing.T) {
	var v dto.DecorationDto
	attachOwnership(&v, model.UserDecoration{BaseModel: model.BaseModel{ID: "ud-1"}, IsUsing: true}, true)

	if v.IsOwned == nil || !*v.IsOwned {
		t.Fatal("持有记录存在时 is_owned 必须为 true")
	}
	if v.IsUsing == nil || !*v.IsUsing {
		t.Fatal("持有记录 is_using=true 时 is_using 必须为 true")
	}
	// 有记录但未装备
	var v2 dto.DecorationDto
	attachOwnership(&v2, model.UserDecoration{BaseModel: model.BaseModel{ID: "ud-2"}, IsUsing: false}, true)
	if v2.IsUsing == nil || *v2.IsUsing {
		t.Fatal("有持有记录但未装备时 is_using 必须为 false（不是 nil、不是 true）")
	}
}

// TestAttachOwnershipNoViewer 无登录语境（viewerID 为空）→ 两个字段整个不传。
//
// 这是「管理端 CRUD / 匿名浏览不伪装成未拥有」的契约：前端能区分
// 「查不到归属语境」与「明确未拥有」。
func TestAttachOwnershipNoViewer(t *testing.T) {
	var v dto.DecorationDto
	attachOwnership(&v, model.UserDecoration{}, false)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, "is_owned") || strings.Contains(got, "is_using") {
		t.Errorf("无归属语境时两个字段都必须不传：%s", got)
	}
}

// TestShopViewsEmptyNotNil 空列表必须返回非 nil 切片（序列化成 [] 而不是 null）。
func TestShopViewsEmptyNotNil(t *testing.T) {
	svc := &DecorationService{}
	got := svc.shopViews("", nil)
	if got == nil {
		t.Fatal("shopViews(nil) 必须返回空切片而非 nil")
	}
	raw, _ := json.Marshal(got)
	if string(raw) != "[]" {
		t.Errorf("空列表必须序列化成 []，实际 %s", raw)
	}
}

// TestActivityViewsEmptyNotNil 活动空列表同理。
func TestActivityViewsEmptyNotNil(t *testing.T) {
	svc := &DecorationService{}
	got := svc.activityViews(nil)
	if got == nil {
		t.Fatal("activityViews(nil) 必须返回空切片而非 nil")
	}
	raw, _ := json.Marshal(got)
	if string(raw) != "[]" {
		t.Errorf("空列表必须序列化成 []，实际 %s", raw)
	}
}

// TestActivityViewsEmbeddedRefsKeepBatchShape 内嵌 article / decoration 的组装形态：
// DB 为 nil（预取短路）时两个字段都不传，本体字段照常输出 —— 证明预取失败
// 只降级不报错（列表接口不会因为关联行缺失而整页 500）。
func TestActivityViewsEmbeddedRefsKeepBatchShape(t *testing.T) {
	svc := &DecorationService{}
	act := model.DecorationActivity{
		Name:               "双十一点赞活动",
		Type:               model.DecorationActivityLike,
		ArticleID:          "a-1",
		DecorationID:       "d-1",
		RequiredLikes:      100,
		RequiredComments:   0,
		Status:             model.DecorationActivityStatusActive,
		ParticipantCount:   12,
		CompletedCount:     3,
		RequiredSignInDays: 0,
	}
	got := svc.activityViews([]model.DecorationActivity{act})
	if len(got) != 1 {
		t.Fatalf("期望 1 条，实际 %d", len(got))
	}
	v := got[0]
	if v.Name != "双十一点赞活动" || v.ArticleID != "a-1" || v.DecorationID != "d-1" {
		t.Errorf("本体字段丢失: %+v", v)
	}
	if v.Article != nil || v.Decoration != nil {
		t.Errorf("无 DB 时内嵌引用必须为空（降级不报错），实际 article=%v decoration=%v",
			v.Article, v.Decoration)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), `"article":null`) || strings.Contains(string(raw), `"decoration":null`) {
		t.Errorf("内嵌引用为空时必须是「不传」而不是 null：%s", raw)
	}
}

// TestUnlockLimitsMapNilDBNoPanic 门槛预取在无 DB 时退化成空 map，不 panic。
func TestUnlockLimitsMapNilDBNoPanic(t *testing.T) {
	svc := &DecorationService{}
	if got := svc.unlockLimitsMap([]string{"d-1"}); len(got) != 0 {
		t.Errorf("无 DB 时应返回空 map，实际 %v", got)
	}
	if got := svc.ownedMap("u-1", []string{"d-1"}); len(got) != 0 {
		t.Errorf("无 DB 时应返回空 map，实际 %v", got)
	}
	if got := svc.articleBriefsMap([]string{"a-1"}); len(got) != 0 {
		t.Errorf("无 DB 时应返回空 map，实际 %v", got)
	}
	if got := svc.decoBriefsMap([]string{"d-1"}); len(got) != 0 {
		t.Errorf("无 DB 时应返回空 map，实际 %v", got)
	}
}
