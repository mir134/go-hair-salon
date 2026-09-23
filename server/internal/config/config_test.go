package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mir134/go-hair-salon/server/internal/config"
)

// validYAML 是只含必填项的最小配置（可选键省略，用于验证默认值）。
const validYAML = `DB_PATH: data/hair-salon.db
UPLOAD_DIR: data/uploads
BACKUP_DIR: data/backups
LOG_DIR: logs
JWT_SECRET: test-secret-for-unit-test-only
`

// requiredKeys 是必须出现在缺失提示中的必填配置键。
var requiredKeys = []string{"DB_PATH", "UPLOAD_DIR", "BACKUP_DIR", "LOG_DIR", "JWT_SECRET"}

// writeConfig 把 YAML 内容写入临时目录并返回文件路径。
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// clearEnv 清空可能污染测试的同名环境变量（环境变量优先级高于配置文件）。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DB_PATH", "UPLOAD_DIR", "BACKUP_DIR", "LOG_DIR",
		"SERVER_HOST", "SERVER_PORT", "JWT_SECRET",
		"INITIAL_ADMIN_PASSWORD", "BACKUP_TIME",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_AppliesDefaultsWhenOptionalKeysAbsent(t *testing.T) {
	// Given 只有必填项的配置，且相关环境变量为空
	clearEnv(t)

	// When 加载配置
	cfg, err := config.Load(writeConfig(t, validYAML))

	// Then 可选键取默认值
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ServerHost != "0.0.0.0" {
		t.Errorf("ServerHost = %q, want %q", cfg.ServerHost, "0.0.0.0")
	}
	if cfg.ServerPort != 8080 {
		t.Errorf("ServerPort = %d, want %d", cfg.ServerPort, 8080)
	}
	if cfg.InitialAdminPassword != "admin123" {
		t.Errorf("InitialAdminPassword = %q, want %q", cfg.InitialAdminPassword, "admin123")
	}
	if cfg.BackupTime != "23:00" {
		t.Errorf("BackupTime = %q, want %q", cfg.BackupTime, "23:00")
	}
}

func TestLoad_OverridesValuesFromEnvironment(t *testing.T) {
	// Given 配置文件已就位，且设置了同名环境变量
	clearEnv(t)
	t.Setenv("DB_PATH", `D:\hair-salon-data\hair-salon.db`)
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("JWT_SECRET", "env-secret-for-unit-test")

	// When 加载配置
	cfg, err := config.Load(writeConfig(t, validYAML))

	// Then 环境变量覆盖配置文件中的值
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBPath != `D:\hair-salon-data\hair-salon.db` {
		t.Errorf("DBPath = %q, want env value %q", cfg.DBPath, `D:\hair-salon-data\hair-salon.db`)
	}
	if cfg.ServerPort != 9090 {
		t.Errorf("ServerPort = %d, want env value %d", cfg.ServerPort, 9090)
	}
	if cfg.JWTSecret != "env-secret-for-unit-test" {
		t.Errorf("JWTSecret = %q, want env value", cfg.JWTSecret)
	}
}

func TestLoad_MissingConfigFileListsRequiredKeys(t *testing.T) {
	// Given 配置文件不存在
	clearEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	// When 加载配置
	_, err := config.Load(missing)

	// Then 报错并列出全部必填键
	if err == nil {
		t.Fatal("Load() error = nil, want missing-file error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q should name the missing file %q", err, missing)
	}
	for _, key := range requiredKeys {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q should name required key %s", err, key)
		}
	}
}

func TestLoad_MissingRequiredKeysAreAllNamed(t *testing.T) {
	// Given 配置文件只提供了一个可选键
	clearEnv(t)

	// When 加载配置
	_, err := config.Load(writeConfig(t, "SERVER_PORT: 9090\n"))

	// Then 报错一次性列出全部缺失的必填键
	if err == nil {
		t.Fatal("Load() error = nil, want missing-keys error")
	}
	for _, key := range requiredKeys {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q should name missing key %s", err, key)
		}
	}
}

func TestLoad_EmptyJWTSecretIsRejected(t *testing.T) {
	// Given JWT_SECRET 显式留空（禁止启动时使用空密钥）
	clearEnv(t)
	yaml := strings.Replace(validYAML, "JWT_SECRET: test-secret-for-unit-test-only", `JWT_SECRET: ""`, 1)

	// When 加载配置
	_, err := config.Load(writeConfig(t, yaml))

	// Then 启动报错且错误信息点名 JWT_SECRET
	if err == nil {
		t.Fatal("Load() error = nil, want JWT_SECRET error")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error %q should mention JWT_SECRET", err)
	}
}

func TestLoad_RejectsInvalidServerPort(t *testing.T) {
	// Given SERVER_PORT 超出合法范围
	clearEnv(t)
	yaml := validYAML + "SERVER_PORT: 70000\n"

	// When 加载配置
	_, err := config.Load(writeConfig(t, yaml))

	// Then 报错点名 SERVER_PORT
	if err == nil {
		t.Fatal("Load() error = nil, want port validation error")
	}
	if !strings.Contains(err.Error(), "SERVER_PORT") {
		t.Errorf("error %q should mention SERVER_PORT", err)
	}
}

func TestLoad_RejectsInvalidBackupTime(t *testing.T) {
	// Given BACKUP_TIME 不是 HH:MM 格式
	clearEnv(t)
	yaml := validYAML + "BACKUP_TIME: \"25:99\"\n"

	// When 加载配置
	_, err := config.Load(writeConfig(t, yaml))

	// Then 报错点名 BACKUP_TIME
	if err == nil {
		t.Fatal("Load() error = nil, want backup-time validation error")
	}
	if !strings.Contains(err.Error(), "BACKUP_TIME") {
		t.Errorf("error %q should mention BACKUP_TIME", err)
	}
}

func TestLoad_RejectsNetworkShareDBPath(t *testing.T) {
	// Given DB_PATH 指向 UNC 网络共享目录（AGENTS.md 硬规则：SQLite 只能放本机磁盘）
	clearEnv(t)
	yaml := strings.Replace(validYAML, "DB_PATH: data/hair-salon.db", `DB_PATH: '\\nas\share\hair-salon.db'`, 1)

	// When 加载配置
	_, err := config.Load(writeConfig(t, yaml))

	// Then 报错点名 DB_PATH
	if err == nil {
		t.Fatal("Load() error = nil, want network-share rejection")
	}
	if !strings.Contains(err.Error(), "DB_PATH") {
		t.Errorf("error %q should mention DB_PATH", err)
	}
}
