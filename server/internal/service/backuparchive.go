package service

// zip 打包与快照校验（todo 50；08-DEPLOYMENT.md:70-76）。
//
// zip 内固定布局（恢复 todo 52 依赖）：
//
//	db/<db 文件名>   数据库快照（VACUUM INTO 产出）
//	uploads/...      UPLOAD_DIR 全量普通文件，相对路径原样保留
//	config.yaml      必要配置
//
// 打包过程不打开数据库连接，因而不与业务写入争抢唯一连接。

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// zip 内条目布局常量（恢复接口按此前缀查找，见 04-API.md:255）。
const (
	// backupEntryDBPrefix 是数据库快照条目前缀（后接数据库文件名）。
	backupEntryDBPrefix = "db/"
	// backupEntryUploadsPrefix 是 uploads 条目前缀。
	backupEntryUploadsPrefix = "uploads/"
	// backupEntryConfig 是配置文件条目名。
	backupEntryConfig = "config.yaml"
)

// archiveFile 描述一个待写入 zip 的条目（zip 内名称 + 源文件路径）。
type archiveFile struct {
	entry string
	src   string
}

// writeArchive 把数据库快照、uploads 与 config.yaml 打包为 zip，返回打包文件数。
//
// UPLOAD_DIR 缺失、config.yaml 缺失都按错误处理：08-DEPLOYMENT.md:74 规定备份内容
// 必须包含 uploads 与必要配置，宁可失败也不产出缺内容的“看似成功”的备份。
func (s *BackupService) writeArchive(snapshotPath, archivePath string) (int, error) {
	files, err := s.archiveFiles(snapshotPath)
	if err != nil {
		return 0, err
	}

	out, err := os.Create(archivePath)
	if err != nil {
		return 0, fmt.Errorf("创建备份压缩包 %s 失败: %w", archivePath, err)
	}
	defer func() { _ = out.Close() }()
	writer := zip.NewWriter(out)
	for _, file := range files {
		if err := appendZipFile(writer, file, s.now()); err != nil {
			return 0, err
		}
	}
	if err := writer.Close(); err != nil {
		return 0, fmt.Errorf("完成备份压缩包失败: %w", err)
	}
	if err := out.Sync(); err != nil {
		return 0, fmt.Errorf("刷新备份压缩包失败: %w", err)
	}
	return len(files), nil
}

// archiveFiles 收集打包清单：数据库快照 + uploads 全量文件 + config.yaml。
func (s *BackupService) archiveFiles(snapshotPath string) ([]archiveFile, error) {
	files := []archiveFile{
		{entry: backupEntryDBPrefix + filepath.Base(s.dbPath), src: snapshotPath},
	}
	uploads, err := s.uploadFiles()
	if err != nil {
		return nil, err
	}
	files = append(files, uploads...)

	if s.configPath == "" {
		return nil, errors.New("配置文件路径未配置（config.yaml）")
	}
	if fileInfo, err := os.Stat(s.configPath); err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败（备份必须包含必要配置）: %w", s.configPath, err)
	} else if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("配置文件 %s 不是普通文件", s.configPath)
	}
	return append(files, archiveFile{entry: backupEntryConfig, src: s.configPath}), nil
}

// uploadFiles 遍历 UPLOAD_DIR 收集全部普通文件（相对路径原样保留在 uploads/ 下）。
//
// 目录缺失按错误处理：备份必须包含 uploads（08-DEPLOYMENT.md:74）。
// 非普通文件（目录/符号链接等）跳过并记日志。
func (s *BackupService) uploadFiles() ([]archiveFile, error) {
	if s.uploadDir == "" {
		return nil, errors.New("uploads 目录未配置（UPLOAD_DIR）")
	}
	dirInfo, err := os.Stat(s.uploadDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("uploads 目录 %s 不存在（备份必须包含 uploads）", s.uploadDir)
		}
		return nil, fmt.Errorf("读取 uploads 目录 %s 失败: %w", s.uploadDir, err)
	}
	if !dirInfo.IsDir() {
		return nil, fmt.Errorf("uploads 路径 %s 不是目录", s.uploadDir)
	}

	var files []archiveFile
	walkErr := filepath.WalkDir(s.uploadDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("遍历 uploads 失败（%s）: %w", path, err)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			s.logger.Warn("跳过 uploads 中的非普通文件", "path", path)
			return nil
		}
		rel, err := filepath.Rel(s.uploadDir, path)
		if err != nil {
			return fmt.Errorf("计算 uploads 相对路径失败（%s）: %w", path, err)
		}
		files = append(files, archiveFile{entry: backupEntryUploadsPrefix + filepath.ToSlash(rel), src: path})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

// appendZipFile 把一个普通文件流式写入 zip 条目（避免大文件一次性读入内存）。
func appendZipFile(writer *zip.Writer, file archiveFile, modified time.Time) error {
	in, err := os.Open(file.src)
	if err != nil {
		return fmt.Errorf("打开待备份文件 %s 失败: %w", file.src, err)
	}
	defer func() { _ = in.Close() }()
	target, err := writer.CreateHeader(&zip.FileHeader{
		Name:     file.entry,
		Method:   zip.Deflate,
		Modified: modified,
	})
	if err != nil {
		return fmt.Errorf("创建 zip 条目 %s 失败: %w", file.entry, err)
	}
	if _, err := io.Copy(target, in); err != nil {
		return fmt.Errorf("写入 zip 条目 %s 失败: %w", file.entry, err)
	}
	return nil
}

// verifySQLiteSnapshot 用 database/sql 只读打开快照并执行 integrity_check，
// 确认产出的是完整可读的 SQLite 数据库（而不是 WAL 撕裂副本）。
func verifySQLiteSnapshot(path string) error {
	sqlDB, err := repository.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	var integrity string
	if err := sqlDB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("读取快照校验结果失败: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("快照完整性校验未通过: %s", integrity)
	}
	return nil
}
