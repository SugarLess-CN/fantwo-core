// 内容域跨域事件订阅。
//
// 归属（AGENTS.md 跨域事件订阅决策，2026-11-19）：
//   - report.handled          → **内容域**监听后删除文章/评论（用户域负责封禁用户，互不越界）；
//   - article.status.changed  → **内容域**监听后落库 articles.status 与相关计数
//     （审核域只发事件、不写 articles 表）；
//   - comment.status.changed  → **内容域**监听后落库 comments.status 与文章评论数。
//
// payload 兼容两种形态：event 包定义的载荷结构体（值/指针），或 map[string]interface{}
// （其它域直接 publish map 也能工作），与 user_events.go 的处理口径保持一致。
package service

import (
	"context"
	"strings"
	"sync"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ContentEventDeps 内容域事件订阅依赖（装配层传入；DB 为 nil 时全部订阅跳过）。
type ContentEventDeps struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// contentSubOnce 保证订阅只注册一次（多入口调用安全）。
var contentSubOnce sync.Once

// RegisterContentEventSubscribers 注册内容域事件订阅者。
//
// 装配层在构造内容域 handler 后调用一次即可；重复调用被 sync.Once 吸收。
func RegisterContentEventSubscribers(d ContentEventDeps) {
	if d.DB == nil {
		return
	}
	if d.Logger == nil {
		d.Logger = zap.NewNop()
	}
	contentSubOnce.Do(func() {
		bus := event.Default()
		bus.Subscribe(event.EventReportHandled, func(payload interface{}) {
			handleReportHandledContent(d, payload)
		})
		bus.Subscribe(event.EventArticleStatusChanged, func(payload interface{}) {
			handleArticleStatusChanged(d, payload)
		})
		bus.Subscribe(event.EventCommentStatusChanged, func(payload interface{}) {
			handleCommentStatusChanged(d, payload)
		})
	})
}

// ---------- report.handled：按举报处置结论删除内容 ----------

// handleReportHandledContent 举报处置 → 删除被举报文章/评论。
//
// 只处理「删除类」处置（DELETE_CONTENT / DELETE_ARTICLE / REMOVE / OFFLINE 等）；
// BAN_USER / WARNING / NONE 不属于内容删除，交给用户域或忽略。
func handleReportHandledContent(d ContentEventDeps, payload interface{}) {
	var (
		reportType string
		action     string
		articleID  string
		commentID  string
		reportID   string
	)
	switch p := payload.(type) {
	case event.ReportHandledPayload:
		reportType, action, reportID = p.Type, p.Action, p.ReportID
		articleID, commentID = p.ReportedArticleID, p.ReportedCommentID
	case *event.ReportHandledPayload:
		if p == nil {
			return
		}
		reportType, action, reportID = p.Type, p.Action, p.ReportID
		articleID, commentID = p.ReportedArticleID, p.ReportedCommentID
	case map[string]interface{}:
		reportType = strOf(p["type"])
		action = strOf(p["action"])
		reportID = firstNonEmptyStr(strOf(p["report_id"]), strOf(p["reportId"]))
		articleID = firstNonEmptyStr(strOf(p["reported_article_id"]), strOf(p["reportedArticleId"]))
		commentID = firstNonEmptyStr(strOf(p["reported_comment_id"]), strOf(p["reportedCommentId"]))
	default:
		return
	}

	if !isDeleteContentAction(action) {
		return
	}

	ctx, cancel := bgCtx()
	defer cancel()

	switch strings.ToUpper(strings.TrimSpace(reportType)) {
	case "ARTICLE":
		if articleID == "" {
			return
		}
		if err := deleteArticleByReport(ctx, d, articleID); err != nil {
			d.Logger.Warn("report.handled 删除文章失败 reportID="+reportID+" articleID="+articleID,
				zap.Error(err))
			return
		}
		d.Logger.Info("report.handled 已删除文章 articleID=" + articleID + " reportID=" + reportID)
	case "COMMENT":
		if commentID == "" {
			return
		}
		if err := deleteCommentByReport(ctx, d, commentID); err != nil {
			d.Logger.Warn("report.handled 删除评论失败 reportID="+reportID+" commentID="+commentID,
				zap.Error(err))
			return
		}
		d.Logger.Info("report.handled 已删除评论 commentID=" + commentID + " reportID=" + reportID)
	}
}

// isDeleteContentAction 是否为「删除内容」类处置。
func isDeleteContentAction(action string) bool {
	a := strings.ToUpper(strings.TrimSpace(action))
	switch a {
	case "", "NONE", "WARNING", "BAN_USER":
		return false
	}
	return strings.Contains(a, "DELETE") || strings.Contains(a, "REMOVE") ||
		strings.Contains(a, "OFFLINE") || strings.Contains(a, "BAN_CONTENT")
}

// deleteArticleByReport 删除文章（软删）并级联回退计数与评论。
// 与 ArticleService.Remove 的级联口径一致，只是没有操作者上下文。
func deleteArticleByReport(ctx context.Context, d ContentEventDeps, articleID string) error {
	repo := repository.NewArticleRepository(d.DB)
	a, err := repo.FindByID(ctx, articleID)
	if err != nil {
		if IsNotFound(err) {
			return nil // 已被删除，幂等返回
		}
		return err
	}
	tagIDs, err := repo.ArticleTagIDs(ctx, articleID)
	if err != nil {
		return err
	}

	return d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repoTx := repository.NewArticleRepository(tx)
		tagRepo := repository.NewTagRepository(tx)
		catRepo := repository.NewCategoryRepository(tx)
		commentRepo := repository.NewCommentRepository(tx)

		if err := repoTx.SoftDelete(ctx, articleID); err != nil {
			return err
		}
		if err := repoTx.DeleteArticleTags(ctx, articleID); err != nil {
			return err
		}
		if err := tagRepo.IncrCounters(ctx, tagIDs, "article_count", -1); err != nil {
			return err
		}
		if a.CategoryID != nil {
			if err := catRepo.IncrCounter(ctx, *a.CategoryID, "article_count", -1); err != nil {
				return err
			}
		}
		if a.AuthorID != nil {
			if err := repository.NewUserRepo().IncrCounter(tx, *a.AuthorID, "article_count", -1); err != nil {
				return err
			}
		}
		return commentRepo.SoftDeleteByArticle(ctx, articleID)
	})
}

