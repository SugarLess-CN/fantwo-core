//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 内容域表（清表用；TRUNCATE CASCADE 不受外键顺序影响）。
var contentTables = []string{
	"articles", "article_tags", "article_likes", "article_favorites",
	"article_dislikes", "browse_histories", "comments", "comment_likes",
	"collections", "collection_items", "tags", "tag_follows", "categories",
}

// newArticleSvc 构造只走 DB 路径的 ArticleService（不注入 authors/audit）。
func newArticleSvc(db *gorm.DB) *service.ArticleService {
	return service.NewArticleService(
		repository.NewArticleRepository(db),
		repository.NewTagRepository(db),
		repository.NewCategoryRepository(db),
		nil, // authors：只验 DB 路径，作者信息留空
		nil, // audit：不注入，避免依赖 Redis/asynq
		zap.NewNop(),
	)
}

// TestArticle_CreateAndGet 真实 PG 下创建 → 回查往返。
// 防：字段未落库、指针字段变 nil、BeforeCreate 未生成 UUID。
func TestArticle_CreateAndGet(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	svc := newArticleSvc(db)
	ctx := context.Background()
	author := service.Actor{UserID: "author-1"}

	content := "正文内容"
	view, e := svc.Create(ctx, author, service.CreateArticleRequest{
		Title:   "集成测试文章",
		Content: &content,
		Status:  model.ArticleStatusDraft,
	})
	if e != nil {
		t.Fatalf("创建文章失败: %+v", e)
	}
	if view.ID == "" {
		t.Fatal("创建后 ID 为空（BaseModel.BeforeCreate 未生效？）")
	}

	var row model.Article
	if err := db.First(&row, "id = ?", view.ID).Error; err != nil {
		t.Fatalf("回查文章失败: %v", err)
	}
	if row.Title != "集成测试文章" {
		t.Errorf("标题不符: got %q", row.Title)
	}
	if row.Content == nil || *row.Content != content {
		t.Errorf("正文指针未正确落库: %v", row.Content)
	}
	if row.Status != model.ArticleStatusDraft {
		t.Errorf("状态不符: got %q", row.Status)
	}
	if row.AuthorID == nil {
		t.Errorf("作者未落库: nil")
	} else if *row.AuthorID != "author-1" {
		t.Errorf("作者不符: got %q", *row.AuthorID)
	}
}

// TestArticleLike_UniqueIndex 点赞幂等的数据层防线。
//
// 防：并发下应用层判重漏判 → 插入两条 like → 计数翻倍。
// 应用层逻辑再对，也必须靠 DB 唯一索引兜底。
func TestArticleLike_UniqueIndex(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "author-like-1"
	art := &model.Article{
		Title:    "点赞幂等测试",
		AuthorID: &author,
		Status:   model.ArticleStatusPublished,
	}
	if err := db.Create(art).Error; err != nil {
		t.Fatalf("建文章失败: %v", err)
	}

	if err := db.Create(&model.ArticleLike{ArticleID: art.ID, UserID: "u-like-1"}).Error; err != nil {
		t.Fatalf("首次点赞应成功: %v", err)
	}
	err := db.Create(&model.ArticleLike{ArticleID: art.ID, UserID: "u-like-1"}).Error
	if err == nil {
		t.Fatal("重复点赞未被唯一索引拦截 —— 计数器会翻倍，数据层防线失效")
	}

	var cnt int64
	db.Model(&model.ArticleLike{}).
		Where("article_id = ? AND user_id = ?", art.ID, "u-like-1").Count(&cnt)
	if cnt != 1 {
		t.Errorf("点赞记录数应为 1，实际 %d", cnt)
	}
}

