package controller

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// UploadController 处理图片上传（D8 决议：POST /uploads）。
//
// 权限边界由路由中间件保证（both：admin 与 staff 均可上传头像——头像属于客户/员工资料编辑，
// 与 POST/PUT /customers 的 both 口径一致，非账务与管理操作）。
// 校验与落盘在 service 层；控制器只做请求解析、DTO 与审计日志（事务外写入）。
type UploadController struct {
	uploads *service.UploadService
	logs    *service.OperationLogService
}

// NewUploadController 构造上传控制器。
func NewUploadController(uploads *service.UploadService, logs *service.OperationLogService) *UploadController {
	return &UploadController{uploads: uploads, logs: logs}
}

// UploadView 是上传成功 DTO：url 可直接用于 <img src>（相对路径，静态托管于 /uploads/）。
type UploadView struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Create 处理 POST /api/v1/uploads（multipart/form-data，文件字段名固定为 file）。
//
// 错误口径（04-API.md:265-275）：
//   - 超过大小上限 / 缺字段 / 非 multipart → 400 + 40000（文档状态集合内；不引入 413 等文档外状态）；
//   - 未登录 → 401、无权限 → 403 由中间件先于本处理器返回。
func (h *UploadController) Create(c *gin.Context) {
	// 请求体上限：文件 2MB + multipart 开销；超限时 FormFile 返回 *http.MaxBytesError，
	// 在解析阶段即被拒绝，不会把超大请求写入磁盘。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxUploadRequestBytes)
	header, err := c.FormFile("file")
	if err != nil {
		Fail(c, uploadRequestError(err))
		return
	}
	file, err := header.Open()
	if err != nil {
		Fail(c, service.BadRequest("读取上传文件失败"))
		return
	}
	defer func() { _ = file.Close() }()

	result, err := h.uploads.Save(header.Filename, file)
	if err != nil {
		Fail(c, err)
		return
	}
	h.writeLog(c, result)
	Success(c, UploadView{URL: result.URL, Name: result.Name, Size: result.Size})
}

// uploadRequestError 把 multipart 解析错误映射为 400：超限与缺字段给出明确文案，绝不 500。
func uploadRequestError(err error) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return service.BadRequest("文件大小超过 2MB 限制")
	}
	return service.BadRequest("缺少文件字段 file（multipart/form-data）")
}

// writeLog 在上传落盘成功后写审计日志（业务写操作之外独立写入，见 service.WriteLog）。
// content 只记录服务端存储名与大小，不记录客户端原始文件名（避免不可信输入进入日志）。
func (h *UploadController) writeLog(c *gin.Context, result *service.UploadResult) {
	content := fmt.Sprintf("上传图片 %s（%d 字节）", result.Name, result.Size)
	if err := h.logs.WriteLog(c.Request.Context(), "upload", "upload", 0, content); err != nil {
		slog.Default().Error("写入上传审计日志失败", "err", err)
	}
}