// deleteCommentByReport 删除评论（软删）并回退评论数/回复数。
func deleteCommentByReport(ctx context.Context, d ContentEventDeps, commentID string) error {
	repo := repository.NewCommentRepository(d.DB)
	c, err := repo.FindByID(ctx, commentID)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repoTx := repository.NewCommentRepository(tx)
		articleRepo := repository.NewArticleRepository(tx)
		if err := repoTx.SoftDelete(ctx, commentID); err != nil {
			return err
		}
		if c.RootID == nil {
			// 一级评论：回退文章评论数
			return articleRepo.IncrCounter(ctx, c.ArticleID, "comment_count", -1)
		}
		if c.ParentID != nil {
			return repoTx.IncrCounter(ctx, *c.ParentID, "reply_count", -1)
		}
		return nil
	})
}

// ---------- article.status.changed：审核结果回写文章状态 ----------

// handleArticleStatusChanged 审核结果 → 更新 articles.status 与计数。
//
// 计数口径：只有 PUBLISHED 计入 article_count。状态在「发布 <-> 非发布」之间迁移时，
// 同步调整作者 users.article_count、分类 categories.article_count、标签 tags.article_count。
func handleArticleStatusChanged(d ContentEventDeps, payload interface{}) {
	var articleID, from, to, reason, source string
	switch p := payload.(type) {
	case event.ArticleStatusChangedPayload:
		articleID, from, to, reason, source = p.ArticleID, p.From, p.To, p.Reason, p.Source
	case *event.ArticleStatusChangedPayload:
		if p == nil {
			return
		}
		articleID, from, to, reason, source = p.ArticleID, p.From, p.To, p.Reason, p.Source
	case map[string]interface{}:
		articleID = firstNonEmptyStr(strOf(p["article_id"]), strOf(p["articleId"]))
		from = strOf(p["from"])
		to = strOf(p["to"])
		reason = strOf(p["reason"])
		source = strOf(p["source"])
	default:
		return
	}
	if articleID == "" {
		return
	}
	newStatus := normalizeContentStatus(to)
	if newStatus == "" {
		d.Logger.Warn("article.status.changed 未知目标状态，已忽略 to=" + to)
		return
	}

	ctx, cancel := bgCtx()
	defer cancel()

	repo := repository.NewArticleRepository(d.DB)
	a, err := repo.FindByID(ctx, articleID)
	if err != nil {
		if IsNotFound(err) {
			d.Logger.Warn("article.status.changed 文章不存在 articleID=" + articleID)
			return
		}
		d.Logger.Warn("article.status.changed 查询文章失败 articleID="+articleID, zap.Error(err))
		return
	}

	wasPublished := a.Status == model.ArticleStatusPublished
	nowPublished := newStatus == model.ArticleStatusPublished

	err = d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repoTx := repository.NewArticleRepository(tx)
		tagRepo := repository.NewTagRepository(tx)
		catRepo := repository.NewCategoryRepository(tx)

		fields := map[string]interface{}{"status": newStatus}
		if err := repoTx.UpdateFields(ctx, articleID, fields); err != nil {
			return err
		}
		if wasPublished == nowPublished {
			return nil
		}
		delta := 1
		if !nowPublished {
			delta = -1
		}
		if a.AuthorID != nil {
			if err := repository.NewUserRepo().IncrCounter(tx, *a.AuthorID, "article_count", delta); err != nil {
				return err
			}
		}
		if a.CategoryID != nil {
			if err := catRepo.IncrCounter(ctx, *a.CategoryID, "article_count", delta); err != nil {
				return err
			}
		}
		tagIDs, err := repoTx.ArticleTagIDs(ctx, articleID)
		if err != nil {
			return err
		}
		return tagRepo.IncrCounters(ctx, tagIDs, "article_count", delta)
	})
	if err != nil {
		d.Logger.Warn("article.status.changed 回写失败 articleID="+articleID, zap.Error(err))
		return
	}
	d.Logger.Info("article.status.changed 已回写 articleID=" + articleID +
		" " + from + "->" + newStatus + " source=" + source + " reason=" + reason)
}

