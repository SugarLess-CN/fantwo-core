// 上传管理 handler（10 个操作 + 删除，对齐 swagger tag「上传管理」）。
//
// 端点与权限（原 upload.controller.ts 的 @Permissions）：
//
//	GET    /upload                     upload:list      文件列表（分页）
//	GET    /upload/info/:id            —（NoAuth）      文件信息
//	GET    /upload/config              —（NoAuth）      上传配置（前端预压缩用）
//	POST   /upload/file                upload:create    上传文件（multipart）
//	POST   /upload/avatar              upload:create    上传头像（固定 scene=avatar，无水印）
//	POST   /upload/presign             upload:create    S3 预签名直传 URL
//	POST   /upload/confirm             upload:create    确认 S3 直传完成
//	GET    /upload/migration/stats     upload:manage    压缩处理统计
//	POST   /upload/migration/process   upload:manage    批量处理未压缩图片
//	POST   /upload/migration/metadata  upload:manage    补充缺失元数据
//	DELETE /upload/:id                 upload:manage    删除文件（引用计数 -1）
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// UploadHandler 上传管理端点。
type UploadHandler struct {
	svc *infra.Infra
}

// NewUploadHandler 构造上传 handler。
func NewUploadHandler(svc *infra.Infra) *UploadHandler { return &UploadHandler{svc: svc} }

// List GET /upload（对齐 findAll：page/limit/sortBy/sortOrder）。
//
//	@Summary	上传记录列表
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@Router		/upload [get]
func (h *UploadHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	// 归一化后再传给分页包装（limit=0 会导致 TotalPages 除零）
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	// 权限降级：有 upload:list 看全站（原实现语义）；否则只返回自己的上传，
	// 避免普通登录用户被 403 挡住「我的上传」这类前端场景，同时不泄露他人文件。
	onlyUserID := ""
	if !infraHasPermission(c, "upload:list") {
		onlyUserID = infraCurrentUserID(c)
		if onlyUserID == "" {
			response.Fail(c, errcode.ErrForbidden)
			return
		}
	}
	list, total, err := h.svc.ListUpload(page, limit, c.Query("sortBy"), c.Query("sortOrder"), onlyUserID)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, up := range list {
		out = append(out, infraFormatUpload(up))
	}
	response.SuccessPage(c, out, total, page, limit)
}

// Info GET /upload/info/:id（NoAuth；不存在返回 data:null，对齐原实现 findOne 返回 null）。
//
//	@Summary	上传记录详情
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "上传记录 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/info/{id} [get]
func (h *UploadHandler) Info(c *gin.Context) {
	up, err := h.svc.GetFileInfo(c.Param("id"))
	if err != nil {
		if e, ok := err.(*errcode.Err); ok && e.Code == http.StatusNotFound {
			response.Success(c, nil) // 原实现 return null 而非 404
			return
		}
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, infraFormatUpload(*up))
}

// Config GET /upload/config（NoAuth）。
//
//	@Summary	上传限制配置
//	@Tags		上传
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/config [get]
func (h *UploadHandler) Config(c *gin.Context) {
	response.Success(c, h.svc.GetUploadConfig(c))
}

// UploadFile POST /upload/file（multipart，任意字段名，scene 默认 image）。
//
//	@Summary	上传文件
//	@Tags		上传
//	@Accept		multipart/form-data
//	@Produce	json
//	@Security	BearerAuth
//	@Param		file formData file true "文件本体"
//	@Param		scene formData string false "场景：article/avatar/comment"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/file [post]
func (h *UploadHandler) UploadFile(c *gin.Context) { h.upload(c, "") }

// UploadAvatar POST /upload/avatar（固定 scene=avatar，不加水印）。
//
//	@Summary	上传头像（走图片审核）
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/avatar [post]
func (h *UploadHandler) UploadAvatar(c *gin.Context) { h.upload(c, infra.SceneAvatar) }

// upload 两个上传端点的共用实现（对齐 UploadController.handleUpload）。
func (h *UploadHandler) upload(c *gin.Context, forcedScene string) {
	form, err := c.MultipartForm()
	if err != nil {
		response.FailWith(c, errcode.ErrBadRequest, "请使用 multipart/form-data 上传文件")
		return
	}
	// AnyFilesInterceptor 语义：任意字段名都收（优先 files，其次第一个非元数据字段）
	files := form.File["files"]
	if len(files) == 0 {
		for field, fhs := range form.File {
			if field == "metadata" || field == "scene" {
				continue
			}
			files = fhs
			break
		}
	}
	if len(files) == 0 {
		response.FailWith(c, errcode.ErrBadRequest, "上传文件不能为空")
		return
	}

	metadata, err := infraParseUploadMetadata(infraFirstValue(form.Value["metadata"]))
	if err != nil {
		response.FailWith(c, errcode.ErrBadRequest, "metadata 必须是合法的 JSON 数组")
		return
	}

	scene := forcedScene
	if scene == "" {
		scene = infraFirstValue(form.Value["scene"])
	}
	if scene == "" {
		scene = infra.SceneImage
	}

	uploads, err := h.svc.HandleUpload(files, metadata, scene, infraRequestBaseURL(c), infraCurrentUserID(c))
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	out := make([]gin.H, 0, len(uploads))
	for _, up := range uploads {
		out = append(out, infraFormatUpload(up))
	}
	response.Success(c, out)
}

// Presign POST /upload/presign（S3 直传预签名）。
//
//	@Summary	S3 预签名直传
//	@Tags		上传
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "presignRequest{filename,contentType,size}"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/presign [post]
func (h *UploadHandler) Presign(c *gin.Context) {
	var req struct {
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"contentType" binding:"required"`
		Size        int64  `json:"size" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, "filename、contentType、size 均为必填")
		return
	}
	res, err := h.svc.GeneratePresign(req.Filename, req.ContentType, req.Size)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// Confirm POST /upload/confirm（确认 S3 直传完成并建记录）。
