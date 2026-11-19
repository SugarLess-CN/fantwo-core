package service

import (
	"sync"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestIsDeleteContentAction 举报处置动作 → 是否删除内容。
func TestIsDeleteContentAction(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"DELETE_CONTENT", true},
		{"DELETE_ARTICLE", true},
		{"delete_comment", true},
		{"REMOVE", true},
		{"OFFLINE", true},
		{"BAN_CONTENT", true},
		{"BAN_USER", false},
		{"WARNING", false},
		{"NONE", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isDeleteContentAction(c.action); got != c.want {
			t.Errorf("isDeleteContentAction(%q) = %v, want %v", c.action, got, c.want)
		}
	}
}

// TestNormalizeContentStatus 审核事件状态 → 本域枚举。
func TestNormalizeContentStatus(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"PUBLISHED", model.ArticleStatusPublished},
		{"published", model.ArticleStatusPublished},
		{"APPROVED", model.ArticleStatusPublished},
		{"REVIEW", model.ArticleStatusPending},
		{"PENDING", model.ArticleStatusPending},
		{"REJECTED", model.ArticleStatusRejected},
		{"FAILED", model.ArticleStatusRejected},
		{"BLOCKED", model.ArticleStatusRejected},
		{"ARCHIVED", model.ArticleStatusArchived},
		{"OFFLINE", model.ArticleStatusArchived},
		{"BANNED", model.ArticleStatusBanned},
		{"DELETED", model.ArticleStatusDeleted},
		{"WHATEVER", ""},
	}
	for _, c := range cases {
		if got := normalizeContentStatus(c.in); got != c.want {
			t.Errorf("normalizeContentStatus(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFirstNonEmptyStr payload 多键名兼容。
func TestFirstNonEmptyStr(t *testing.T) {
	if got := firstNonEmptyStr("", "b", "c"); got != "b" {
		t.Errorf("期望 b，实际 %q", got)
	}
	if got := firstNonEmptyStr("", ""); got != "" {
		t.Errorf("期望空串，实际 %q", got)
	}
}

// withTestBus 用独立总线隔离事件捕获（测试结束恢复全局总线）。
func withTestBus(t *testing.T) func(topic string) []interface{} {
	t.Helper()
	oldBus := event.Default()
	bus := event.NewBus()
	event.SetDefault(bus)
	t.Cleanup(func() { event.SetDefault(oldBus) })

	var mu sync.Mutex
	captured := make(map[string][]interface{})
	for _, tp := range []string{
		event.EventArticleCreated,
		event.EventArticleReceivedLike,
		event.EventCommentCreated,
	} {
		topic := tp
		bus.Subscribe(topic, func(payload interface{}) {
			mu.Lock()
			captured[topic] = append(captured[topic], payload)
			mu.Unlock()
		})
	}
	return func(topic string) []interface{} {
		mu.Lock()
		defer mu.Unlock()
		out := make([]interface{}, len(captured[topic]))
		copy(out, captured[topic])
		return out
	}
}

// TestShouldPublishArticleCreated 只有进入可发布链路的文章才计成就。
func TestShouldPublishArticleCreated(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{model.ArticleStatusPublished, true},
		{model.ArticleStatusPending, true},
		{model.ArticleStatusDraft, false},
		{model.ArticleStatusArchived, false},
		{model.ArticleStatusRejected, false},
		{model.ArticleStatusBanned, false},
		{"", false},
	}
	for _, c := range cases {
		if got := shouldPublishArticleCreated(c.status); got != c.want {
			t.Errorf("shouldPublishArticleCreated(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestPublishArticleCreated 发布触发 + payload 字段正确（UserID = 作者）。
func TestPublishArticleCreated(t *testing.T) {
	get := withTestBus(t)
	authorID := "author-1"
	publishArticleCreated(&model.Article{
		BaseModel: model.BaseModel{ID: "art-1"},
		AuthorID:  &authorID,
	})

	got := get(event.EventArticleCreated)
	if len(got) != 1 {
		t.Fatalf("期望发布 1 次 article.created，实际 %d", len(got))
	}
	p, ok := got[0].(event.ArticleCreatedPayload)
	if !ok {
		t.Fatalf("payload 类型错误：%T", got[0])
	}
	if p.UserID != "author-1" || p.ArticleID != "art-1" {
		t.Errorf("payload 字段错误：%+v", p)
	}
}

// TestPublishArticleCreatedSkipNoAuthor 无作者（系统文章）不发布。
func TestPublishArticleCreatedSkipNoAuthor(t *testing.T) {
	get := withTestBus(t)
	publishArticleCreated(&model.Article{BaseModel: model.BaseModel{ID: "art-x"}})
	if n := len(get(event.EventArticleCreated)); n != 0 {
		t.Errorf("无作者文章不应发布，实际发布 %d 次", n)
	}
}

// TestPublishArticleReceivedLike 推进的是文章作者，LikerID 才是点赞人。
func TestPublishArticleReceivedLike(t *testing.T) {
	get := withTestBus(t)
	authorID := "author-2"
	publishArticleReceivedLike(&model.Article{
		BaseModel: model.BaseModel{ID: "art-2"},
		AuthorID:  &authorID,
	}, "liker-9")

	got := get(event.EventArticleReceivedLike)
	if len(got) != 1 {
		t.Fatalf("期望发布 1 次 receivedLike，实际 %d", len(got))
	}
	p, ok := got[0].(event.ArticleReceivedLikePayload)
	if !ok {
		t.Fatalf("payload 类型错误：%T", got[0])
	}
	if p.AuthorID != "author-2" {
		t.Errorf("AuthorID 应为文章作者 author-2，实际 %q（点赞人被错填为作者？）", p.AuthorID)
	}
	if p.LikerID != "liker-9" {
		t.Errorf("LikerID 应为点赞人 liker-9，实际 %q", p.LikerID)
	}
	if p.ArticleID != "art-2" {
		t.Errorf("ArticleID 错误：%q", p.ArticleID)
	}
}

// TestPublishArticleReceivedLikeSkipSelf 自赞不推进成就。
func TestPublishArticleReceivedLikeSkipSelf(t *testing.T) {
	get := withTestBus(t)
	self := "same-user"
	publishArticleReceivedLike(&model.Article{
		BaseModel: model.BaseModel{ID: "art-3"},
		AuthorID:  &self,
	}, self)
	if n := len(get(event.EventArticleReceivedLike)); n != 0 {
		t.Errorf("自赞不应发布，实际发布 %d 次", n)
	}
}

// TestPublishCommentCreated 评论创建发布（UserID = 评论人）。
func TestPublishCommentCreated(t *testing.T) {
	get := withTestBus(t)
	publishCommentCreated(&model.Comment{
		BaseModel: model.BaseModel{ID: "cmt-1"},
		AuthorID:  "commenter-1",
		ArticleID: "art-4",
	})

	got := get(event.EventCommentCreated)
	if len(got) != 1 {
		t.Fatalf("期望发布 1 次 comment.created，实际 %d", len(got))
	}
	p, ok := got[0].(event.CommentCreatedPayload)
	if !ok {
		t.Fatalf("payload 类型错误：%T", got[0])
	}
	if p.UserID != "commenter-1" || p.ArticleID != "art-4" || p.CommentID != "cmt-1" {
		t.Errorf("payload 字段错误：%+v", p)
	}
}

// ---------- 跨域契约对齐（我方 payload ↔ 成就域解析） ----------
//
// 这三条断言锁死「发布发什么」与「消费读什么」必须一致：
// 尤其 receivedLike 必须解析出文章作者，绝不能落到点赞人身上。

// TestContractArticleCreatedUserID 文章创建：推进作者本人。
func TestContractArticleCreatedUserID(t *testing.T) {
	uid, codes, ok := resolveAchievementProgress(event.EventArticleCreated,
		event.ArticleCreatedPayload{UserID: "author-7", ArticleID: "art-7"})
	if !ok || uid != "author-7" {
		t.Fatalf("article.created 应解析出作者 author-7，实际 ok=%v uid=%q", ok, uid)
	}
	if len(codes) == 0 {
		t.Error("article.created 应关联成就代码")
	}
}

// TestContractArticleReceivedLikeResolvesAuthor 点赞：推进作者，不是点赞人。
func TestContractArticleReceivedLikeResolvesAuthor(t *testing.T) {
	uid, codes, ok := resolveAchievementProgress(event.EventArticleReceivedLike,
		event.ArticleReceivedLikePayload{AuthorID: "author-8", ArticleID: "art-8", LikerID: "liker-8"})
	if !ok {
		t.Fatal("receivedLike 应可解析")
	}
	if uid != "author-8" {
		t.Errorf("必须解析出作者 author-8，实际 %q（传反会把成就记到点赞人头上）", uid)
	}
	if uid == "liker-8" {
		t.Error("成就被错误地记到了点赞人 liker-8 头上")
	}
	if len(codes) == 0 {
		t.Error("receivedLike 应关联成就代码")
	}
}

// TestContractCommentCreatedUserID 评论创建：推进评论人。
func TestContractCommentCreatedUserID(t *testing.T) {
	uid, codes, ok := resolveAchievementProgress(event.EventCommentCreated,
		event.CommentCreatedPayload{UserID: "commenter-9", ArticleID: "art-9", CommentID: "cmt-9"})
	if !ok || uid != "commenter-9" {
		t.Fatalf("comment.created 应解析出评论人 commenter-9，实际 ok=%v uid=%q", ok, uid)
	}
	if len(codes) == 0 {
		t.Error("comment.created 应关联成就代码")
	}
}
