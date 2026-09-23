package service

// 备份服务（todo 50；08-DEPLOYMENT.md:70-76、05-TASKS.md:157-167）。
//
// 约定：
//   - 备份内容 = 数据库快照 + UPLOAD_DIR 全量文件 + config.yaml；
//   - 数据库快照必须用 `VACUUM INTO` 生成：WAL 模式下直接拷贝 .db 文件会得到撕裂副本
//     （已提交数据可能还在 -wal 中），VACUUM INTO 由 SQLite 保证副本一致可读；
//   - 打包为 BACKUP_DIR/backup-YYYYMMDD-HHMMSS.zip（同秒冲突追加 -N），保留最近 7 份；
//   - 备份成功后在事务之外写 operation_logs(action=backup)。
//
// 本文件负责编排与保留策略；zip 打包与快照校验见 backuparchive.go。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	// backupRetain 是保留的最近备份份数（08-DEPLOYMENT.md:72「保留最近 7 份」）。
	backupRetain = 7
	// backupNamePrefix/backupTimeLayout/backupNameExt 约定 backup-20060102-150405.zip。
	backupNamePrefix = "backup-"
	backupTimeLayout = "20060102-150405"
	backupNameExt    = ".zip"
	// backupStagingPrefix 是打包用临时目录前缀（位于 BACKUP_DIR 内，保证同卷 rename）。
	backupStagingPrefix = ".staging-"
)

// backupNamePattern 匹配 backup-YYYYMMDD-HHMMSS.zip，组 1=时间戳、组 2=同秒序号。
var backupNamePattern = regexp.MustCompile(`^backup-(\d{8}-\d{6})(?:-(\d+))?\.zip$`)

// BackupTrigger 标识备份触发来源（写入 operation_logs content，便于审计区分手动/自动）。
type BackupTrigger string

const (
	// BackupTriggerManual 手动备份（POST /backups）。
	BackupTriggerManual BackupTrigger = "manual"
	// BackupTriggerAutomatic 定时或启动补偿备份（todo 51 定时器）。
	BackupTriggerAutomatic BackupTrigger = "automatic"
)

// label 返回审计描述用的中文标签。
func (t BackupTrigger) label() string {
	switch t {
	case BackupTriggerAutomatic:
		return "自动备份"
	case BackupTriggerManual:
		return "手动备份"
	default:
		return "备份"
	}
}

// BackupInfo 是一份备份文件的信息（GET /backups 列表项）。
type BackupInfo struct {
	// Name 是文件名 backup-YYYYMMDD-HHMMSS.zip。
	Name string
	// SizeBytes 是备份文件大小（字节）。
	SizeBytes int64
	// CreatedAt 是从文件名解析出的创建时间（本地时区；解析失败回退文件修改时间）。
	CreatedAt time.Time
	// Path 是备份文件完整路径（保留清理与后续恢复使用）。
	Path string
	// seq 是同秒冲突序号（命名排序用，0 表示无后缀）。
	seq int
}

// BackupResult 是一次成功备份的结果。
type BackupResult struct {
	// Info 是新生成的备份文件信息。
	Info BackupInfo
	// FileCount 是 zip 内文件条目数（数据库快照 + uploads 文件 + config.yaml）。
	FileCount int
	// Pruned 是本次被保留策略删除的旧备份文件名（最旧优先）。
	Pruned []string
}

// BackupServiceDeps 是备份服务依赖（字段较多，避免长参数列表）。
type BackupServiceDeps struct {
	// DB 是进程内唯一 gorm 连接池（VACUUM INTO 快照来源）。
	DB *gorm.DB
	// DBPath 是 SQLite 数据库文件路径（config.DB_PATH）。
	DBPath string
	// UploadDir 是上传文件目录（config.UPLOAD_DIR）。
	UploadDir string
	// BackupDir 是备份产物目录（config.BACKUP_DIR）。
	BackupDir string
	// ConfigPath 是打包进备份的配置文件路径（通常为 config.yaml）。
	ConfigPath string
	// Logs 是审计日志服务（备份成功后写 action=backup）。
	Logs *OperationLogService
	// Now 是可注入时钟；nil 时使用 time.Now（todo 51 假时钟测试共用）。
	Now func() time.Time
	// Logger 是 slog 日志器；nil 时使用默认 logger。
	Logger *slog.Logger
}

