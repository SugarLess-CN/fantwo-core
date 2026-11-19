// 邀请服务：邀请码创建/使用、我的邀请、统计、收益（分成）记录。
//
// 对齐原 invite.service.ts。跨域边界：使用邀请码时原实现会回写 users 表的
// inviter_id / invite_code / invite_count —— 这些归用户域，本域只把邀请记录
// 置为 USED 并发布 event.InviteUsed，由用户域监听落库。
package service

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// InviteService 邀请业务。
type InviteService struct {
	DB *gorm.DB

	invites     repository.InviteRepo
	commissions repository.InviteCommissionRepo

	bus *event.Bus
	// frontURL 邀请链接基址（对齐原 FRONTEND_URL，默认 http://localhost:3000）。
	frontURL string
	// defaultRate 默认分成比例（对齐原默认 0.05）。
	defaultRate float64
}

// NewInviteService 构造。
func NewInviteService(db *gorm.DB, bus *event.Bus, frontURL string, defaultRate float64) *InviteService {
	if frontURL == "" {
		frontURL = "http://localhost:3000"
	}
	if defaultRate <= 0 {
		defaultRate = 0.05
	}
	return &InviteService{DB: db, bus: bus, frontURL: frontURL, defaultRate: defaultRate}
}

// generateInviteCode 生成邀请码：INV + base32(随机 8 字节)，去掉填充与易混字符。
func generateInviteCode() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// 极端情况下退回时间戳（保持可用性，不阻断业务）
		return "INV" + strings.ToUpper(time.Now().Format("060102150405.000"))
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return "INV" + enc
}

// Create 创建邀请码。
func (s *InviteService) Create(inviterID string, in model.InviteCreateInput) (*model.Invite, error) {
	// 邀请码碰撞重试（应用层唯一校验，避免软删与唯一索引冲突）
	var code string
	for i := 0; i < 5; i++ {
		candidate := generateInviteCode()
		exists, err := s.invites.FindByCode(s.DB, candidate)
		if err != nil {
			return nil, err
		}
		if exists == nil {
			code = candidate
			break
		}
	}
	if code == "" {
		return nil, errcode.ErrInternal.With("邀请码生成失败，请重试")
	}

	inv := &model.Invite{
		InviterID:      inviterID,
		InviteCode:     code,
		InviteURL:      s.frontURL + "/register?invite=" + code,
		Type:           model.InviteTypeGeneral,
		CommissionRate: s.defaultRate,
		Status:         model.InviteStatusPending,
	}
	if in.Type != nil && *in.Type != "" {
		inv.Type = *in.Type
	}
	if in.CommissionRate != nil && *in.CommissionRate > 0 {
		inv.CommissionRate = *in.CommissionRate
	}
	if in.ExpiredAt != nil {
		inv.ExpiredAt = in.ExpiredAt
	}
	if in.Remark != nil {
		inv.Remark = *in.Remark
	}
	if err := s.invites.Create(s.DB, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// Use 使用邀请码（把邀请记录置 USED，并发布 invite.used）。
func (s *InviteService) Use(inviteeID, code string) (*model.Invite, error) {
	inv, err := s.invites.FindByCode(s.DB, code)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, errcode.ErrNotFound.With("邀请码不存在")
	}
	if inv.Status != model.InviteStatusPending {
		return nil, errcode.ErrBadRequest.With("邀请码无效或已被使用")
	}
	if inv.ExpiredAt != nil && inv.ExpiredAt.Before(time.Now()) {
		inv.Status = model.InviteStatusExpired
		if err := s.invites.Save(s.DB, inv); err != nil {
			return nil, err
		}
		return nil, errcode.ErrBadRequest.With("邀请码已过期")
	}
	if inv.InviterID == inviteeID {
		return nil, errcode.ErrBadRequest.With("不能使用自己的邀请码")
	}

	now := time.Now()
	inv.InviteeID = inviteeID
	inv.Status = model.InviteStatusUsed
	inv.UsedAt = &now
	if err := s.invites.Save(s.DB, inv); err != nil {
		return nil, err
	}

	if s.bus != nil {
		s.bus.Publish(event.EventInviteUsed, event.InviteUsedPayload{
			InviteID:   inv.ID,
			InviterID:  inv.InviterID,
			InviteeID:  inviteeID,
			InviteCode: inv.InviteCode,
		})
	}
	return inv, nil
}

// Get 邀请详情（仅邀请人可看）。
func (s *InviteService) Get(id, inviterID string) (*model.Invite, error) {
	inv, err := s.invites.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, errcode.ErrNotFound.With("邀请不存在")
	}
	if inviterID != "" && inv.InviterID != inviterID {
		return nil, errcode.ErrForbidden.With("只能查看自己的邀请")
	}
	return inv, nil
}

