// 系统配置 handler（13 个操作 + 按 id/key 直取扩展，对齐 swagger tag「系统配置」）。
//
// 端点与权限（原 config.controller.ts 的 @Permissions，另兼容任务书约定的 config:manage 命名）：
//
//	GET    /config                          setting:read         全部配置
//	POST   /config                          setting:create       创建配置
//	PATCH  /config                          setting:update       批量更新配置
//	GET    /config/group/:group             setting:read         按分组取配置
//	PATCH  /config/group/:group             setting:update       按分组批量更新
//	GET    /config/public                   —（NoAuth）          公开配置（按分组聚合）
//	GET    /config/advertisement            —（NoAuth）          广告配置
//	GET    /config/key/:key                 setting:read         按 key 取配置
//	GET    /config/:id                      setting:read         按 id 取配置
//	PATCH  /config/:id                      setting:update       按 id 更新配置
//	DELETE /config/:id                      setting:update       删除配置
//	GET    /config/sensitive-words          setting:read         敏感词列表
//	POST   /config/sensitive-words          setting:update       添加敏感词
//	DELETE /config/sensitive-words          setting:update       删除敏感词（body: {word}）
//	POST   /config/sensitive-words/batch    setting:update       批量添加
//	DELETE /config/sensitive-words/batch    setting:update       批量删除
//	POST   /config/sensitive-words/clear-cache setting:update    清除敏感词缓存
//
// 敏感词接口响应保留原实现的 {success, data|message} 形态，便于前端零改动对接。
//
// 敏感项打码（管理端读契约，见 infra.ConfigView）：
//
//	GET /config、/config/group/:group、/config/key/:key、/config/:id 均返回 ConfigView，
//	其中 s3_access_key / s3_secret_key / search_password 等敏感项的 value 恒为 "***"
//	（库中值为空时为 ""），并附 hasValue（后端是否已配置）。非敏感项 value 原样返回。
//	PATCH 时 value 传空串或 "***" 表示「保持原值」，敏感项不会被清空；
//	批量更新响应新增 skipped 字段（未命中 key + 被跳过的敏感项条数），
//	updated 语义收敛为「真实写库条数」（原先等于请求条数）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// ConfigHandler 系统配置端点。
type ConfigHandler struct {
	svc *infra.ConfigService
}

// NewConfigHandler 构造配置 handler。
func NewConfigHandler(svc *infra.ConfigService) *ConfigHandler {
	return &ConfigHandler{svc: svc}
}

// List GET /config。
//
//	@Summary	配置列表（管理端）
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.ConfigDto}
//	@ID			ConfigController_findAll
//	@Router		/config [get]
func (h *ConfigHandler) List(c *gin.Context) {
	list, err := h.svc.List()
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, list)
}

// Create POST /config。
//
//	@Summary	创建配置项
//	@Tags		系统配置
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.ConfigRequest true "配置项入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigDto}
//	@ID			ConfigController_create
//	@Router		/config [post]
func (h *ConfigHandler) Create(c *gin.Context) {
	var in infra.ConfigInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigKeyRequired)
		return
	}
	out, err := h.svc.Create(in)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, out)
}

// UpdateAll PATCH /config（body: [{key,value}]）。
//
//	@Summary	批量更新配置
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body []dto.ConfigRequest true "配置数组（批量更新）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigUpdateResultDto}
//	@ID			ConfigController_updateAll
//	@Router		/config [patch]
func (h *ConfigHandler) UpdateAll(c *gin.Context) {
	var items []infra.ConfigInput
	if err := c.ShouldBindJSON(&items); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigBodyArrayRequired)
		return
	}
	updated, skipped, err := h.svc.UpdateAll(items)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	// updated 为真实写库条数；skipped 为「未命中的 key + 敏感项空值/*** 占位符」条数
	response.Success(c, dto.ConfigUpdateResultDto{Updated: updated, Skipped: skipped})
}