// BackupService 生成 WAL 安全的 ZIP 备份并维护保留策略。
//
// 并发：手动备份（API）与自动备份（todo 51 定时器）共享同一实例，
// mu 保证 VACUUM INTO、打包与保留清理不会并发交错。
type BackupService struct {
	db         *gorm.DB
	dbPath     string
	uploadDir  string
	backupDir  string
	configPath string
	logs       *OperationLogService
	now        func() time.Time
	logger     *slog.Logger
	mu         sync.Mutex
}

// NewBackupService 构造备份服务。
func NewBackupService(deps BackupServiceDeps) *BackupService {
	return &BackupService{
		db:         deps.DB,
		dbPath:     deps.DBPath,
		uploadDir:  deps.UploadDir,
		backupDir:  deps.BackupDir,
		configPath: deps.ConfigPath,
		logs:       deps.Logs,
		now:        orDefaultNow(deps.Now),
		logger:     orDefaultLogger(deps.Logger),
	}
}

// orDefaultNow 返回注入的时钟；未注入时使用 time.Now（todo 51 假时钟测试共用）。
func orDefaultNow(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return time.Now
}

// orDefaultLogger 返回注入的日志器；未注入时使用默认 logger。
func orDefaultLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// Create 生成一份备份并执行保留策略；成功后写 operation_logs(action=backup)。
//
// 任意一步失败都返回明确错误且不产生半成品（staging 目录在 defer 中整体清理）；
// 审计日志在快照/打包/清理全部结束之后写入，不在任何业务事务内（避免占住唯一连接）。
func (s *BackupService) Create(ctx context.Context, trigger BackupTrigger) (*BackupResult, error) {
	if s.backupDir == "" {
		return nil, errors.New("备份目录未配置（BACKUP_DIR）")
	}
	if s.dbPath == "" {
		return nil, errors.New("数据库路径未配置（DB_PATH）")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建备份目录 %s 失败: %w", s.backupDir, err)
	}
	staging, err := os.MkdirTemp(s.backupDir, backupStagingPrefix)
	if err != nil {
		return nil, fmt.Errorf("创建备份目录 %s 临时目录失败: %w", s.backupDir, err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	snapshotPath := filepath.Join(staging, "snapshot.db")
	if err := s.snapshotInto(snapshotPath); err != nil {
		return nil, err
	}
	if err := verifySQLiteSnapshot(snapshotPath); err != nil {
		return nil, fmt.Errorf("备份快照校验失败: %w", err)
	}

	archivePath := filepath.Join(staging, "backup.zip")
	fileCount, err := s.writeArchive(snapshotPath, archivePath)
	if err != nil {
		return nil, err
	}

	name, err := s.uniqueName()
	if err != nil {
		return nil, err
	}
	finalPath := filepath.Join(s.backupDir, name)
	if err := os.Rename(archivePath, finalPath); err != nil {
		return nil, fmt.Errorf("备份文件落盘 %s 失败: %w", finalPath, err)
	}
	info, err := s.describe(finalPath)
	if err != nil {
		return nil, err
	}
	result := &BackupResult{Info: info, FileCount: fileCount, Pruned: s.prune()}
	s.writeBackupLog(ctx, trigger, info, fileCount)
	s.logger.Info("备份完成", "name", info.Name, "size_bytes", info.SizeBytes,
		"file_count", fileCount, "trigger", string(trigger))
	return result, nil
}

// List 返回 BACKUP_DIR 下全部备份（最新在前）。
//
// 只识别 backup-YYYYMMDD-HHMMSS.zip 命名的文件；目录不存在或无备份时返回空切片（不是 nil）。
func (s *BackupService) List() ([]BackupInfo, error) {
	if s.backupDir == "" {
		return []BackupInfo{}, nil
	}
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("读取备份目录 %s 失败: %w", s.backupDir, err)
	}
	infos := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !backupNamePattern.MatchString(entry.Name()) {
			continue
		}
		info, err := s.describe(filepath.Join(s.backupDir, entry.Name()))
		if err != nil {
			// 单个文件读取失败（被并发删除等）不阻断列表。
			s.logger.Warn("跳过无法读取的备份文件", "file", entry.Name(), "err", err)
			continue
		}
		infos = append(infos, info)
	}
	sortBackups(infos)
	return infos, nil
}

