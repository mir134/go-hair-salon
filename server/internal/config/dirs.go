package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDirs 创建运行期目录：数据库父目录、UPLOAD_DIR、BACKUP_DIR、LOG_DIR。
// 必须在打开数据库与启动日志前调用。
func (c *Config) EnsureDirs() error {
	targets := []struct {
		key  string
		path string
	}{
		{"DB_PATH 父目录", filepath.Dir(c.DBPath)},
		{"UPLOAD_DIR", c.UploadDir},
		{"BACKUP_DIR", c.BackupDir},
		{"LOG_DIR", c.LogDir},
	}
	for _, t := range targets {
		if t.path == "" || t.path == "." {
			continue
		}
		if err := os.MkdirAll(t.path, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s（%s）失败: %w", t.path, t.key, err)
		}
	}
	return nil
}
