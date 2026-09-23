package service_test

// 本文件是 todo 50 的边界与对抗测试（与 backup_test.go 同一个 TestBackup 验收命令覆盖）：
//   - 保留策略：连续造 8 份备份后只剩最近 7 份，最旧一份被物理删除（05-TASKS.md:164）；
//   - 权限：GET/POST /backups 仅 admin（04-API.md:245-256、06 §7）；staff → 403、未登录 → 401，
//     且 admin 创建后 operation_logs(action=backup) 记录真实操作人；
//   - malformed_input：备份目录不可用 / uploads 目录缺失 / config.yaml 缺失 →
//     明确错误且不 panic、不留半成品文件；
//   - stale_state：写入进行中取快照仍是完整有效的数据库，且后续写入不会破坏已生成的旧快照；
//   - misleading_success_output：所有断言以 zip/DB 真实内容为准，不以响应码或日志为准。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

func TestBackupRetention(t *testing.T) {
	env := newBackupTestEnv(t)
	names := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		result := env.createBackup(t, service.BackupTriggerManual)
		names = append(names, result.Info.Name)
	}

	// Then 1: 磁盘上只剩 7 份，最旧一份被删除。
	if got := countBackupZips(t, env.backupDir); got != 7 {
		t.Fatalf("磁盘备份数 = %d, want 7", got)
	}
	if _, err := os.Stat(filepath.Join(env.backupDir, names[0])); !os.IsNotExist(err) {
		t.Errorf("最旧备份 %s 仍存在（err=%v）", names[0], err)
	}
	if _, err := os.Stat(filepath.Join(env.backupDir, names[7])); err != nil {
		t.Errorf("最新备份 %s 不存在: %v", names[7], err)
	}

	// Then 2: List 与磁盘一致（最新在前），且最新一份是有效 zip。
	infos, err := env.svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 7 {
		t.Fatalf("List 条数 = %d, want 7", len(infos))
	}
	if infos[0].Name != names[7] {
		t.Errorf("List 首条 = %s, want 最新 %s", infos[0].Name, names[7])
	}
	if infos[len(infos)-1].Name != names[1] {
		t.Errorf("List 末条 = %s, want 最旧保留 %s", infos[len(infos)-1].Name, names[1])
	}
	readZip(t, filepath.Join(env.backupDir, infos[0].Name))
}

