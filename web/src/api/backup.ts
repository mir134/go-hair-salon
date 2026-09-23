import { get, post } from './http'

/**
 * 备份 / 恢复 API（04-API.md:245-256、plan todo 50/52）。
 *
 * 仅 admin 可访问（06 §7）：前端隐藏入口只是导航简化，后端 RBAC 是最终边界。
 * 恢复的 :id 是备份文件名（backup-YYYYMMDD-HHMMSS.zip），与列表的 name 字段一致。
 * 恢复是破坏性操作：必须显式提交 confirm=true（二次确认），恢复期间业务写请求
 * 返回 503 + code=50300（「系统维护中」）。
 */

/** 备份信息 DTO（server/internal/controller/backup.go BackupView） */
export interface Backup {
  /** 文件名 backup-YYYYMMDD-HHMMSS.zip（恢复接口的 :id） */
  name: string
  size_bytes: number
  created_at: string
}

/** GET /backups 的 data：items 恒为数组（空结果 []，不是 null） */
export interface BackupListData {
  items: Backup[]
  total: number
}

/** POST /backups/:id/restore 请求体（二次确认） */
export interface RestorePayload {
  confirm: boolean
}

/** 恢复结果 DTO（server/internal/controller/backup.go RestoreView） */
export interface RestoreResult {
  /** 被恢复的备份文件名 */
  restored: string
  /** 恢复前自动生成的安全备份文件名 */
  safety_backup: string
  /** 恢复的文件条目数（数据库快照 + uploads + config） */
  file_count: number
  uploads_replaced: boolean
  config_replaced: boolean
}

/** GET /backups（admin）：按创建时间倒序返回全部备份 */
export function listBackups(): Promise<BackupListData> {
  return get<BackupListData>('/backups')
}

/** POST /backups（admin）：立即创建一份手动备份（后端自动保留最近 7 份） */
export function createBackup(): Promise<Backup> {
  return post<Backup>('/backups')
}

/**
 * POST /backups/:id/restore（admin）：恢复指定备份。
 *
 * 后端流程：维护模式（业务写请求 503）→ 恢复前安全备份 → 校验 zip 与数据库完整性
 * → 替换 DB/uploads/config → 数据检查 → 清除维护模式。备份损坏时返回 422 且原数据不受影响。
 */
export function restoreBackup(name: string, payload: RestorePayload): Promise<RestoreResult> {
  return post<RestoreResult>(`/backups/${encodeURIComponent(name)}/restore`, payload)
}
