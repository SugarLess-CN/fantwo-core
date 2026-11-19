// 表情包域 HTTP 层（原 EmojiController 13 个操作 + EmojiPackController 8 个）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// emojiAllowedMime 表情可接受的 MIME（对齐原 EMOJI_ALLOWED_MIME_TYPES）。
var emojiAllowedMime = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
}

// emojiMaxSize 表情文件大小上限 5MB（对齐原实现）。
const emojiMaxSize = 5 * 1024 * 1024

// EmojiCreate POST /emoji。
func (h *OpsHandler) EmojiCreate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.EmojiInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	e, err := h.Emoji.Create(uid, in, isAdmin(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, e)
}

// EmojiUpload POST /emoji/upload（multipart）。
// 落盘归上传域：本端点做类型/大小校验后，把持久化交给上传域（尚未接入），
// 接入前明确报错而不是静默失败；已上传到 /upload 的图片可直接走 POST /emoji 传 URL。
func (h *OpsHandler) EmojiUpload(c *gin.Context) {
	if requireUser(c) == "" {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.FailWith(c, errcode.ErrBadRequest, "请选择要上传的表情文件")
		return
	}
	mime := file.Header.Get("Content-Type")
	if !emojiAllowedMime[mime] {
		response.FailWith(c, errcode.ErrBadRequest, "不支持的文件类型")
		return
	}
	if file.Size > emojiMaxSize {
		response.FailWith(c, errcode.ErrBadRequest, "文件过大（上限 5MB）")
		return
	}
	response.FailWith(c, errcode.ErrInternal,
		"表情上传落盘由上传域负责（尚未接入）；请先调用 POST /upload 上传，再用返回的 URL 调 POST /emoji")
}

// EmojiList GET /emoji（公开；登录后可见自己的私有表情与收藏过滤）。
func (h *OpsHandler) EmojiList(c *gin.Context) {
	q := model.EmojiQuery{
		Page:          atoiDefault(c.Query("page"), 1),
		Limit:         atoiDefault(c.Query("limit"), 0),
		Grouped:       boolPtr(c.Query("grouped")),
		Type:          c.Query("type"),
		Category:      c.Query("category"),
		Keyword:       c.Query("keyword"),
		IsPublic:      boolPtr(c.Query("isPublic")),
		Status:        c.Query("status"),
		UserID:        c.Query("userId"),
		OnlyFavorites: c.Query("onlyFavorites") == "true",
		SortBy:        c.Query("sortBy"),
		SortOrder:     c.Query("sortOrder"),
	}
	if q.OnlyFavorites && currentUserID(c) == "" {
		response.Fail(c, errcode.ErrUnauthorized)
		return
	}
	lr, err := h.Emoji.List(q, currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// EmojiGet GET /emoji/:id（公开）。
func (h *OpsHandler) EmojiGet(c *gin.Context) {
	e, err := h.Emoji.Get(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, e)
}

// EmojiUpdate PATCH /emoji/:id。
func (h *OpsHandler) EmojiUpdate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.EmojiInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	e, err := h.Emoji.Update(uid, c.Param("id"), in, isAdmin(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, e)
}

// EmojiDelete DELETE /emoji/:id。
func (h *OpsHandler) EmojiDelete(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	if err := h.Emoji.Delete(uid, c.Param("id"), isAdmin(c)); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// EmojiCategories GET /emoji/categories/list（公开）。
func (h *OpsHandler) EmojiCategories(c *gin.Context) {
	cats, err := h.Emoji.Categories()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, cats)
}

// EmojiPopular GET /emoji/popular/list（公开）。
func (h *OpsHandler) EmojiPopular(c *gin.Context) {
	list, err := h.Emoji.Popular(atoiDefault(c.Query("limit"), 20))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// EmojiRecent GET /emoji/recent/list。
func (h *OpsHandler) EmojiRecent(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	list, err := h.Emoji.Recent(uid, atoiDefault(c.Query("limit"), 20))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// EmojiFavorites GET /emoji/favorites/list。
func (h *OpsHandler) EmojiFavorites(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	lr, err := h.Emoji.Favorites(uid, atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// EmojiFavoriteAdd POST /emoji/:id/favorite。
func (h *OpsHandler) EmojiFavoriteAdd(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	if err := h.Emoji.FavoriteAdd(uid, c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"favorited": true})
}

// EmojiFavoriteRemove DELETE /emoji/:id/favorite。
func (h *OpsHandler) EmojiFavoriteRemove(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	if err := h.Emoji.FavoriteRemove(uid, c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"favorited": false})
}

// EmojiUse POST /emoji/:id/use。
func (h *OpsHandler) EmojiUse(c *gin.Context) {
	if err := h.Emoji.Use(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"incremented": true})
}

// ---- 表情包 ----

// EmojiPackCreate POST /emoji/packs。
func (h *OpsHandler) EmojiPackCreate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.EmojiPackInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	p, err := h.Emoji.PackCreate(uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, p)
}

// EmojiPackList GET /emoji/packs（公开：公开包 ∪ 自己的）。
func (h *OpsHandler) EmojiPackList(c *gin.Context) {
	q := model.EmojiPackQuery{
		Page:    atoiDefault(c.Query("page"), 1),
		Limit:   atoiDefault(c.Query("limit"), 0),
		UserID:  c.Query("userId"),
		Keyword: c.Query("keyword"),
		Status:  c.Query("status"),
	}
	lr, err := h.Emoji.PackList(q, currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// EmojiPackGet GET /emoji/packs/:id（公开 + 内容列表）。
func (h *OpsHandler) EmojiPackGet(c *gin.Context) {
	detail, err := h.Emoji.PackDetail(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, detail)
}

// EmojiPackUpdate PATCH /emoji/packs/:id。
func (h *OpsHandler) EmojiPackUpdate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.EmojiPackInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	p, err := h.Emoji.PackUpdate(uid, c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, p)
}

// EmojiPackDelete DELETE /emoji/packs/:id。
func (h *OpsHandler) EmojiPackDelete(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	if err := h.Emoji.PackDelete(uid, c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// EmojiPackAddEmoji POST /emoji/packs/:id/emojis。
func (h *OpsHandler) EmojiPackAddEmoji(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body struct {
		EmojiID string `json:"emojiId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	item, err := h.Emoji.PackAddEmoji(uid, c.Param("id"), body.EmojiID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, item)
}

// EmojiPackUpdateEmoji PATCH /emoji/packs/:id/emojis/:emojiId。
func (h *OpsHandler) EmojiPackUpdateEmoji(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body struct {
		Sort int `json:"sort"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	item, err := h.Emoji.PackUpdateEmojiSort(uid, c.Param("id"), c.Param("emojiId"), body.Sort)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, item)
}

// EmojiPackRemoveEmoji DELETE /emoji/packs/:id/emojis/:emojiId。
func (h *OpsHandler) EmojiPackRemoveEmoji(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	if err := h.Emoji.PackRemoveEmoji(uid, c.Param("id"), c.Param("emojiId")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"removed": true})
}
