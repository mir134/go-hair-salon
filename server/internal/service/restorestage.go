package service

// 恢复的暂存与校验阶段（todo 52 步骤 4；08-DEPLOYMENT.md:78）。
//
// 本文件只做「只读准备」：打开目标 zip、校验条目布局、解压到 staging、
// 校验数据库快照。全部成功之前不触碰任何现有数据；失败返回 422（备份不合法）。
// 破坏性替换见 restoreswap.go。

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mir134/go-hair-salon/server/internal/repository"
)

const (
	// restoreStagingPrefix 是恢复暂存目录前缀（与 DB 同目录，保证替换用 rename 同卷原子）。
	restoreStagingPrefix = ".restore-staging-"
)

// restoreKeyTables 是恢复后必须可查询的关键表（数据检查，plan todo 52 步骤 5）。
var restoreKeyTables = []string{"users", "customers", "orders", "settings", "operation_logs"}

// restoreStaging 是一次恢复的暂存区：所有解压与校验都在这里完成，随后整体替换。
type restoreStaging struct {
	dir        string
	dbPath     string
	uploadsDir string
	configPath string
	hasUploads bool
	hasConfig  bool
	fileCount  int
}

// cleanup 删除暂存目录（成功/失败路径都会调用；已改名进正式位置的条目不受影响）。
func (s *restoreStaging) cleanup() {
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

// stageRestore 打开目标 zip、校验条目布局、解压到暂存目录并校验数据库快照。
//
// 任一步失败返回 422（备份文件本身不合法），调用方保证现有数据未被触碰。
func (s *RestoreService) stageRestore(archivePath string) (*restoreStaging, error) {
	stagingDir, err := os.MkdirTemp(filepath.Dir(s.backups.dbPath), restoreStagingPrefix)
	if err != nil {
		return nil, Internal("创建恢复暂存目录失败")
	}
	staging := &restoreStaging{
		dir:        stagingDir,
		dbPath:     filepath.Join(stagingDir, "snapshot.db"),
		uploadsDir: filepath.Join(stagingDir, "uploads"),
		configPath: filepath.Join(stagingDir, backupEntryConfig),
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		staging.cleanup()
		return nil, Validation("备份文件不是有效的 ZIP 压缩包")
	}
	defer func() { _ = reader.Close() }()

	dbEntry, err := collectRestoreEntries(reader, staging)
	if err != nil {
		staging.cleanup()
		return nil, err
	}

	// 解压：数据库快照 + uploads（相对路径原样保留）+ config.yaml。
	if err := extractZipEntry(dbEntry, staging.dbPath); err != nil {
		staging.cleanup()
		return nil, Internal("解压数据库快照失败")
	}
	staging.fileCount++
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		switch {
		case strings.HasPrefix(f.Name, backupEntryUploadsPrefix):
			rel := strings.TrimPrefix(f.Name, backupEntryUploadsPrefix)
			if err := extractZipEntry(f, filepath.Join(staging.uploadsDir, filepath.FromSlash(rel))); err != nil {
				staging.cleanup()
				return nil, Internal("解压 uploads 文件失败")
			}
			staging.fileCount++
		case f.Name == backupEntryConfig:
			if err := extractZipEntry(f, staging.configPath); err != nil {
				staging.cleanup()
				return nil, Internal("解压配置文件失败")
			}
			staging.fileCount++
		}
	}

	// 快照必须是完整可读的 SQLite 数据库（integrity_check=ok 且关键表可查）。
	if err := verifyRestoreSnapshot(staging.dbPath); err != nil {
		staging.cleanup()
		return nil, Validation("备份数据库校验失败：" + err.Error())
	}
	return staging, nil
}

// collectRestoreEntries 校验 zip 条目布局并返回唯一的数据库快照条目。
//
// 布局约定见 backuparchive.go；uploads 相对路径必须是本地安全路径（防 zip slip）。
func collectRestoreEntries(reader *zip.ReadCloser, staging *restoreStaging) (*zip.File, error) {
	var dbEntry *zip.File
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		switch {
		case strings.HasPrefix(f.Name, backupEntryDBPrefix):
			rel := strings.TrimPrefix(f.Name, backupEntryDBPrefix)
			if dbEntry != nil || !isLocalRelPath(rel) {
				return nil, Validation("备份包含非法或重复的数据库快照条目")
			}
			dbEntry = f
		case strings.HasPrefix(f.Name, backupEntryUploadsPrefix):
			if !isLocalRelPath(strings.TrimPrefix(f.Name, backupEntryUploadsPrefix)) {
				return nil, Validation("备份包含非法路径条目：" + f.Name)
			}
			staging.hasUploads = true
		case f.Name == backupEntryConfig:
			staging.hasConfig = true
		}
	}
	if dbEntry == nil {
		return nil, Validation("备份缺少数据库快照（db/）")
	}
	return dbEntry, nil
}

// isLocalRelPath 判定 zip 内相对路径是否为安全的本地路径（拒绝绝对路径、..、盘符）。
func isLocalRelPath(rel string) bool {
	if rel == "" || strings.HasSuffix(rel, "/") {
		return false
	}
	return filepath.IsLocal(filepath.FromSlash(rel))
}

// extractZipEntry 把一个 zip 条目流式解压到目标路径。
func extractZipEntry(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// verifyRestoreSnapshot 只读打开快照：PRAGMA integrity_check=ok 且关键表可查询。
func verifyRestoreSnapshot(path string) error {
	sqlDB, err := repository.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	var integrity string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("读取完整性校验结果失败: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("完整性校验未通过: %s", integrity)
	}
	for _, table := range restoreKeyTables {
		var count int64
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			return fmt.Errorf("关键表 %s 不可查询: %w", table, err)
		}
	}
	return nil
}