// ---------- comment.status.changed：审核结果回写评论状态 ----------

// handleCommentStatusChanged 审核结果 → 更新 comments.status 与文章评论数。
func handleCommentStatusChanged(d ContentEventDeps, payload interface{}) {
	var commentID, from, to, reason, source string
	switch p := payload.(type) {
	case event.CommentStatusChangedPayload:
		commentID, from, to, reason, source = p.CommentID, p.From, p.To, p.Reason, p.Source
	case *event.CommentStatusChangedPayload:
		if p == nil {
			return
		}
		commentID, from, to, reason, source = p.CommentID, p.From, p.To, p.Reason, p.Source
	case map[string]interface{}:
		commentID = firstNonEmptyStr(strOf(p["comment_id"]), strOf(p["commentId"]))
		from = strOf(p["from"])
		to = strOf(p["to"])
		reason = strOf(p["reason"])
		source = strOf(p["source"])
	default:
		return
	}
	if commentID == "" {
		return
	}
	newStatus := normalizeContentStatus(to)
	if newStatus == "" {
		d.Logger.Warn("comment.status.changed 未知目标状态，已忽略 to=" + to)
		return
	}

	ctx, cancel := bgCtx()
	defer cancel()

	repo := repository.NewCommentRepository(d.DB)
	c, err := repo.FindByID(ctx, commentID)
	if err != nil {
		if IsNotFound(err) {
			d.Logger.Warn("comment.status.changed 评论不存在 commentID=" + commentID)
			return
		}
		d.Logger.Warn("comment.status.changed 查询评论失败 commentID="+commentID, zap.Error(err))
		return
	}

	wasPublished := c.Status == model.CommentStatusPublished
	nowPublished := newStatus == model.CommentStatusPublished

	err = d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repoTx := repository.NewCommentRepository(tx)
		articleRepo := repository.NewArticleRepository(tx)
		if err := repoTx.UpdateFields(ctx, commentID, map[string]interface{}{"status": newStatus}); err != nil {
			return err
		}
		if wasPublished == nowPublished {
			return nil
		}
		delta := 1
		if !nowPublished {
			delta = -1
		}
		if c.RootID == nil {
			return articleRepo.IncrCounter(ctx, c.ArticleID, "comment_count", delta)
		}
		if c.ParentID != nil {
			return repoTx.IncrCounter(ctx, *c.ParentID, "reply_count", delta)
		}
		return nil
	})
	if err != nil {
		d.Logger.Warn("comment.status.changed 回写失败 commentID="+commentID, zap.Error(err))
		return
	}
	d.Logger.Info("comment.status.changed 已回写 commentID=" + commentID +
		" " + from + "->" + newStatus + " source=" + source + " reason=" + reason)
}

