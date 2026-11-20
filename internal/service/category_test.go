package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestBuildCategoryTree 防的 bug：
//  1. 子分类被当成顶级分类重复出现（前端出现两份「前端开发」）；
//  2. 孤儿分类（parentId 指向已删除的父）静默消失且无日志；
//  3. parentId 指向空串时被当作有父，导致该分类整个丢失。
func TestBuildCategoryTree(t *testing.T) {
	root1 := "root-1"
	empty := ""
	ghostParent := "not-exist"

	all := []model.Category{
		{BaseModel: model.BaseModel{ID: "root-1"}, Name: "技术", Sort: 10},
		{BaseModel: model.BaseModel{ID: "root-2"}, Name: "生活", Sort: 5},
		{BaseModel: model.BaseModel{ID: "child-1"}, Name: "前端", ParentID: &root1, Sort: 3},
		{BaseModel: model.BaseModel{ID: "child-2"}, Name: "后端", ParentID: &root1, Sort: 2},
		// 空串 parentId 应视为顶级
		{BaseModel: model.BaseModel{ID: "root-3"}, Name: "摄影", ParentID: &empty},
		// 孤儿：父不存在
		{BaseModel: model.BaseModel{ID: "orphan"}, Name: "孤儿", ParentID: &ghostParent},
	}

	tree := buildCategoryTree(all)

	// 顶级应有 3 个：root-1 / root-2 / root-3（空串视为顶级）
	if len(tree) != 3 {
		var ids []string
		for _, c := range tree {
			ids = append(ids, c.ID)
		}
		t.Fatalf("顶级分类应为 3 个，实际 %d 个：%v", len(tree), ids)
	}

	byID := map[string]CategoryView{}
	for _, c := range tree {
		byID[c.ID] = c
	}
	if _, ok := byID["child-1"]; ok {
		t.Error("子分类 child-1 不应出现在顶级列表")
	}
	if _, ok := byID["orphan"]; ok {
		t.Error("孤儿分类不应出现在顶级列表")
	}
	if len(byID["root-1"].Children) != 2 {
		t.Errorf("技术下应有 2 个子分类，实际 %d", len(byID["root-1"].Children))
	}
	for _, ch := range byID["root-1"].Children {
		if ch.ParentID == nil || *ch.ParentID != "root-1" {
			t.Errorf("子分类 parentId 错误：%v", ch.ParentID)
		}
	}
}

// TestBuildCategoryTreeEmpty 空输入不得返回 nil（前端 .map 会崩）。
func TestBuildCategoryTreeEmpty(t *testing.T) {
	tree := buildCategoryTree(nil)
	if tree == nil {
		t.Fatal("空输入应返回空切片而非 nil")
	}
	if len(tree) != 0 {
		t.Errorf("空输入应返回 0 个，实际 %d", len(tree))
	}
}

// TestToCategoryView 防的 bug：状态/计数漏映射，前端分类管理页显示 0 篇文章、状态空白。
func TestToCategoryView(t *testing.T) {
	desc := "技术分类"
	link := "https://example.com/tech"
	parent := "p-1"
	c := model.Category{
		BaseModel:    model.BaseModel{ID: "c-1"},
		Name:         "技术",
		Description:  &desc,
		ParentID:     &parent,
		Link:         &link,
		Sort:         7,
		Status:       model.CategoryStatusEnabled,
		ArticleCount: 12,
		FollowCount:  3,
	}
	v := toCategoryView(c)
	if v.ID != "c-1" || v.Name != "技术" || v.Sort != 7 {
		t.Errorf("基础字段错误：%+v", v)
	}
	if v.Status != model.CategoryStatusEnabled {
		t.Errorf("状态应为 ENABLED，实际 %q", v.Status)
	}
	if v.ArticleCount != 12 || v.FollowCount != 3 {
		t.Errorf("计数错误：article=%d follow=%d", v.ArticleCount, v.FollowCount)
	}
	if v.ParentID == nil || *v.ParentID != "p-1" {
		t.Errorf("父分类错误：%v", v.ParentID)
	}
	if v.Description == nil || *v.Description != desc || v.Link == nil || *v.Link != link {
		t.Error("描述/链接未映射")
	}
}