// MyList 我的邀请列表。
func (s *InviteService) MyList(inviterID, keyword, status string, page, limit int) (*ListResult, error) {
	page, limit = normPage(page, limit)
	list, total, err := s.invites.ListByInviter(s.DB, inviterID, keyword, status, page, limit)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, page, limit)}, nil
}

// InviteStats 邀请统计。
type InviteStats struct {
	Total           int64   `json:"total"`
	Pending         int64   `json:"pending"`
	Used            int64   `json:"used"`
	Expired         int64   `json:"expired"`
	TotalEarnings   float64 `json:"totalEarnings"`
	PendingEarnings float64 `json:"pendingEarnings"`
	PaidEarnings    float64 `json:"paidEarnings"`
	CommissionCount int64   `json:"commissionCount"`
}

// Stats 统计。
func (s *InviteService) Stats(inviterID string) (*InviteStats, error) {
	st := &InviteStats{}
	var err error
	if st.Total, err = s.invites.CountByInviter(s.DB, inviterID); err != nil {
		return nil, err
	}
	if st.Pending, err = s.invites.CountByStatus(s.DB, inviterID, model.InviteStatusPending); err != nil {
		return nil, err
	}
	if st.Used, err = s.invites.CountByStatus(s.DB, inviterID, model.InviteStatusUsed); err != nil {
		return nil, err
	}
	if st.Expired, err = s.invites.CountByStatus(s.DB, inviterID, model.InviteStatusExpired); err != nil {
		return nil, err
	}
	if st.TotalEarnings, err = s.commissions.SumEarnings(s.DB, inviterID, ""); err != nil {
		return nil, err
	}
	if st.PendingEarnings, err = s.commissions.SumEarnings(s.DB, inviterID, model.InviteCommissionPending); err != nil {
		return nil, err
	}
	if st.PaidEarnings, err = s.commissions.SumEarnings(s.DB, inviterID, model.InviteCommissionPaid); err != nil {
		return nil, err
	}
	if st.CommissionCount, err = s.commissions.CountByStatus(s.DB, inviterID, model.InviteCommissionPaid); err != nil {
		return nil, err
	}
	return st, nil
}

// Earnings 收益（分成记录）分页。
func (s *InviteService) Earnings(userID string, page, limit int) (*ListResult, error) {
	page, limit = normPage(page, limit)
	list, total, err := s.commissions.ListByInviter(s.DB, userID, page, limit)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, page, limit)}, nil
}

// RecordCommission 记一笔邀请分成（由交易域在订单支付后调用）。
// amount 为订单金额，rate 取邀请记录上的分成比例；inviterID/inviteeID 由调用方给出。
func (s *InviteService) RecordCommission(inviteID, inviterID, inviteeID, orderID, orderType string, amount float64) (*model.InviteCommission, error) {
	inv, err := s.invites.FindByID(s.DB, inviteID)
	if err != nil {
		return nil, err
	}
	rate := s.defaultRate
	if inv != nil && inv.CommissionRate > 0 {
		rate = inv.CommissionRate
	}
	c := &model.InviteCommission{
		InviteID:         inviteID,
		InviterID:        inviterID,
		InviteeID:        inviteeID,
		OrderID:          orderID,
		OrderType:        orderType,
		OrderAmount:      amount,
		CommissionRate:   rate,
		CommissionAmount: amount * rate,
		Status:           model.InviteCommissionPending,
	}
	if err := s.commissions.Create(s.DB, c); err != nil {
		return nil, err
	}
	return c, nil
}

// MarkCommissionPaid 标记分成为已发放（钱包入账后调用）。
func (s *InviteService) MarkCommissionPaid(commissionID string) (*model.InviteCommission, error) {
	var c model.InviteCommission
	if err := s.DB.First(&c, "id = ?", commissionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.ErrNotFound.With("分成记录不存在")
		}
		return nil, err
	}
	now := time.Now()
	c.Status = model.InviteCommissionPaid
	c.PaidAt = &now
	if err := s.commissions.Save(s.DB, &c); err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(event.EventInviteCommissionPaid, event.InviteCommissionPaidPayload{
			CommissionID:     c.ID,
			InviterID:        c.InviterID,
			InviteeID:        c.InviteeID,
			OrderID:          c.OrderID,
			CommissionAmount: c.CommissionAmount,
		})
	}
	return &c, nil
}