//
//	@Summary	确认直传完成入库
//	@Tags		上传
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "confirmRequest{objectKey,...}"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/confirm [post]
func (h *UploadHandler) Confirm(c *gin.Context) {
	var req infra.S3Confirm
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, "key、url、originalName、mimeType、size 均为必填")
		return
	}
	if req.Scene != "" && req.Scene != infra.SceneAvatar && req.Scene != infra.SceneImage && req.Scene != infra.SceneOther {
		response.FailWith(c, errcode.ErrBadRequest, "scene 必须是 avatar、image 或 other")
		return
	}
	up, err := h.svc.ConfirmS3Upload(req, infraCurrentUserID(c))
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, infraFormatUpload(*up))
}

// MigrationStats GET /upload/migration/stats（upload:manage）。
//
//	@Summary	迁移统计
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/migration/stats [get]
func (h *UploadHandler) MigrationStats(c *gin.Context) {
	stats, err := h.svc.MigrationStats(c.Request.Context())
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, stats)
}

// MigrationProcess POST /upload/migration/process（upload:manage）。
// 参数可来自 query（limit、dryRun）或 body（对齐原实现两处都读）。
//
//	@Summary	执行本地→S3 迁移
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/migration/process [post]
func (h *UploadHandler) MigrationProcess(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	dryRun := c.Query("dryRun") == "true"
	// body 覆盖 query（原 controller 只读 query，但 swagger 声明了 body，取并集更宽容）
	var body struct {
		Limit  *int  `json:"limit"`
		DryRun *bool `json:"dryRun"`
	}
	if err := c.ShouldBindJSON(&body); err == nil {
		if body.Limit != nil && *body.Limit > 0 {
			limit = *body.Limit
		}
		if body.DryRun != nil {
			dryRun = *body.DryRun
		}
	}
	res, err := h.svc.MigrationProcess(c.Request.Context(), limit, dryRun)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// MigrationMetadata POST /upload/migration/metadata（upload:manage）。
//
//	@Summary	回填元数据
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/migration/metadata [post]
func (h *UploadHandler) MigrationMetadata(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	res, err := h.svc.MigrationFillMetadata(c.Request.Context(), limit)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// Remove DELETE /upload/:id（引用计数 -1，归零删盘删记录）。
//
//	@Summary	删除上传记录
//	@Tags		上传
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "上传记录 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/upload/{id} [delete]
func (h *UploadHandler) Remove(c *gin.Context) {
	if err := h.svc.RemoveUpload(c.Param("id")); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// ---------- 工具 ----------

// infraFormatUpload 组装上传文件响应体（jsonb 列还原为结构化字段，对齐原实体序列化）。
func infraFormatUpload(up model.Upload) gin.H {
	out := gin.H{
		"id":                     up.ID,
		"hash":                   up.Hash,
		"originalName":           up.OriginalName,
		"storage":                up.Storage,
		"filename":               up.Filename,
		"path":                   up.Path,
		"url":                    up.URL,
		"size":                   up.Size,
		"mimeType":               up.MimeType,
		"referenceCount":         up.ReferenceCount,
		"processed":              up.Processed,
		"watermarked":            up.Watermarked,
		"videoCompressionStatus": up.VideoCompressionStatus,
		"auditStatus":            up.AuditStatus,
		"scene":                  up.Scene,
		"userId":                 up.UserID,
		"createdAt":              up.CreatedAt,
		"updatedAt":              up.UpdatedAt,
	}
	out["original"] = infraJSONOrNil(up.OriginalJSON)
	out["thumbnails"] = infraJSONOrNil(up.ThumbsJSON)
	out["videoCompressionJob"] = infraJSONOrNil(up.VideoJobJSON)
	out["auditResult"] = infraJSONOrNil(up.AuditResultJS)
	return out
}

// infraJSONOrNil 把 jsonb 文本解析成对象；空/非法返回 nil（对齐 nullable json 列序列化为 null）。
func infraJSONOrNil(raw string) interface{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil
	}
	return v
}

// infraParseUploadMetadata 解析 metadata JSON（兼容单个对象与数组）。
func infraParseUploadMetadata(raw string) ([]infra.UploadMetaItem, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list []infra.UploadMetaItem
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list, nil
	}
	var single infra.UploadMetaItem
	if err := json.Unmarshal([]byte(raw), &single); err != nil {
		return nil, err
	}
	return []infra.UploadMetaItem{single}, nil
}

// infraFirstValue 取 multipart 值列表首项。
func infraFirstValue(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

// infraRequestBaseURL 推导请求根 URL（本地存储无 CDN 时拼绝对地址，对齐 getRequestBaseUrl）。
func infraRequestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// infraErrOrNil 把 service 层返回的 error 归一成 *errcode.Err（response.Fail 的入参类型）。
//
// service 层统一以 error 接口返回（便于 errors.Is 判断与包装），
// handler 层在这里做一次收敛：已是 *errcode.Err 直接透传，其它按 500 处理。
func infraErrOrNil(err error) *errcode.Err {
	if err == nil {
		return nil
	}
	if e, ok := err.(*errcode.Err); ok {
		return e
	}
	return errcode.ErrInternal.With(err.Error())
}

// infraHasPermission 判断当前 claims 是否含某权限点（OR 语义单点查）。
func infraHasPermission(c *gin.Context, perm string) bool {
	raw, exists := c.Get(middleware.CtxUserKey)
	if !exists {
		return false
	}
	claims, ok := raw.(*jwtx.Claims)
	if !ok || claims == nil {
		return false
	}
	for _, p := range claims.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}
