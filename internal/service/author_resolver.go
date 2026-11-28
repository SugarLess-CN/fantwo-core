// Package service 用户域对其它业务域暴露的跨域能力。
//
// 两个接口/实现：
//   - AuthorResolver（内容域定义）：批量解析作者展示信息，避免 N+1；
//   - FollowProvider（本文件定义）：提供「关注流」所需的 followee ID 集合，
//     内容域 /article?type=following 直接消费，不再自行查关注表。
package service

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
)

// FollowProvider 关注关系提供者（跨域接口，由 user 域实现）。
type FollowProvider interface {
	// FollowingIDs 返回 userID 关注的用户 ID 列表（空列表表示未关注任何人）。
	FollowingIDs(ctx context.Context, userID string) ([]string, error)
}

// UserAuthorResolver 作者信息解析器实现（AuthorResolver + FollowProvider）。
type UserAuthorResolver struct {
	d *Deps
}

// NewUserAuthorResolver 构造。
func NewUserAuthorResolver(d *Deps) *UserAuthorResolver { return &UserAuthorResolver{d: d} }

// ResolveAuthors 批量解析作者简要信息（单次 IN 查询，无 N+1）。
// DB 不可用或查询失败时返回空 map（内容域视图退化为只输出 authorId，不报错）。
func (r *UserAuthorResolver) ResolveAuthors(ctx context.Context, userIDs []string) (map[string]AuthorBrief, error) {
	out := make(map[string]AuthorBrief, len(userIDs))
	if r == nil || r.d == nil || r.d.DB == nil || len(userIDs) == 0 {
		return out, nil
	}
	// 去重，避免 IN 列表重复。
	uniq := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var users []model.User
	if err := r.d.DB.Select("id", "username", "nickname", "avatar", "nick_id").
		Where("id IN ?", uniq).Find(&users).Error; err != nil {
		r.d.Logger.Warn("ResolveAuthors 查询失败: " + err.Error())
		return out, err
	}
	// 作者佩戴的装扮：**一次批量查询**（与用户域共用 batchEquippedDecorations）。
	//
	// 此前 AuthorBriefDto.equipped_decorations 只有声明、全仓无填充点 ——
	// 文章列表 / 评论区的作者头像框因此一直不显示。
	decos := batchEquippedDecorations(r.d.DB, r.d.Logger, uniq)
	for i := range users {
		u := &users[i]
		nickID, nickIDStr := nickIDPair(u.NickID)
		brief := AuthorBrief{
			ID: u.ID, Username: u.Username, Avatar: u.Avatar,
			NickID: nickID, NickIDStr: nickIDStr,
		}
		if u.Nickname != nil && *u.Nickname != "" {
			brief.Nickname = *u.Nickname
		} else {
			brief.Nickname = u.Username
		}
		// 没装扮时保持 nil + omitempty → 整个字段不传（不是 null）
		if d, ok := decos[u.ID]; ok {
			brief.EquippedDecorations = d
		}
		out[u.ID] = brief
	}
	return out, nil
}

// FollowingIDs 关注列表（FollowProvider 实现；仅 ID，供关注流过滤）。
func (r *UserAuthorResolver) FollowingIDs(ctx context.Context, userID string) ([]string, error) {
	if r == nil || r.d == nil || r.d.DB == nil || userID == "" {
		return []string{}, nil
	}
	ids, err := r.d.Users.FollowingIDs(r.d.DB, userID)
	if err != nil {
		return []string{}, err
	}
	return ids, nil
}

// FollowingIDs 用户服务侧同一能力（handler 或其它 service 直接调用）。
func (s *UserService) FollowingIDs(ctx context.Context, userID string) ([]string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	ids, err := s.d.Users.FollowingIDs(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return ids, nil
}
