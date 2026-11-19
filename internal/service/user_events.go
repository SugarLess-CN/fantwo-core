// Package service 用户域跨域事件订阅。
//
// 归属（AGENTS.md 跨域事件订阅决策，2026-11-19）：
//   - report.handled  → **auth-user 域**监听后封禁用户（content 域负责删文章/评论）；
//   - invite.used     → **auth-user 域**监听后回写被邀请人的 inviter_id/invite_code
//     与邀请人的 invite_count（ops 只把 invite 状态置 USED，不写 users 表）。
//
// payload 兼容两种形态：event 包定义的结构体，或 map[string]interface{}
// （其它域若直接 publish map 也能工作）。
package service

import (
	"context"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
)

// RegisterUserEventSubscribers 注册用户域订阅者（装配层调用一次，幂等性由装配层保证）。
func RegisterUserEventSubscribers(d *Deps) {
	if d == nil {
		return
	}
	bus := event.Default()
	bus.Subscribe(event.EventReportHandled, func(payload interface{}) {
		handleReportHandled(d, payload)
	})
	bus.Subscribe(event.EventInviteUsed, func(payload interface{}) {
		handleInviteUsed(d, payload)
	})
}

// handleReportHandled 举报处置 → 封禁被举报用户。
// 仅当 action 为 BAN_USER 且带 reported_user_id 时执行。
func handleReportHandled(d *Deps, payload interface{}) {
	if d.DB == nil {
		return
	}
	var (
		action string
		userID string
	)
	switch p := payload.(type) {
	case event.ReportHandledPayload:
		action, userID = p.Action, p.ReportedUserID
	case *event.ReportHandledPayload:
		if p != nil {
			action, userID = p.Action, p.ReportedUserID
		}
	case map[string]interface{}:
		action = strOf(p["action"])
		userID = strOf(p["reported_user_id"])
		if userID == "" {
			userID = strOf(p["reportedUserId"])
		}
	default:
		return
	}
	if action != "BAN_USER" || userID == "" {
		return
	}
	now := time.Now()
	reason := "举报处置：内容违规"
	if err := d.Users.UpdateFields(d.DB, userID, map[string]interface{}{
		"status":     model.UserStatusBanned,
		"banned":     now,
		"ban_reason": reason,
	}); err != nil {
		d.Logger.Warn("report.handled 封禁用户失败 userID=" + userID)
		return
	}
	// 封禁后立即失效全部 token。
	ctx, cancel := bgCtx()
	defer cancel()
	_ = authtoken.SetLogoutAll(ctx, d.Redis, userID, now)
	d.Logger.Info("report.handled 已封禁用户 " + userID)
}

// handleInviteUsed 邀请码使用 → 回写邀请关系与计数。
func handleInviteUsed(d *Deps, payload interface{}) {
	if d.DB == nil {
		return
	}
	var inviter, invitee, code string
	switch p := payload.(type) {
	case event.InviteUsedPayload:
		inviter, invitee, code = p.InviterID, p.InviteeID, p.InviteCode
	case *event.InviteUsedPayload:
		if p != nil {
			inviter, invitee, code = p.InviterID, p.InviteeID, p.InviteCode
		}
	case map[string]interface{}:
		inviter = strOf(p["inviter_id"])
		invitee = strOf(p["invitee_id"])
		code = strOf(p["invite_code"])
	default:
		return
	}
	if invitee == "" {
		return
	}
	fields := map[string]interface{}{}
	if inviter != "" {
		fields["inviter_id"] = inviter
	}
	if code != "" {
		fields["invite_code"] = code
	}
	if len(fields) > 0 {
		if err := d.Users.UpdateFields(d.DB, invitee, fields); err != nil {
			d.Logger.Warn("invite.used 回写被邀请人失败 userID=" + invitee)
		}
	}
	if inviter != "" {
		if err := d.Users.IncrCounter(d.DB, inviter, "invite_count", 1); err != nil {
			d.Logger.Warn("invite.used 回写邀请人计数失败 userID=" + inviter)
		}
	}
}

// strOf 任意值取字符串（payload 兼容 map 形态用）。
func strOf(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// BanUser 封禁用户（管理员/内部复用；空原因走默认文案）。
func (s *UserService) BanUser(ctx context.Context, userID, reason string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if userID == "" {
		return errcode.ErrInvalidParam
	}
	if reason == "" {
		reason = "账号违规"
	}
	now := time.Now()
	if err := s.d.Users.UpdateFields(s.d.DB, userID, map[string]interface{}{
		"status": model.UserStatusBanned, "banned": now, "ban_reason": reason,
	}); err != nil {
		return errcode.ErrInternal
	}
	_ = authtoken.SetLogoutAll(ctx, s.d.Redis, userID, now)
	return nil
}
