package service

// 恢复的破坏性替换阶段（todo 52 步骤 5；08-DEPLOYMENT.md:78）。
//
// 顺序：关闭连接池 → 原库文件移开 → 放入恢复后的库 → 重开连接池并原地挂回
// 已有的 *gorm.DB → 数据检查（迁移/完整性/关键表）→ uploads/config 替换。
//
// 回滚：步骤 5 内部的任何失败都会把原库文件放回并重开连接，原数据保持完好；
// 只有 uploads/config 替换失败不回滚数据库（数据检查已通过，且恢复前安全备份
// 保留了替换前的一切）。暂存与校验见 restorestage.go。

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// restoreUploadsPrefix 是 uploads 替换用临时目录前缀（与 UPLOAD_DIR 同目录）。
const restoreUploadsPrefix = ".restore-uploads-"

// swap 执行破坏性阶段：关闭连接 → 替换 DB/uploads/config → 重开 → 数据检查。
//
// DB 文件替换失败时回滚原文件并重开连接（原数据完好）；
// uploads/config 替换失败不回滚数据库（数据检查已通过，安全备份含替换前的一切）。
func (s *RestoreService) swap(staging *restoreStaging) error {
	dbPath := s.backups.dbPath
	rollbackPath := filepath.Join(staging.dir, "db-original")

	// 1. 关闭当前连接池（Windows 下文件被打开时无法重命名/替换）。
	if err := s.closeLivePool(); err != nil {
		return s.abortSwap("", fmt.Errorf("关闭数据库连接失败: %w", err))
	}
	// 2. 原库文件先移开（保留回滚能力）；-wal/-shm 属于旧库，一并清除。
	if err := os.Rename(dbPath, rollbackPath); err != nil {
		return s.abortSwap("", fmt.Errorf("暂存原数据库文件失败: %w", err))
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	// 3. 放入恢复后的数据库文件（与 DB 同目录 → 同卷 rename）。
	if err := os.Rename(staging.dbPath, dbPath); err != nil {
		return s.abortSwap(rollbackPath, fmt.Errorf("替换数据库文件失败: %w", err))
	}
	// 4. 重新打开连接池并原地挂回已有的 *gorm.DB（仓储无需重新装配）。
	pool, err := repository.OpenPool(dbPath)
	if err != nil {
		return s.abortSwap(rollbackPath, err)
	}
	repository.ReplaceConnPool(s.backups.db, pool)
	// 5. 数据检查：迁移（向前兼容旧备份）+ 完整性 + 关键表可查。
	if err := s.dataCheck(); err != nil {
		return s.abortSwap(rollbackPath, err)
	}
	_ = os.Remove(rollbackPath)

	// 6. uploads / config：备份包含时才替换。
	if err := s.replaceUploads(staging); err != nil {
		return Internal("数据库已恢复，但 uploads 替换失败，请使用恢复前安全备份人工处理")
	}
	if err := s.replaceConfig(staging); err != nil {
		return Internal("数据库已恢复，但 config.yaml 替换失败，请使用恢复前安全备份人工处理")
	}
	return nil
}

// closeLivePool 关闭当前数据库连接池。
func (s *RestoreService) closeLivePool() error {
	sqlDB, err := s.backups.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// dataCheck 对恢复后的数据库执行迁移与可查询性检查（plan todo 52 步骤 5）。
func (s *RestoreService) dataCheck() error {
	if err := repository.Migrate(s.backups.db); err != nil {
		return fmt.Errorf("恢复后迁移检查失败: %w", err)
	}
	if err := repository.EnsureDefaultSettings(s.backups.db); err != nil {
		return fmt.Errorf("恢复后 settings 检查失败: %w", err)
	}
	var integrity string
	if err := s.backups.db.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil {
		return fmt.Errorf("恢复后完整性检查失败: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("恢复后完整性检查未通过: %s", integrity)
	}
	for _, table := range restoreKeyTables {
		var count int64
		if err := s.backups.db.Table(table).Count(&count).Error; err != nil {
			return fmt.Errorf("恢复后关键表 %s 不可查询: %w", table, err)
		}
	}
	return nil
}

// abortSwap 回滚破坏性阶段：恢复原数据库文件、重开连接并返回用户可读错误。
//
// rollbackPath 为空表示原文件尚未被移开（无需回滚文件），只需重开连接。
func (s *RestoreService) abortSwap(rollbackPath string, cause error) error {
	s.logger.Error("恢复失败，回滚到原数据", "err", cause)
	if sqlDB, err := s.backups.db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	if rollbackPath != "" {
		if _, statErr := os.Stat(rollbackPath); statErr == nil {
			_ = os.Remove(s.backups.dbPath)
			_ = os.Remove(s.backups.dbPath + "-wal")
			_ = os.Remove(s.backups.dbPath + "-shm")
			if err := os.Rename(rollbackPath, s.backups.dbPath); err != nil {
				s.logger.Error("回滚原数据库文件失败", "err", err)
			}
		}
	}
	pool, err := repository.OpenPool(s.backups.dbPath)
	if err != nil {
		s.logger.Error("回滚后重新打开数据库失败", "err", err)
		return Internal("恢复失败且原数据库无法重新打开，请使用恢复前安全备份人工恢复")
	}
	repository.ReplaceConnPool(s.backups.db, pool)
	return Internal("恢复失败，已回滚到恢复前的数据（原数据未受影响）")
}

// replaceUploads 用备份中的 uploads 替换现有目录（整体改名，失败可回滚）。
func (s *RestoreService) replaceUploads(staging *restoreStaging) error {
	if !staging.hasUploads {
		return nil
	}
	dst := s.backups.uploadDir
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	fresh, err := os.MkdirTemp(filepath.Dir(dst), restoreUploadsPrefix)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(fresh) }()
	if err := copyTree(staging.uploadsDir, fresh); err != nil {
		return fmt.Errorf("准备 uploads 替换目录失败: %w", err)
	}

	aside := ""
	if _, err := os.Stat(dst); err == nil {
		aside = fresh + "-old"
		if err := os.Rename(dst, aside); err != nil {
			return fmt.Errorf("暂存原 uploads 目录失败: %w", err)
		}
	}
	if err := os.Rename(fresh, dst); err != nil {
		if aside != "" {
			_ = os.Rename(aside, dst)
		}
		return fmt.Errorf("替换 uploads 目录失败: %w", err)
	}
	if aside != "" {
		_ = os.RemoveAll(aside)
	}
	return nil
}

// replaceConfig 用备份中的 config.yaml 原子替换现有配置（写入同目录临时文件后改名）。
func (s *RestoreService) replaceConfig(staging *restoreStaging) error {
	if !staging.hasConfig {
		return nil
	}
	data, err := os.ReadFile(staging.configPath)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.backups.configPath, data)
}

// copyTree 递归复制普通文件（跳过非普通文件，与备份打包一致）。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

// copyFile 复制单个普通文件。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeFileAtomic 原子写文件：同目录临时文件 + fsync + rename。
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
