package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirs(t *testing.T) {
	// Given: 各路径均不存在于空目录下的配置。
	base := t.TempDir()
	cfg := &Config{
		DBPath:    filepath.Join(base, "data", "hair-salon.db"),
		UploadDir: filepath.Join(base, "data", "uploads"),
		BackupDir: filepath.Join(base, "data", "backups"),
		LogDir:    filepath.Join(base, "logs"),
	}

	// When: 调用 EnsureDirs。
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Then: 数据库父目录与三个运行期目录全部存在，且二次调用幂等。
	for _, dir := range []string{
		filepath.Join(base, "data"),
		filepath.Join(base, "data", "uploads"),
		filepath.Join(base, "data", "backups"),
		filepath.Join(base, "logs"),
	} {
		stat, err := os.Stat(dir)
		if err != nil {
			t.Errorf("目录 %s 未创建: %v", dir, err)
			continue
		}
		if !stat.IsDir() {
			t.Errorf("%s 不是目录", dir)
		}
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Errorf("EnsureDirs 二次调用应幂等: %v", err)
	}
}