// LatestBackupTime 返回最新一份备份的创建时间；没有任何备份时 ok=false（todo 51 启动补偿判定）。
func (s *BackupService) LatestBackupTime() (time.Time, bool) {
	infos, err := s.List()
	if err != nil || len(infos) == 0 {
		return time.Time{}, false
	}
	return infos[0].CreatedAt, true
}

// prune 删除超出保留份数的旧备份，返回被删除文件名（最旧优先）。
//
// 删除失败只记日志、不使本次备份失败：新备份已经落盘，清理失败是可降级的运维问题。
// staging 临时目录与 staging/snapshot.db 不匹配备份命名，因此不会被保留策略误删。
func (s *BackupService) prune() []string {
	infos, err := s.List()
	if err != nil {
		s.logger.Error("读取备份目录失败，跳过保留清理", "err", err)
		return nil
	}
	var pruned []string
	for _, info := range infos[min(len(infos), backupRetain):] {
		if err := os.Remove(info.Path); err != nil {
			s.logger.Error("删除旧备份失败", "file", info.Name, "err", err)
			continue
		}
		pruned = append(pruned, info.Name)
	}
	return pruned
}

// uniqueName 生成不冲突的备份文件名（同一秒内重复备份追加 -1、-2…）。
func (s *BackupService) uniqueName() (string, error) {
	base := backupNamePrefix + s.now().Format(backupTimeLayout)
	name := base + backupNameExt
	for seq := 1; ; seq++ {
		path := filepath.Join(s.backupDir, name)
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("检查备份文件名 %s 失败: %w", path, err)
		}
		name = fmt.Sprintf("%s-%d%s", base, seq, backupNameExt)
	}
}

// describe 读取备份文件信息（大小/时间/同秒序号）。
func (s *BackupService) describe(path string) (BackupInfo, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("读取备份文件 %s 失败: %w", path, err)
	}
	createdAt, seq := parseBackupName(fileInfo.Name())
	if createdAt.IsZero() {
		createdAt = fileInfo.ModTime()
	}
	return BackupInfo{
		Name:      fileInfo.Name(),
		SizeBytes: fileInfo.Size(),
		CreatedAt: createdAt,
		Path:      path,
		seq:       seq,
	}, nil
}

// snapshotInto 用 `VACUUM INTO` 生成数据库快照（WAL 模式下唯一安全的在线备份方式）。
//
// 目标文件必须不存在（VACUUM INTO 语义），调用方传入 staging 内的新路径。
func (s *BackupService) snapshotInto(path string) error {
	if s.db == nil {
		return errors.New("数据库连接未配置")
	}
	if err := s.db.Exec("VACUUM INTO ?", filepath.ToSlash(path)).Error; err != nil {
		return fmt.Errorf("创建数据库快照（VACUUM INTO %s）失败: %w", path, err)
	}
	return nil
}

// writeBackupLog 在事务之外写审计日志（action=backup，target_type=backup）。
//
// 日志写失败只记错误：备份文件已生成，审计失败不应回滚/伪装备份失败。
func (s *BackupService) writeBackupLog(ctx context.Context, trigger BackupTrigger, info BackupInfo, fileCount int) {
	if s.logs == nil {
		return
	}
	content := fmt.Sprintf("%s %s（%d 个文件，%d 字节）", trigger.label(), info.Name, fileCount, info.SizeBytes)
	if err := s.logs.WriteLog(ctx, "backup", "backup", 0, content); err != nil {
		s.logger.Error("写入备份审计日志失败", "backup", info.Name, "err", err)
	}
}

// parseBackupName 从文件名解析创建时间（本地时区）与同秒序号；格式不匹配返回零值。
func parseBackupName(name string) (time.Time, int) {
	matches := backupNamePattern.FindStringSubmatch(name)
	if matches == nil {
		return time.Time{}, 0
	}
	createdAt, err := time.ParseInLocation(backupTimeLayout, matches[1], time.Local)
	if err != nil {
		return time.Time{}, 0
	}
	seq := 0
	if matches[2] != "" {
		if parsed, err := strconv.Atoi(matches[2]); err == nil {
			seq = parsed
		}
	}
	return createdAt, seq
}

// sortBackups 按（时间，同秒序号）倒序排列：最新在前。
func sortBackups(infos []BackupInfo) {
	sort.Slice(infos, func(i, j int) bool {
		if !infos[i].CreatedAt.Equal(infos[j].CreatedAt) {
			return infos[i].CreatedAt.After(infos[j].CreatedAt)
		}
		return infos[i].seq > infos[j].seq
	})
}