func TestBackupRBAC(t *testing.T) {
	env := newBackupTestEnv(t)

	// Then 1: 未登录 → 401；staff → 403（后端权限边界，非前端隐藏）。
	if w := env.do(http.MethodGet, "/api/v1/backups", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("未登录 GET /backups status = %d, want 401 (body=%s)", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPost, ""},
	} {
		w := env.do(tc.method, "/api/v1/backups", tc.body, env.staffToken)
		if w.Code != http.StatusForbidden {
			t.Errorf("staff %s /backups status = %d, want 403 (body=%s)", tc.method, w.Code, w.Body.String())
		}
	}
	if got := countBackupZips(t, env.backupDir); got != 0 {
		t.Fatalf("staff 被拒后备份数 = %d, want 0", got)
	}

	// Then 2: admin GET → 200 空列表；POST → 201 并落盘。
	w := env.do(http.MethodGet, "/api/v1/backups", "", env.adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("admin GET /backups status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var listEnv struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				Name      string    `json:"name"`
				SizeBytes int64     `json:"size_bytes"`
				CreatedAt time.Time `json:"created_at"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("解析列表响应失败: %v (body=%s)", err, w.Body.String())
	}
	if listEnv.Code != service.CodeOK || listEnv.Data.Total != 0 || len(listEnv.Data.Items) != 0 {
		t.Fatalf("空列表 = %+v, want code=0 total=0 items=[]", listEnv)
	}

	created := env.do(http.MethodPost, "/api/v1/backups", "", env.adminToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("admin POST /backups status = %d, want 201 (body=%s)", created.Code, created.Body.String())
	}
	var createEnv struct {
		Code int `json:"code"`
		Data struct {
			Name      string    `json:"name"`
			SizeBytes int64     `json:"size_bytes"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createEnv); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	if createEnv.Code != service.CodeOK || !backupNamePattern.MatchString(createEnv.Data.Name) {
		t.Fatalf("创建响应 data = %+v, want backup-*.zip", createEnv.Data)
	}
	if _, err := os.Stat(filepath.Join(env.backupDir, createEnv.Data.Name)); err != nil {
		t.Fatalf("创建响应成功但备份文件不存在: %v", err)
	}

	// Then 3: 再次 GET 有 1 条；operation_logs 记录 admin + ip/ua。
	w2 := env.do(http.MethodGet, "/api/v1/backups", "", env.adminToken)
	if err := json.Unmarshal(w2.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("解析列表响应失败: %v", err)
	}
	if listEnv.Data.Total != 1 || len(listEnv.Data.Items) != 1 || listEnv.Data.Items[0].Name != createEnv.Data.Name {
		t.Fatalf("列表 = %+v, want 仅 %s", listEnv.Data, createEnv.Data.Name)
	}
	var logRow model.OperationLog
	if err := env.db.Where("action = ?", "backup").First(&logRow).Error; err != nil {
		t.Fatalf("operation_logs 缺少 action=backup: %v", err)
	}
	if logRow.OperatorID == nil || *logRow.OperatorID != env.admin.ID {
		t.Errorf("backup 日志 operator_id = %v, want admin#%d", logRow.OperatorID, env.admin.ID)
	}
	if logRow.IP != "10.7.7.7" || logRow.UserAgent != "backup-probe/1.0" {
		t.Errorf("backup 日志 ip/ua = %q/%q, want 10.7.7.7/backup-probe/1.0", logRow.IP, logRow.UserAgent)
	}
}

func TestBackupMalformedInput(t *testing.T) {
	env := newBackupTestEnv(t)
	ctx := service.WithOperatorID(context.Background(), env.admin.ID)

	// Case 1: BACKUP_DIR 的父路径是普通文件（目录不可写）→ 明确错误、不留半成品。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("造 blocker 文件: %v", err)
	}
	unwritableDir := filepath.Join(blocker, "backups")
	unwritable := service.NewBackupService(service.BackupServiceDeps{
		DB: env.db, DBPath: env.dbPath, UploadDir: env.uploadDir, BackupDir: unwritableDir,
		ConfigPath: env.configPath,
		Logs:       service.NewOperationLogService(repository.NewOperationLogRepository(env.db)),
	})
	if _, err := unwritable.Create(ctx, service.BackupTriggerManual); err == nil {
		t.Fatal("备份目录不可用时应返回错误")
	} else if !strings.Contains(err.Error(), "备份目录") {
		t.Errorf("错误信息 = %q, 应说明备份目录问题", err.Error())
	}

	// Case 2: UPLOAD_DIR 缺失 → 明确错误（08-DEPLOYMENT.md:74 备份必须包含 uploads）。
	if err := os.RemoveAll(env.uploadDir); err != nil {
		t.Fatalf("删除 uploads 目录: %v", err)
	}
	if _, err := env.svc.Create(ctx, service.BackupTriggerManual); err == nil {
		t.Fatal("uploads 目录缺失时应返回错误")
	} else if !strings.Contains(err.Error(), "uploads") {
		t.Errorf("错误信息 = %q, 应说明 uploads 目录问题", err.Error())
	}

	// Case 3: config.yaml 缺失 → 明确错误（08-DEPLOYMENT.md:74 备份必须包含必要配置）。
	if err := os.MkdirAll(env.uploadDir, 0o755); err != nil {
		t.Fatalf("恢复 uploads 目录: %v", err)
	}
	if err := os.Remove(env.configPath); err != nil {
		t.Fatalf("删除 config.yaml: %v", err)
	}
	if _, err := env.svc.Create(ctx, service.BackupTriggerManual); err == nil {
		t.Fatal("config.yaml 缺失时应返回错误")
	} else if !strings.Contains(err.Error(), "config.yaml") {
		t.Errorf("错误信息 = %q, 应说明 config.yaml 问题", err.Error())
	}

	// Case 4: 全部失败路径都不留半成品（无备份 zip、无 staging 残留）。
	if got := countBackupZips(t, env.backupDir); got != 0 {
		t.Errorf("失败路径留下 %d 个备份 zip, want 0", got)
	}
	entries, err := os.ReadDir(env.backupDir)
	if err != nil {
		t.Fatalf("ReadDir(backupDir): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("失败路径留下残留文件: %v", entries)
	}
}

func TestBackupDuringWrites(t *testing.T) {
	env := newBackupTestEnv(t)
	baseline := env.seedCustomer(t, "写入基线客户")

	var (
		mu       sync.Mutex
		writeErr []error
		stop     = make(chan struct{})
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			customer := &model.Customer{Name: fmt.Sprintf("并发客户-%d", i)}
			if err := env.db.Create(customer).Error; err != nil {
				mu.Lock()
				writeErr = append(writeErr, err)
				mu.Unlock()
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// When: 并发写入进行中创建备份。
	result := env.createBackup(t, service.BackupTriggerManual)
	close(stop)
	wg.Wait()
	mu.Lock()
	errs := append([]error(nil), writeErr...)
	mu.Unlock()
	for _, err := range errs {
		t.Errorf("并发写入失败: %v", err)
	}

	// Then 1: 写入进行中的快照仍为完整有效的数据库，且包含基线数据。
	entries := readZip(t, filepath.Join(env.backupDir, result.Info.Name))
	snapshot, ok := entries["db/"+filepath.Base(env.dbPath)]
	if !ok {
		t.Fatalf("zip 缺少数据库快照（条目=%v）", zipKeys(entries))
	}
	sqlDB := openSnapshot(t, snapshot)
	var name string
	if err := sqlDB.QueryRow("SELECT name FROM customers WHERE id = ?", baseline.ID).Scan(&name); err != nil {
		t.Fatalf("快照查询基线客户失败: %v", err)
	}
	if name != baseline.Name {
		t.Errorf("快照基线客户 = %q, want %q", name, baseline.Name)
	}

	// Then 2: 后续写入继续发生，但旧快照依然可读（stale_state 不被破坏）。
	env.seedCustomer(t, "写入后续客户")
	again := readZip(t, filepath.Join(env.backupDir, result.Info.Name))
	openSnapshot(t, again["db/"+filepath.Base(env.dbPath)])

	// Then 3: 最新备份时间来自文件名（供 todo 51 启动补偿判定）。
	latest, ok := env.svc.LatestBackupTime()
	if !ok || latest.IsZero() {
		t.Fatalf("LatestBackupTime = %v/%v, want 有效时间", latest, ok)
	}
}
