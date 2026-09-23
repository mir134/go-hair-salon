package service_test

// TestBackup 是 todo 50 的验收测试
// （计划：`go test ./internal/service -run TestBackup -v -count=1`）：
//   - 备份=WAL 安全快照（VACUUM INTO）+ uploads + config.yaml 打成带时间戳 ZIP 存 BACKUP_DIR
//     （08-DEPLOYMENT.md:70-76、02-AGENTS.md:45-63）；
//   - zip 内含数据库快照、uploads 文件与 config.yaml；快照用 database/sql 只读打开后
//     PRAGMA integrity_check=ok 且能查到真实业务行（misleading_success_output 防线：
//     不以成功日志为准，必须打开产物核对）；
//   - 手动备份写 operation_logs(action=backup)，operator 与执行者一致（05-TASKS.md:157-164）；
//   - 保留最近 7 份、staff → 403、备份目录不可用返回明确错误等边界见 backup_guard_test.go。
//
// 测试使用 t.TempDir() 内的真实 SQLite（WAL）与真实文件系统，不 mock、不启动服务器。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestBackup(t *testing.T) {
	t.Run("创建备份：zip 含数据库快照、uploads 与 config，且快照可读", func(t *testing.T) {
		env := newBackupTestEnv(t)
		customer := env.seedCustomer(t, "备份客户")
		uploadBytes := []byte("avatar-bytes-2026")
		env.seedUpload(t, "avatars/customer-1.png", uploadBytes)
		env.seedUpload(t, "tickets/2026/09/note.txt", []byte("ticket-bytes"))

		// When: admin 上下文触发手动备份。
		result := env.createBackup(t, service.BackupTriggerManual)

		// Then 1: 文件名/大小/时间符合约定。
		if !backupNamePattern.MatchString(result.Info.Name) {
			t.Fatalf("备份名 %q 不符合 backup-YYYYMMDD-HHMMSS.zip", result.Info.Name)
		}
		info, err := os.Stat(filepath.Join(env.backupDir, result.Info.Name))
		if err != nil {
			t.Fatalf("备份文件不存在: %v", err)
		}
		if result.Info.SizeBytes != info.Size() || result.Info.SizeBytes <= 0 {
			t.Errorf("SizeBytes = %d, 磁盘大小 = %d", result.Info.SizeBytes, info.Size())
		}
		if result.Info.CreatedAt.IsZero() {
			t.Error("CreatedAt 为零值")
		}
		if len(result.Pruned) != 0 {
			t.Errorf("首份备份不应触发清理，Pruned=%v", result.Pruned)
		}

		// Then 2: zip 内容 = 数据库快照 + uploads + config.yaml。
		entries := readZip(t, filepath.Join(env.backupDir, result.Info.Name))
		dbEntry := "db/" + filepath.Base(env.dbPath)
		snapshot, ok := entries[dbEntry]
		if !ok {
			t.Fatalf("zip 缺少数据库快照条目 %q（实际条目=%v）", dbEntry, zipKeys(entries))
		}
		if got := entries["uploads/avatars/customer-1.png"]; !bytes.Equal(got, uploadBytes) {
			t.Errorf("uploads/avatars/customer-1.png = %q, want %q", got, uploadBytes)
		}
		if got := entries["uploads/tickets/2026/09/note.txt"]; string(got) != "ticket-bytes" {
			t.Errorf("uploads/tickets/2026/09/note.txt = %q, want ticket-bytes", got)
		}
		configBytes, err := os.ReadFile(env.configPath)
		if err != nil {
			t.Fatalf("读取 config.yaml: %v", err)
		}
		if got := entries["config.yaml"]; !bytes.Equal(got, configBytes) {
			t.Errorf("config.yaml 条目 = %q, want %q", got, configBytes)
		}
		if result.FileCount != len(entries) {
			t.Errorf("FileCount = %d, zip 条目数 = %d", result.FileCount, len(entries))
		}

		// Then 3: 快照是真实可读的 SQLite（不信任成功日志）。
		sqlDB := openSnapshot(t, snapshot)
		var name string
		if err := sqlDB.QueryRow("SELECT name FROM customers WHERE id = ?", customer.ID).Scan(&name); err != nil {
			t.Fatalf("快照查询客户失败: %v", err)
		}
		if name != customer.Name {
			t.Errorf("快照客户 = %q, want %q", name, customer.Name)
		}

		// Then 4: List 返回 1 份；operation_logs(action=backup) 记录执行人。
		infos, err := env.svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(infos) != 1 || infos[0].Name != result.Info.Name {
			t.Fatalf("List = %+v, want 仅 %s", infos, result.Info.Name)
		}
		var logRow model.OperationLog
		if err := env.db.Where("action = ?", "backup").First(&logRow).Error; err != nil {
			t.Fatalf("operation_logs 缺少 action=backup: %v", err)
		}
		if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
			t.Errorf("backup 日志 operator_id = %v, want admin#%d", logRow.OperatorID, env.admin.ID)
		}
		if logRow.TargetType != "backup" {
			t.Errorf("backup 日志 target_type = %q, want backup", logRow.TargetType)
		}
		if !strings.Contains(logRow.Content, result.Info.Name) {
			t.Errorf("backup 日志 content = %q, 未包含备份文件名", logRow.Content)
		}
	})
}
