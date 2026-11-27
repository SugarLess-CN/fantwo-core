// 举报服务：提交、列表、详情、处理（含动作）、统计。
//
// 对齐原 report.service.ts。跨域边界（重要）：
//   - 举报目标的「是否存在」用轻量局部视图查（只查主键列），不引用其它域 model；
//   - DELETE_CONTENT / BAN_USER 这类动作**不在本域直接执行**：处理时发
//     event.ReportHandled，由 content 域删文章/评论、用户域封禁账号。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// ReportService 举报业务。
type ReportService struct {
	DB *gorm.DB

	reports repository.ReportRepo
	bus     *event.Bus
}

// NewReportService 构造；bus 可传 event.Default()。
func NewReportService(db *gorm.DB, bus *event.Bus) *ReportService {
	return &ReportService{DB: db, bus: bus}
}

// Create 提交举报（校验目标存在 + 同人同目标去重）。
func (s *ReportService) Create(reporterID string, in model.ReportCreateInput) (*model.Report, error) {
	switch in.Type {
	case model.ReportTypeUser:
		if in.ReportedUserID == "" {
			return nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonMissingField, "reportedUserId")
		}
		ok, err := existsByID(s.DB, &model.ReportTargetUserView{}, in.ReportedUserID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errcode.ErrNotFound.WithKey(i18n.UserNotFound)
		}
	case model.ReportTypeArticle:
		if in.ReportedArticleID == "" {
			return nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonMissingField, "reportedArticleId")
		}
		ok, err := existsByID(s.DB, &model.ReportTargetArticleView{}, in.ReportedArticleID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)
		}
	case model.ReportTypeComment:
		if in.ReportedCommentID == "" {
			return nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonMissingField, "reportedCommentId")
		}
		ok, err := existsByID(s.DB, &model.ReportTargetCommentView{}, in.ReportedCommentID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errcode.ErrNotFound.WithKey(i18n.CommentNotFound)
		}
	default:
		return nil, errcode.ErrBadRequest.WithKey(i18n.ReportTargetInvalid)
	}

	dup, err := s.reports.FindDuplicatePending(s.DB, reporterID, in)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ReportAlreadySubmitted)
	}

	r := &model.Report{
		Type:              in.Type,
		Reason:            in.Reason,
		Category:          in.Category,
		Description:       in.Description,
		Status:            model.ReportStatusPending,
		ReporterID:        reporterID,
		ReportedUserID:    in.ReportedUserID,
		ReportedArticleID: in.ReportedArticleID,
		ReportedCommentID: in.ReportedCommentID,
	}
	if err := s.reports.Create(s.DB, r); err != nil {
		return nil, err
	}
	return r, nil
}

// List 举报列表（管理端）。
func (s *ReportService) List(q model.ReportQuery) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.reports.List(s.DB, q)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, q.Page, q.Limit)}, nil
}

// Get 举报详情。
func (s *ReportService) Get(id string) (*model.Report, error) {
	r, err := s.reports.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.ReportNotFound)
	}
	return r, nil
}

// Update 处理举报：写处理结果 + 记录处理人/时间；动作通过事件下发。
func (s *ReportService) Update(id, handlerID string, in model.ReportUpdateInput) (*model.Report, error) {
	r, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if in.Result != "" {
		r.Result = in.Result
	}
	if in.Action != "" {
		r.Action = in.Action
	}
	if in.Status != "" {
		r.Status = in.Status
	}
	if r.Status != model.ReportStatusPending {
		now := time.Now()
		r.HandlerID = handlerID
		r.HandledAt = &now
	}
	if err := s.reports.Save(s.DB, r); err != nil {
		return nil, err
	}

	// 动作下发（content / 用户域监听执行；本域不直接写他们的表）
	if shouldDispatchReportAction(r.Action) {
		if s.bus != nil {
			s.bus.Publish(event.EventReportHandled, event.ReportHandledPayload{
				ReportID:          r.ID,
				Type:              r.Type,
				Action:            r.Action,
				ReportedUserID:    r.ReportedUserID,
				ReportedArticleID: r.ReportedArticleID,
				ReportedCommentID: r.ReportedCommentID,
				HandlerID:         handlerID,
			})
		}
	}
	return r, nil
}

// Delete 删除举报记录。
func (s *ReportService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.reports.Delete(s.DB, id)
}

// shouldDispatchReportAction 判断处理动作是否需要下发事件。
//
// 防的 bug：DELETE_CONTENT / BAN_USER 涉及别域的表（articles/comments/users），
// 运营域只发 report.handled 让 content/auth-user 执行；而 NONE（无需处理）与空动作
// 绝不能被当成「要删内容」发出去 —— 否则一次误点就会删掉别人的文章。
func shouldDispatchReportAction(action string) bool {
	switch action {
	case model.ReportActionDeleteContent, model.ReportActionBanUser, model.ReportActionWarning:
		return true
	}
	return false
}

// ReportStatistics 举报统计（对齐原 getStatistics）。
type ReportStatistics struct {
	Total      int64            `json:"total"`
	ByStatus   map[string]int64 `json:"by_status"`
	ByType     []GroupCount     `json:"by_type"`
	ByCategory []GroupCount     `json:"by_category"`
}

// GroupCount 分组计数。
type GroupCount struct {
	Key   string `json:"key" gorm:"column:key"`
	Count int64  `json:"count" gorm:"column:count"`
}

// Statistics 统计。
func (s *ReportService) Statistics() (*ReportStatistics, error) {
	total, err := s.reports.CountTotal(s.DB)
	if err != nil {
		return nil, err
	}
	st := &ReportStatistics{Total: total, ByStatus: map[string]int64{}}
	for _, status := range []string{
		model.ReportStatusPending,
		model.ReportStatusProcessing,
		model.ReportStatusResolved,
		model.ReportStatusRejected,
	} {
		n, err := s.reports.CountByStatus(s.DB, status)
		if err != nil {
			return nil, err
		}
		st.ByStatus[status] = n
	}

	byType, err := s.groupCount("type")
	if err != nil {
		return nil, err
	}
	byCategory, err := s.groupCount("category")
	if err != nil {
		return nil, err
	}
	st.ByType = byType
	st.ByCategory = byCategory
	return st, nil
}

// groupCount 分组计数（列名由本文件内常量传入，不接受外部拼接）。
func (s *ReportService) groupCount(column string) ([]GroupCount, error) {
	if column != "type" && column != "category" {
		return nil, errcode.ErrInternal.WithKey(i18n.ReportInvalidColumn)
	}
	var out []GroupCount
	err := s.DB.Model(&model.Report{}).
		Select(column + " AS key, COUNT(*) AS count").
		Group(column).Order("count DESC").Scan(&out).Error
	return out, err
}

// existsByID 用局部视图判断目标行是否存在。
func existsByID(db *gorm.DB, view interface{}, id string) (bool, error) {
	var n int64
	if err := db.Model(view).Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
