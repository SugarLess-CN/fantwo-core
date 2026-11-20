package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestToTagView 防的 bug：
//  1. isFollowed 恒为 false（登录用户看不到自己关注的标签，前端按钮状态错）；
//  2. articleCount / followCount 漏映射（标签广场显示 0）；
//  3. cover 丢失（列表无图）。
func TestToTagView(t *testing.T) {
	cover := "cover.jpg"
	desc := "前端相关"
	tag := model.Tag{
		BaseModel:    model.BaseModel{ID: "t-1"},
		Name:         "Vue",
		Description:  &desc,
		Cover:        &cover,
		Sort:         9,
		ArticleCount: 42,
		FollowCount:  8,
	}

	followed := toTagView(tag, true)
	if followed.ID != "t-1" || followed.Name != "Vue" || followed.Sort != 9 {
		t.Errorf("基础字段错误：%+v", followed)
	}
	if !followed.IsFollowed {
		t.Error("followed=true 时 IsFollowed 应为 true")
	}
	if followed.ArticleCount != 42 || followed.FollowCount != 8 {
		t.Errorf("计数错误：article=%d follow=%d", followed.ArticleCount, followed.FollowCount)
	}
	if followed.Cover == nil || *followed.Cover != cover {
		t.Error("封面未映射")
	}

	notFollowed := toTagView(tag, false)
	if notFollowed.IsFollowed {
		t.Error("followed=false 时 IsFollowed 应为 false")
	}
	// 同一实体的两个视图必须互不影响（值类型拷贝）
	if followed.Name != notFollowed.Name {
		t.Error("两次转换结果不一致")
	}
}

// TestToCollectionView 防的 bug：isPublic 漏映射导致私有合集在列表里显示成公开，
// 或 itemCount 为 0 让前端显示「空合集」。
func TestToCollectionView(t *testing.T) {
	name := "我的收藏"
	c := model.Collection{
		BaseModel: model.BaseModel{ID: "col-1"},
		Name:      name,
		IsPublic:  false,
		Sort:      3,
		UserID:    "u-1",
		ItemCount: 15,
		Views:     100,
	}
	v := toCollectionView(c)
	if v.ID != "col-1" || v.Name != name || v.UserID != "u-1" {
		t.Errorf("基础字段错误：%+v", v)
	}
	if v.IsPublic {
		t.Error("私有合集的 isPublic 应为 false")
	}
	if v.ItemCount != 15 || v.Views != 100 || v.Sort != 3 {
		t.Errorf("计数错误：item=%d views=%d sort=%d", v.ItemCount, v.Views, v.Sort)
	}
	// 详情外嵌 items 默认为 nil（列表场景不带 items，避免包体膨胀）
	if v.Items != nil {
		t.Error("列表视图不应带 items")
	}
}

// TestReorderSortValue 防的 bug：合集拖拽排序后顺序反转。
//
// 语义：传入顺序即展示顺序，越靠前 sort 越大（DB 查询按 sort DESC）。
// 3 个元素：[A,B,C] → A=3, B=2, C=1。
func TestReorderSortValue(t *testing.T) {
	cases := []struct {
		name  string
		total int
		idx   int
		want  int
	}{
		{"第 1 个 → 最大", 3, 0, 3},
		{"第 2 个 → 中间", 3, 1, 2},
		{"第 3 个 → 最小", 3, 2, 1},
		{"单元素", 1, 0, 1},
		{"空合集（防御）", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reorderSortValue(c.total, c.idx); got != c.want {
				t.Errorf("reorderSortValue(%d,%d) = %d, want %d", c.total, c.idx, got, c.want)
			}
		})
	}

	// 关键性质断言：顺序必须严格单调递减（防实现被改成 total+idx 之类的反向逻辑）
	prev := 1 << 30
	for idx := 0; idx < 5; idx++ {
		v := reorderSortValue(5, idx)
		if v >= prev {
			t.Fatalf("第 %d 个元素的 sort=%d 不小于前一个 %d（顺序反了）", idx, v, prev)
		}
		prev = v
	}
}
