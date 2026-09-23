package controller

// BackupController 提供备份列表与手动备份接口（04-API.md:245-256、plan todo 50）。
//
// 权限 admin（路由 RBAC 中间件，06 §7）。
// 备份产物、保留策略与 operation_logs(action=backup) 由 service.BackupService 负责，
// controller 只做 DTO 映射，不触碰文件系统；恢复接口属于 todo 52。

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// BackupController 是备份接口控制器。
type BackupController struct {
	backups *service.BackupService
}

// NewBackupController 构造备份控制器。
func NewBackupController(backups *service.BackupService) *BackupController {
	return &BackupController{backups: backups}
}

// BackupView 是备份信息 DTO：文件名 / 大小 / 创建时间（08-DEPLOYMENT.md:74-76）。
type BackupView struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupListData 是 GET /backups 的 data：items 恒为数组（空结果 []，不是 null）。
type BackupListData struct {
	Items []BackupView `json:"items"`
	Total int          `json:"total"`
}

// List 处理 GET /api/v1/backups（admin）：按创建时间倒序返回现有备份。
func (h *BackupController) List(c *gin.Context) {
	infos, err := h.backups.List()
	if err != nil {
		Fail(c, err)
		return
	}
	items := make([]BackupView, 0, len(infos))
	for _, info := range infos {
		items = append(items, newBackupView(info))
	}
	Success(c, BackupListData{Items: items, Total: len(items)})
}

// Create 处理 POST /api/v1/backups（admin）：立即创建一份备份（201）。
//
// 触发来源固定为 manual；审计日志由 service 层在文件全部落盘后写入（不在事务内）。
func (h *BackupController) Create(c *gin.Context) {
	result, err := h.backups.Create(c.Request.Context(), service.BackupTriggerManual)
	if err != nil {
		Fail(c, err)
		return
	}
	Created(c, newBackupView(result.Info))
}

// newBackupView 把备份信息转为 DTO。
func newBackupView(info service.BackupInfo) BackupView {
	return BackupView{Name: info.Name, SizeBytes: info.SizeBytes, CreatedAt: info.CreatedAt}
}