// ByGroup GET /config/group/:group。
//
//	@Summary	按分组读配置
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		group path string true "分组名 example(upload)"
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.ConfigDto}
//	@ID			ConfigController_findByGroup
//	@Router		/config/group/{group} [get]
func (h *ConfigHandler) ByGroup(c *gin.Context) {
	list, err := h.svc.ByGroup(c.Param("group"))
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, list)
}

// UpdateGroup PATCH /config/group/:group。
//
//	@Summary	按分组更新配置
//	@Tags		系统配置
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		group path string true "分组名 example(upload)"
//	@Param		body body []dto.ConfigRequest true "配置数组（按分组批量更新）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigUpdateResultDto}
//	@ID			ConfigController_updateGroup
//	@Router		/config/group/{group} [patch]
func (h *ConfigHandler) UpdateGroup(c *gin.Context) {
	var items []infra.ConfigInput
	if err := c.ShouldBindJSON(&items); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigBodyArrayRequired)
		return
	}
	updated, skipped, err := h.svc.UpdateGroup(c.Param("group"), items)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, dto.ConfigUpdateResultDto{Updated: updated, Skipped: skipped})
}

// Public GET /config/public（NoAuth）。
//
//	@Summary	公开配置
//	@Tags		系统配置
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.PublicConfigsDto}
//	@ID			ConfigController_getPublicConfigs
//	@Router		/config/public [get]
func (h *ConfigHandler) Public(c *gin.Context) {
	res, err := h.svc.PublicConfigs()
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// Advertisement GET /config/advertisement（NoAuth）。
//
//	@Summary	广告位配置
//	@Tags		系统配置
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.AdvertisementConfigDto}
//	@ID			ConfigController_getAdvertisementConfig
//	@Router		/config/advertisement [get]
func (h *ConfigHandler) Advertisement(c *gin.Context) {
	res, err := h.svc.AdvertisementConfig()
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// ByKey GET /config/key/:key。
//
//	@Summary	按键读配置
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		key path string true "配置键 example(upload_max_image_size)"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigDto}
//	@ID			ConfigController_findByKey
//	@Router		/config/key/{key} [get]
func (h *ConfigHandler) ByKey(c *gin.Context) {
	cfg, err := h.svc.ByKey(c.Param("key"))
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, cfg)
}

// ByID GET /config/:id。
//
//	@Summary	配置项详情
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "配置 ID example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigDto}
//	@ID			ConfigController_findOne
//	@Router		/config/{id} [get]
func (h *ConfigHandler) ByID(c *gin.Context) {
	cfg, err := h.svc.ByID(c.Param("id"))
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, cfg)
}

// UpdateByID PATCH /config/:id。
//
//	@Summary	按 ID 更新配置项
//	@Tags		系统配置
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Param		body body dto.ConfigRequest true "配置项入参（PATCH 部分更新）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ConfigDto}
//	@ID			ConfigController_update
//	@Router		/config/{id} [patch]
func (h *ConfigHandler) UpdateByID(c *gin.Context) {
	var in infra.ConfigInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.CommonInvalidParam)
		return
	}
	out, err := h.svc.Update(c.Param("id"), in)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, out)
}

// Remove DELETE /config/:id。
//
//	@Summary	删除配置项
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "配置 ID example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletedFlagDto}
//	@ID			ConfigController_remove
//	@Router		/config/{id} [delete]
func (h *ConfigHandler) Remove(c *gin.Context) {
	if err := h.svc.Remove(c.Param("id")); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, dto.DeletedFlagDto{Deleted: true})
}

// ---------- 敏感词 ----------

// ListSensitiveWords GET /config/sensitive-words。
//
//	@Summary	敏感词列表
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordListDto}
//	@ID			ConfigController_findAllSensitiveWords
//	@Router		/config/sensitive-words [get]
func (h *ConfigHandler) ListSensitiveWords(c *gin.Context) {
	words, err := h.svc.SensitiveWords()
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	// words 为 nil 时序列化成 null —— 先兜底成空切片（docs/21 规则二）
	if words == nil {
		words = []string{}
	}
	response.Success(c, dto.SensitiveWordListDto{Success: true, Words: words, Total: len(words)})
}

