// 表情包域 HTTP 层（原 EmojiController 13 个操作 + EmojiPackController 8 个）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
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
//
//	@Summary	创建表情
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body model.EmojiInput true "emojiRequest"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_create
//	@Router		/emoji [post]
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
//
//	@Summary	上传表情（走图片审核队列）
//	@Tags		表情包
//	@Accept		multipart/form-data
//	@Produce	json
//	@Security	BearerAuth
//	@Param		file formData file true "表情图片文件"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_upload
//	@Router		/emoji/upload [post]
func (h *OpsHandler) EmojiUpload(c *gin.Context) {
	if requireUser(c) == "" {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.UploadNoFile)
		return
	}
	mime := file.Header.Get("Content-Type")
	if !emojiAllowedMime[mime] {
		response.FailKey(c, errcode.ErrBadRequest, i18n.UploadTypeNotAllowed, mime)
		return
	}
	if file.Size > emojiMaxSize {
		response.FailKey(c, errcode.ErrBadRequest, i18n.UploadFileTooLarge, float64(file.Size)/(1024*1024), 5)
		return
	}
	response.FailKey(c, errcode.ErrInternal, i18n.EmojiUploadNotAvailable)
}

// EmojiList GET /emoji（公开；登录后可见自己的私有表情与收藏过滤）。
//
//	@Summary	表情列表
//	@Tags		表情包
//	@Produce	json
//	@Success	200	{object}	ResponsePageBody
//	@ID				EmojiController_findAll
//	@Router		/emoji [get]
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
//
//	@Summary	表情详情
//	@Tags		表情包
//	@Produce	json
//	@Param		id path string true "表情 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_findOne
//	@Router		/emoji/{id} [get]
func (h *OpsHandler) EmojiGet(c *gin.Context) {
	e, err := h.Emoji.Get(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, e)
}

// EmojiUpdate PATCH /emoji/:id。
//
//	@Summary	更新表情
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body model.EmojiInput true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_update
//	@Router		/emoji/{id} [patch]
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
//
//	@Summary	删除表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_remove
//	@Router		/emoji/{id} [delete]
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
//
//	@Summary	表情分类
//	@Tags		表情包
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_getCategories
//	@Router		/emoji/categories/list [get]
func (h *OpsHandler) EmojiCategories(c *gin.Context) {
	cats, err := h.Emoji.Categories()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, cats)
}

// EmojiPopular GET /emoji/popular/list（公开）。
//
//	@Summary	热门表情
//	@Tags		表情包
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_getPopular
//	@Router		/emoji/popular/list [get]
func (h *OpsHandler) EmojiPopular(c *gin.Context) {
	list, err := h.Emoji.Popular(atoiDefault(c.Query("limit"), 20))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// EmojiRecent GET /emoji/recent/list。
//
//	@Summary	我最近使用的表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_getRecent
//	@Router		/emoji/recent/list [get]
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
//
//	@Summary	我收藏的表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_getFavorites
//	@Router		/emoji/favorites/list [get]
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
//
//	@Summary	收藏表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "表情 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_addToFavorites
//	@Router		/emoji/{id}/favorite [post]
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
//
//	@Summary	取消收藏表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_removeFromFavorites
//	@Router		/emoji/{id}/favorite [delete]
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
//
//	@Summary	记录表情使用
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				EmojiController_incrementUseCount
//	@Router		/emoji/{id}/use [post]
func (h *OpsHandler) EmojiUse(c *gin.Context) {
	if err := h.Emoji.Use(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"incremented": true})
}

// ---- 表情包 ----

// EmojiPackCreate POST /emoji/packs。
//
//	@Summary	创建表情包
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body model.EmojiPackInput true "emojiPackRequest"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackCreate
//	@Router		/emoji/packs [post]
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
//
//	@Summary	表情包列表
//	@Tags		表情包
//	@Produce	json
//	@Success	200	{object}	ResponsePageBody
//	@ID				OpsHandler_EmojiPackList
//	@Router		/emoji/packs [get]
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
//
//	@Summary	表情包详情
//	@Tags		表情包
//	@Produce	json
//	@Param		id path string true "表情包 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackGet
//	@Router		/emoji/packs/{id} [get]
func (h *OpsHandler) EmojiPackGet(c *gin.Context) {
	detail, err := h.Emoji.PackDetail(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, detail)
}

// EmojiPackUpdate PATCH /emoji/packs/:id。
//
//	@Summary	更新表情包
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body model.EmojiPackInput true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackUpdate
//	@Router		/emoji/packs/{id} [patch]
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
//
//	@Summary	删除表情包
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackDelete
//	@Router		/emoji/packs/{id} [delete]
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
//
//	@Summary	向表情包添加表情
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body EmojiPackItemRequest true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackAddEmoji
//	@Router		/emoji/packs/{id}/emojis [post]
func (h *OpsHandler) EmojiPackAddEmoji(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body EmojiPackItemRequest
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
//
//	@Summary	更新包内表情
//	@Tags		表情包
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		emojiId path string true "emojiId"
//	@Param		body body EmojiPackItemSortRequest true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackUpdateEmoji
//	@Router		/emoji/packs/{id}/emojis/{emojiId} [patch]
func (h *OpsHandler) EmojiPackUpdateEmoji(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body EmojiPackItemSortRequest
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
//
//	@Summary	从表情包移除表情
//	@Tags		表情包
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		emojiId path string true "emojiId"
//	@Success	200	{object}	ResponseBody
//	@ID				OpsHandler_EmojiPackRemoveEmoji
//	@Router		/emoji/packs/{id}/emojis/{emojiId} [delete]
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

// ---- 请求体 DTO（注解引用，swag 按包内类型解析）----

// EmojiPackItemSortRequest 表情包内表情排序请求体。
type EmojiPackItemSortRequest struct {
	Sort int `json:"sort"`
}

// EmojiPackItemRequest 表情包添加表情请求体。
type EmojiPackItemRequest struct {
	EmojiID string `json:"emojiId" binding:"required"`
}