// TestComment_FloorAndRoot 楼号自增与回复的 root 归属。
//
// 防：并发评论拿同一楼号；回复把「父」当「根」导致楼中楼错乱。
func TestComment_FloorAndRoot(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "author-c1"
	art := &model.Article{Title: "评论测试", AuthorID: &author, Status: model.ArticleStatusPublished}
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
	actor := service.Actor{UserID: "commenter-1"}

	v1, e := svc.Create(ctx, actor, service.CreateCommentRequest{ArticleID: art.ID, Content: "一楼"})
	if e != nil {
		t.Fatalf("创建一级评论失败: %+v", e)
	}
	if v1.Floor != 1 {
		t.Errorf("首个评论楼号应为 1，实际 %d", v1.Floor)
	}

	v2, e := svc.Create(ctx, actor, service.CreateCommentRequest{
		ArticleID: art.ID, Content: "回复一楼", ParentID: &v1.ID,
	})
	if e != nil {
		t.Fatalf("创建回复失败: %+v", e)
	}
	// 楼号只属于一级评论；回复的 floor 保持 0（不是缺陷，是口径）
	if v2.Floor != 0 {
		t.Errorf("回复不应有楼号（应 0），实际 %d", v2.Floor)
	}

	v3, e := svc.Create(ctx, actor, service.CreateCommentRequest{
		ArticleID: art.ID, Content: "回复二楼", ParentID: &v2.ID,
	})
	if e != nil {
		t.Fatalf("创建三级评论失败: %+v", e)
	}
	if v3.RootID == nil {
		t.Fatal("三级评论 rootId 为空 —— 楼中楼归属丢失")
	}
	if *v3.RootID != v1.ID {
		t.Errorf("三级 rootId 应指向一楼 %s，实际 %s（把父当成了根）", v1.ID, *v3.RootID)
	}
	if v3.Floor != 0 {
		t.Errorf("回复不应有楼号（应 0），实际 %d", v3.Floor)
	}
}

// TestCategory_ParentPersist 分类 parent_id 自关联落库与读取。
func TestCategory_ParentPersist(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	root := &model.Category{Name: "根分类"}
	if err := db.Create(root).Error; err != nil {
		t.Fatalf("建根分类失败: %v", err)
	}
	child := &model.Category{Name: "子分类", ParentID: &root.ID}
	if err := db.Create(child).Error; err != nil {
		t.Fatalf("建子分类失败: %v", err)
	}

	cats := repository.NewCategoryRepository(db)
	list, total, err := cats.List(context.Background(), repository.CategoryListQuery{})
	if err != nil {
		t.Fatalf("读分类失败: %v", err)
	}
	if total < 2 || len(list) < 2 {
		t.Fatalf("分类数应 ≥2，实际 total=%d len=%d", total, len(list))
	}
	var found bool
	for _, c := range list {
		if c.ParentID != nil && *c.ParentID == root.ID && c.Name == "子分类" {
			found = true
		}
	}
	if !found {
		t.Error("子分类的 parentId 未正确落库或读取")
	}
}

// TestArticlePagination 分页与总数一致性（真实 SQL 的 LIMIT/OFFSET/COUNT）。
// 防：分页 meta 的 total 与实际条数不一致（count 少了 where 条件等经典 bug）。
func TestArticlePagination(t *testing.T) {
	db := testDB(t)
	cleanTables(t, db, contentTables...)

	author := "author-page"
	for i := 0; i < 7; i++ {
		a := &model.Article{
			Title:    "分页文章" + uniqueSuffix(),
			AuthorID: &author,
			Status:   model.ArticleStatusPublished,
		}
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("建文章失败: %v", err)
		}
	}

	svc := newArticleSvc(db)
	q := service.ArticleListQuery{Page: 1, Limit: 5}
	list, total, e := svc.List(context.Background(), q, service.Actor{UserID: author})
	if e != nil {
		t.Fatalf("分页查询失败: %+v", e)
	}
	if len(list) != 5 {
		t.Errorf("第 1 页应返回 5 条，实际 %d", len(list))
	}
	if total != 7 {
		t.Errorf("total 应为 7，实际 %d", total)
	}

	// 第 2 页应剩 2 条
	list2, total2, e := svc.List(context.Background(), service.ArticleListQuery{Page: 2, Limit: 5}, service.Actor{UserID: author})
	if e != nil {
		t.Fatalf("第 2 页查询失败: %+v", e)
	}
	if len(list2) != 2 {
		t.Errorf("第 2 页应返回 2 条，实际 %d", len(list2))
	}
	if total2 != 7 {
		t.Errorf("第 2 页 total 仍应为 7，实际 %d", total2)
	}
}
