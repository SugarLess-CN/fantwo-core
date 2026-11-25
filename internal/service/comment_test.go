package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestResolveCommentRootID 防的 bug：多级回复挂错楼层根。
//
// 场景：A 是一级评论（rootID 空）→ B 回复 A → C 回复 B。
// 若 C 把 B 当根，评论树会分裂出「B-C」这个假楼层，前端只渲染 A 的 replies 时就看不到 C。
func TestResolveCommentRootID(t *testing.T) {
	rootID := "root-1"

	cases := []struct {
		name   string
		parent *model.Comment
		want   string
		wantOK bool
	}{
		{
			name:   "nil 父（防御）",
			parent: nil,
			wantOK: false,
		},
		{
			name:   "回复一级评论 → 根是父本身",
			parent: &model.Comment{BaseModel: model.BaseModel{ID: "root-1"}},
			want:   "root-1",
			wantOK: true,
		},
		{
			name:   "回复二级评论 → 继承父的根",
			parent: &model.Comment{BaseModel: model.BaseModel{ID: "child-2"}, RootID: &rootID},
			want:   "root-1",
			wantOK: true,
		},
		{
			name: "父的 rootID 为空串视为无根 → 取父 ID",
			parent: func() *model.Comment {
				empty := ""
				return &model.Comment{BaseModel: model.BaseModel{ID: "x-1"}, RootID: &empty}
			}(),
			want:   "x-1",
			wantOK: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveCommentRootID(c.parent)
			if !c.wantOK {
				if got != nil {
					t.Errorf("期望 nil，实际 %q", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("期望 %q，实际 nil", c.want)
			}
			if *got != c.want {
				t.Errorf("rootID = %q, want %q", *got, c.want)
			}
			// 必须返回新指针：直接返回 parent.RootID 会让调用方与父评论共享地址，
			// 后续修改互相污染。
			if c.parent != nil && c.parent.RootID != nil && got == c.parent.RootID {
				t.Error("返回值复用了父评论的 RootID 指针（应复制一份）")
			}
		})
	}
}

// TestNextCommentFloor 防的 bug：楼层从 0 开始、负数楼层、空表时楼层为 0。
func TestNextCommentFloor(t *testing.T) {
	cases := []struct {
		name     string
		maxFloor int
		want     int
	}{
		{"空表首个评论 → 1", 0, 1},
		{"已有 5 楼 → 6", 5, 6},
		{"异常负数 → 1", -3, 1},
		{"最大整数（防御）", 41, 42},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextCommentFloor(c.maxFloor); got != c.want {
				t.Errorf("nextCommentFloor(%d) = %d, want %d", c.maxFloor, got, c.want)
			}
		})
	}
}

// TestBuildCommentView 防的 bug：
//  1. images 裸串直出（前端渲染崩）；2. isLiked 对所有人都是 true/false（登录用户看不到自己的点赞态）；
//  3. 作者信息在 authorMap 缺失时污染成空结构体（应保持 nil，前端才好降级显示）。
func TestBuildCommentView(t *testing.T) {
	images := `["a.jpg"]`
	c := model.Comment{
		BaseModel:  model.BaseModel{ID: "cmt-1"},
		Content:    "hello",
		Images:     &images,
		Likes:      3,
		ReplyCount: 2,
		Floor:      7,
		IsPinned:   true,
		Status:     model.CommentStatusPublished,
		AuthorID:   "u-1",
		ArticleID:  "art-1",
	}
	authorMap := map[string]AuthorBrief{
		"u-1": {ID: "u-1", Nickname: "小明", Username: "xm"},
	}
	likedSet := map[string]bool{"cmt-1": true}

	v := buildCommentView(c, authorMap, likedSet)

	if v.ID != "cmt-1" || v.Content != "hello" || v.Floor != 7 || v.Likes != 3 || v.ReplyCount != 2 {
		t.Errorf("基础字段映射错误：%+v", v)
	}
	if v.IsPinned != true || v.Status != model.CommentStatusPublished {
		t.Errorf("状态字段错误：pinned=%v status=%q", v.IsPinned, v.Status)
	}
	if v.IsLiked != true {
		t.Error("likedSet 命中时 isLiked 应为 true")
	}
	imgs := v.Images
	if len(imgs) != 1 || imgs[0] != "a.jpg" {
		t.Errorf("images 应为 []string{a.jpg}，实际 %#v", v.Images)
	}
	if v.Replies == nil {
		t.Error("replies 必须是已初始化切片（空即 []，不能是 null）")
	}
	if v.ParentID != "" || v.RootID != "" {
		t.Errorf("一级评论 parentId/rootId 应为空串，实际 %q/%q", v.ParentID, v.RootID)
	}
	if v.Author == nil || v.Author.Nickname != "小明" {
		t.Errorf("作者信息未填充：%+v", v.Author)
	}

	// authorMap 缺失时不得塞空结构体
	v2 := buildCommentView(c, map[string]AuthorBrief{}, map[string]bool{})
	if v2.Author != nil {
		t.Errorf("作者不在 map 时应为 nil，实际 %+v", v2.Author)
	}
	if v2.IsLiked {
		t.Error("likedSet 未命中时 isLiked 应为 false")
	}
}
