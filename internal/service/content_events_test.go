package service

import (
	"testing"

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