// normalizeContentStatus 事件状态值 → 本域枚举（无法识别返回空串，调用方跳过）。
//
// 审核域用 REVIEW 表示「审核中」，本域对应 PENDING；其余按业务状态名直通。
func normalizeContentStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PUBLISHED", "PASS", "APPROVED":
		return model.ArticleStatusPublished
	case "DRAFT":
		return model.ArticleStatusDraft
	case "PENDING", "REVIEW", "AUDITING":
		return model.ArticleStatusPending
	case "REJECTED", "REJECT", "FAILED", "BLOCKED":
		return model.ArticleStatusRejected
	case "ARCHIVED", "OFFLINE":
		return model.ArticleStatusArchived
	case "BANNED":
		return model.ArticleStatusBanned
	case "DELETED":
		return model.ArticleStatusDeleted
	default:
		return ""
	}
}

// firstNonEmptyStr 返回第一个非空字符串（payload 兼容多种键名）。
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---------- 内容域事件发布端 ----------
//
// 契约见 internal/event/ops_events.go（成就域订阅这些事件推进用户成就）。
// 发布纪律：
//   - 一律在**事务提交之后**发布，保证订阅方读得到已提交数据；
//   - 同步内存调用，单个订阅者 panic 由 event.Bus.Publish 内部 recover 吸收，
//     不会回滚或中断创建流程（与 order.paid 的发布写法一致）。

// shouldPublishArticleCreated 文章创建后是否发布 article.created。
//
// 只有进入「可发布链路」的创作才计入成就：PUBLISHED（直接发布）或
// PENDING（已提交审核，等待结果）。DRAFT 草稿不发，避免反复存草稿刷成就。
func shouldPublishArticleCreated(status string) bool {
	return status == model.ArticleStatusPublished || status == model.ArticleStatusPending
}

// publishArticleCreated 发布 article.created（UserID = 作者本人）。
func publishArticleCreated(a *model.Article) {
	if a == nil || a.AuthorID == nil || *a.AuthorID == "" {
		return
	}
	event.Default().Publish(event.EventArticleCreated, event.ArticleCreatedPayload{
		UserID:    *a.AuthorID,
		ArticleID: a.ID,
	})
}

// publishArticleReceivedLike 发布 article.receivedLike。
//
// 关键语义：推进的是**被点赞文章的作者**（AuthorID），LikerID 才是点赞人；
// 自赞不发（否则作者能靠给自己点赞刷 FIRST_LIKE / LIKE_100）。
func publishArticleReceivedLike(a *model.Article, likerID string) {
	if a == nil || a.AuthorID == nil || *a.AuthorID == "" || likerID == "" {
		return
	}
	if *a.AuthorID == likerID {
		return
	}
	event.Default().Publish(event.EventArticleReceivedLike, event.ArticleReceivedLikePayload{
		AuthorID:  *a.AuthorID,
		ArticleID: a.ID,
		LikerID:   likerID,
	})
}

// publishCommentCreated 发布 comment.created（UserID = 评论人）。
func publishCommentCreated(c *model.Comment) {
	if c == nil || c.AuthorID == "" {
		return
	}
	event.Default().Publish(event.EventCommentCreated, event.CommentCreatedPayload{
		UserID:    c.AuthorID,
		ArticleID: c.ArticleID,
		CommentID: c.ID,
	})
}
