package controller

// BackupController 提供备份列表、手动备份与恢复接口（04-API.md:245-256、plan todo 50/52）。
//
// 权限 admin（路由 RBAC 中间件，06 §7）。
// 备份产物、保留策略与 operation_logs(action=backup) 由 service.BackupService 负责；
// 恢复流程（维护模式、安全备份、文件替换、数据检查）由 service.RestoreService 负责；
// controller 只做 DTO 映射与二次确认校验，不触碰文件系统。

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mir134/go-hair-salon/server/internal/service"
)

// BackupController 是备份接口控制器。
type BackupController struct {
	backups  *service.BackupService
	restores *service.RestoreService
}

// NewBackupController 构造备份控制器（restores 为 nil 时不注册恢复路由）。
func NewBackupController(backups *service.BackupService, restores *service.RestoreService) *BackupController {
	return &BackupController{backups: backups, restores: restores}
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

// RestoreRequest 是 POST /backups/:id/restore 的请求体（二次确认，05-TASKS.md:166）。
//
// confirm 必须显式为 true：缺失/false → 400，防止误触与脚本误调用。
type RestoreRequest struct {
	Confirm bool `json:"confirm"`
}

// RestoreView 是恢复结果 DTO。
type RestoreView struct {
	// Restored 是被恢复的备份文件名。
	Restored string `json:"restored"`
	// SafetyBackup 是恢复前自动生成的安全备份文件名。
	SafetyBackup string `json:"safety_backup"`
	// FileCount 是从备份恢复的文件条目数（数据库快照 + uploads + config）。
	FileCount int `json:"file_count"`
	// UploadsReplaced 表示 uploads 目录已被备份内容替换。
	UploadsReplaced bool `json:"uploads_replaced"`
	// ConfigReplaced 表示 config.yaml 已被备份内容替换。
	ConfigReplaced bool `json:"config_replaced"`
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

// Restore 处理 POST /api/v1/backups/:id/restore（admin）：
// 二次确认 → 维护模式 → 安全备份 → 校验 → 替换 → 数据检查 → 清除维护 → 审计（todo 52）。
//
// :id 是备份文件名（backup-YYYYMMDD-HHMMSS.zip），与 GET /backups 的 name 字段一致。
func (h *BackupController) Restore(c *gin.Context) {
	if h.restores == nil {
		Fail(c, service.Internal("恢复功能未启用"))
		return
	}
	var req RestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, service.BadRequest("请求体必须是 JSON 且包含 confirm 字段"))
		return
	}
	if !req.Confirm {
		Fail(c, service.BadRequest("恢复操作需要二次确认：请提交 confirm=true"))
		return
	}
	result, err := h.restores.Restore(c.Request.Context(), c.Param("id"))
	if err != nil {
		Fail(c, err)
		return
	}
	Success(c, RestoreView{
		Restored:        result.Restored,
		SafetyBackup:    result.SafetyBackup,
		FileCount:       result.FileCount,
		UploadsReplaced: result.UploadsReplaced,
		ConfigReplaced:  result.ConfigReplaced,
	})
}

// newBackupView 把备份信息转为 DTO。
func newBackupView(info service.BackupInfo) BackupView {
	return BackupView{Name: info.Name, SizeBytes: info.SizeBytes, CreatedAt: info.CreatedAt}
}
