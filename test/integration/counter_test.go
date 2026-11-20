//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"go.uber.org/zap"
)

// 计数器一致性测试。
//
// 社区类系统最容易出的一类线上问题是「计数漂移」：
// 点赞了但 article.likes 没加、取消收藏两次导致 -2、删评论后 comment_count 不回退。
// 这类 bug 不会报错、不会 5xx，只是数字慢慢对不上，用户投诉「我的赞数不对」。
//
// 这里验证「操作 → 计数」的配对：每次增减都必须有对应的回退路径。

// TestArticleLike_CountConsistency 点赞/取消点赞的计数配对。
func TestArticleLike_CountConsistency(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "count-author-1"
	art := &model.Article{
		Title:    "计数一致性测试",
		AuthorID: &author,
		Status:   model.ArticleStatusPublished,
	}
	if err := db.Create(art).Error; err != nil {
		t.Fatalf("建文章失败: %v", err)
	}

	svc := newArticleSvc(db)
	ctx := context.Background()

	// 用三个不同用户点赞
	actors := []service.Actor{
		{UserID: "u-c1"}, {UserID: "u-c2"}, {UserID: "u-c3"},
	}
	for _, a := range actors {
		if _, e := svc.Like(ctx, art.ID, a, service.ArticleReactionRequest{ReactionType: "like"}); e != nil {
			t.Fatalf("点赞失败(%s): %+v", a.UserID, e)
		}
	}

	var likes int64
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("likes", &likes)
	if likes != 3 {
		t.Errorf("3 人点赞后 likes 应为 3，实际 %d", likes)
	}

	// 同一人重复点赞必须幂等（不重复计数）——这是本用例的核心断言。
	//
	// 注意产品语义：**点赞不可撤销**（原 API 只有 POST /article/{id}/like，
	// 没有 DELETE 版本；MCP 与 swagger 均如此）。可撤销的是收藏与「不喜欢」。
	// 所以这里不测 unlike，而是测「改表情不重复计数」。
	if _, e := svc.Like(ctx, art.ID, actors[0], service.ArticleReactionRequest{ReactionType: "love"}); e != nil {
		t.Fatalf("改表情失败: %+v", e)
	}
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("likes", &likes)
	if likes != 3 {
		t.Errorf("同一人改表情后 likes 仍应为 3，实际 %d（计数被重复累加）", likes)
	}

	// 库里该用户的 like 记录仍应只有 1 条（改表情是 UPDATE 不是 INSERT）
	var n int64
	db.Model(&model.ArticleLike{}).
		Where("article_id = ? AND user_id = ?", art.ID, actors[0].UserID).Count(&n)
	if n != 1 {
		t.Errorf("同一用户应有 1 条点赞记录，实际 %d（改表情插了新行）", n)
	}
}

// TestComment_CountConsistency 评论/删除评论对 article.comment_count 的影响。
func TestComment_CountConsistency(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "count-author-2"
	art := &model.Article{
		Title:    "评论计数测试",
		AuthorID: &author,
		Status:   model.ArticleStatusPublished,
	}
	if err := db.Create(art).Error; err != nil {
		t.Fatalf("建文章失败: %v", err)
	}

	svc := service.NewCommentService(
		repository.NewCommentRepository(db),
		repository.NewArticleRepository(db),
		nil,
		zap.NewNop(),
	)
	ctx := context.Background()
	actor := service.Actor{UserID: "commenter-count"}

	// 两条一级评论
	var ids []string
	for i := 0; i < 2; i++ {
		v, e := svc.Create(ctx, actor, service.CreateCommentRequest{
			ArticleID: art.ID, Content: "评论内容",
		})
		if e != nil {
			t.Fatalf("创建评论失败: %+v", e)
		}
		ids = append(ids, v.ID)
	}

	var cnt int64
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("comment_count", &cnt)
	if cnt != 2 {
		t.Errorf("2 条一级评论后 comment_count 应为 2，实际 %d", cnt)
	}

	// 删除一条（软删），计数应回退
	if _, e := svc.Remove(ctx, ids[0], actor); e != nil {
		t.Fatalf("删除评论失败: %+v", e)
	}
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("comment_count", &cnt)
	if cnt != 1 {
		t.Errorf("删除 1 条后 comment_count 应为 1，实际 %d（未回退）", cnt)
	}
}

// TestArticleFavorite_CountConsistency 收藏/取消收藏计数配对。
func TestArticleFavorite_CountConsistency(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "count-author-3"
	art := &model.Article{
		Title:    "收藏计数测试",
		AuthorID: &author,
		Status:   model.ArticleStatusPublished,
	}
	if err := db.Create(art).Error; err != nil {
		t.Fatalf("建文章失败: %v", err)
	}

	svc := newArticleSvc(db)
	ctx := context.Background()
	a := service.Actor{UserID: "favoriter-1"}

	if _, e := svc.Favorite(ctx, art.ID, a); e != nil {
		t.Fatalf("收藏失败: %+v", e)
	}
	var cnt int64
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("favorite_count", &cnt)
	if cnt != 1 {
		t.Errorf("收藏后 favorite_count 应为 1，实际 %d", cnt)
	}

	// 重复收藏幂等
	_, _ = svc.Favorite(ctx, art.ID, a)
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("favorite_count", &cnt)
	if cnt != 1 {
		t.Errorf("重复收藏后仍应为 1，实际 %d", cnt)
	}

	if _, e := svc.Unfavorite(ctx, art.ID, a); e != nil {
		t.Fatalf("取消收藏失败: %+v", e)
	}
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("favorite_count", &cnt)
	if cnt != 0 {
		t.Errorf("取消收藏后 favorite_count 应为 0，实际 %d", cnt)
	}

	// 重复取消幂等（不能变负）
	_, _ = svc.Unfavorite(ctx, art.ID, a)
	db.Model(&model.Article{}).Where("id = ?", art.ID).Pluck("favorite_count", &cnt)
	if cnt != 0 {
		t.Errorf("重复取消后仍应为 0，实际 %d（计数变负）", cnt)
	}
}