// CreateSensitiveWord POST /config/sensitive-words（body: {word}）。
//
//	@Summary	新增敏感词
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SensitiveWordRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordActionDto}
//	@ID			ConfigController_createSensitiveWord
//	@Router		/config/sensitive-words [post]
func (h *ConfigHandler) CreateSensitiveWord(c *gin.Context) {
	var body dto.SensitiveWordRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigSensitiveEmpty)
		return
	}
	if err := h.svc.AddSensitiveWord(body.Word); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.SuccessMsg(c, i18n.ConfigSensitiveAdded, dto.SensitiveWordActionDto{Success: true})
}

// RemoveSensitiveWord DELETE /config/sensitive-words（body: {word}）。
//
//	@Summary	删除敏感词
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SensitiveWordRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordActionDto}
//	@ID			ConfigController_removeSensitiveWord
//	@Router		/config/sensitive-words [delete]
func (h *ConfigHandler) RemoveSensitiveWord(c *gin.Context) {
	var body dto.SensitiveWordRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigSensitiveEmpty)
		return
	}
	if err := h.svc.RemoveSensitiveWord(body.Word); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.SuccessMsg(c, i18n.ConfigSensitiveRemoved, dto.SensitiveWordActionDto{Success: true})
}

// BatchCreateSensitiveWords POST /config/sensitive-words/batch（body: {words:[...]}）。
//
//	@Summary	批量导入敏感词
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SensitiveWordBatchRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordBatchResultDto}
//	@ID			ConfigController_batchCreateSensitiveWords
//	@Router		/config/sensitive-words/batch [post]
func (h *ConfigHandler) BatchCreateSensitiveWords(c *gin.Context) {
	var body dto.SensitiveWordBatchRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigSensitiveEmpty)
		return
	}
	added, err := h.svc.BatchAddSensitiveWords(body.Words)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, dto.SensitiveWordBatchResultDto{
		Success: true,
		Message: i18n.Tf(i18n.ConfigSensitiveBatchAdded, len(body.Words)),
		Added:   added,
	})
}

// BatchRemoveSensitiveWords DELETE /config/sensitive-words/batch（body: {words:[...]}）。
//
//	@Summary	批量删除敏感词
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SensitiveWordBatchRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordBatchResultDto}
//	@ID			ConfigController_batchRemoveSensitiveWords
//	@Router		/config/sensitive-words/batch [delete]
func (h *ConfigHandler) BatchRemoveSensitiveWords(c *gin.Context) {
	var body dto.SensitiveWordBatchRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.ConfigSensitiveEmpty)
		return
	}
	removed, err := h.svc.BatchRemoveSensitiveWords(body.Words)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, dto.SensitiveWordBatchResultDto{
		Success: true,
		Message: i18n.Tf(i18n.ConfigSensitiveBatchRemoved, len(body.Words)),
		Removed: removed,
	})
}

// ClearSensitiveWordsCache POST /config/sensitive-words/clear-cache（对齐 refreshConfigCache）。
//
//	@Summary	清空敏感词缓存
//	@Tags		系统配置
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.SensitiveWordCacheClearDto}
//	@ID			ConfigController_clearSensitiveWordsCache
//	@Router		/config/sensitive-words/clear-cache [post]
func (h *ConfigHandler) ClearSensitiveWordsCache(c *gin.Context) {
	h.svc.RefreshCache(infra.ConfigKeySensitiveWords)
	response.SuccessMsg(c, i18n.ConfigCacheCleared, dto.SensitiveWordCacheClearDto{
		Success: true,
		Total:   h.svc.TrieSize(),
	})
}

// 让 dto 包成为本文件的真实 import（swag 只按「注解所在文件的 import 列表」解析
// 跨包类型，docs/18 §三之一 约束 B）。本文件的注解写了 dto.Xxx，这里引用一次。
// 不参与运行逻辑。
var _ = dto.ResponseBody{}
