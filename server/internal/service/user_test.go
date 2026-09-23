package service_test

// TestSeedAdmin 是 todo 9 的验收测试（计划：
// `go test ./internal/service -run TestSeedAdmin -v -count=1`）：
//   - users 空表首次启动 → 创建 admin（bcrypt 哈希，前缀 $2a$，非明文）；
//   - 第二次启动（表非空）→ 不新增用户、不重置密码；
//   - 员工账号创建/改密/重置/停用：改密后旧密码 bcrypt 校验失败；
//     停用只改 status，保留账号记录（06-BUSINESS-RULES.md:120-125 禁止物理删除 user）。

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
	"github.com/mir134/go-hair-salon/server/internal/service"
)

// newUserSvc 构造已迁移的临时库与用户服务。
func newUserSvc(t *testing.T) (*service.UserService, *gorm.DB) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("repository.Migrate: %v", err)
	}
	return service.NewUserService(repository.NewUserRepository(db)), db
}

func TestSeedAdmin(t *testing.T) {
	svc, db := newUserSvc(t)
	const initialPassword = "Init-Pwd-9x"

	// When: 空表首次播种初始管理员。
	created, err := svc.SeedAdmin(context.Background(), initialPassword)

	// Then: 创建成功且落库行为 admin。
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if !created {
		t.Fatal("首次播种 created = false, want true")
	}
	var admin model.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("查询 admin 用户: %v", err)
	}
	if admin.Role != model.RoleAdmin {
		t.Errorf("role = %q, want %q", admin.Role, model.RoleAdmin)
	}
	if admin.Status != model.StatusEnabled {
		t.Errorf("status = %d, want %d（启用）", admin.Status, model.StatusEnabled)
	}

	// Then: password_hash 是 bcrypt 哈希而非明文（验收：前缀 $2a$）。
	if admin.PasswordHash == initialPassword {
		t.Fatal("password_hash 存了明文密码")
	}
	if !strings.HasPrefix(admin.PasswordHash, "$2a$") {
		t.Errorf("password_hash = %q, want $2a$ 前缀的 bcrypt 哈希", admin.PasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(initialPassword)); err != nil {
		t.Errorf("初始密码 bcrypt 校验失败: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("wrong-password")); err == nil {
		t.Error("错误密码不应通过 bcrypt 校验")
	}

	// When: 模拟第二次启动再次播种（users 表已非空）。
	createdAgain, err := svc.SeedAdmin(context.Background(), "Another-Pwd-1")

	// Then: 不新增、不重置，仍恰好一个 admin。
	if err != nil {
		t.Fatalf("第二次 SeedAdmin: %v", err)
	}
	if createdAgain {
		t.Error("第二次播种 created = true, want false（幂等）")
	}
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("users 行数 = %d, want 1", count)
	}
	var reloaded model.User
	if err := db.Where("username = ?", "admin").First(&reloaded).Error; err != nil {
		t.Fatalf("重查 admin: %v", err)
	}
	if reloaded.PasswordHash != admin.PasswordHash {
		t.Error("第二次播种改写了既有 admin 密码（必须幂等）")
	}
}

func TestUserPasswordLifecycle(t *testing.T) {
	svc, db := newUserSvc(t)
	ctx := context.Background()
	if _, err := svc.SeedAdmin(ctx, "Init-Pwd-9x"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}

	// When: 创建员工账号。
	staff, err := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "staff1",
		Password: "Staff-Pwd-1",
		Role:     model.RoleStaff,
	})

	// Then: 落库启用、密码为 bcrypt 哈希。
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if staff.Status != model.StatusEnabled || staff.Role != model.RoleStaff {
		t.Errorf("staff = %+v, want role=staff status=1", staff)
	}
	var row model.User
	if err := db.First(&row, staff.ID).Error; err != nil {
		t.Fatalf("查询 staff: %v", err)
	}
	if !strings.HasPrefix(row.PasswordHash, "$2a$") {
		t.Errorf("staff password_hash = %q, want $2a$ 前缀", row.PasswordHash)
	}

	// When: 修改自己的密码（旧密码正确）。
	if err := svc.ChangeOwnPassword(ctx, staff.ID, "Staff-Pwd-1", "Staff-Pwd-2"); err != nil {
		t.Fatalf("ChangeOwnPassword: %v", err)
	}

	// Then: 旧密码失效、新密码可用。
	if err := db.First(&row, staff.ID).Error; err != nil {
		t.Fatalf("重查 staff: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("Staff-Pwd-1")); err == nil {
		t.Error("改密后旧密码仍通过校验")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("Staff-Pwd-2")); err != nil {
		t.Errorf("新密码校验失败: %v", err)
	}

	// When: 旧密码错误时修改密码。
	err = svc.ChangeOwnPassword(ctx, staff.ID, "wrong-old", "Staff-Pwd-3")

	// Then: 返回 422 业务错误且密码未变。
	var biz *service.BizError
	if !errors.As(err, &biz) || biz.Status != 422 {
		t.Fatalf("旧密码错误 err = %v, want *BizError(422)", err)
	}
	if err := db.First(&row, staff.ID).Error; err != nil {
		t.Fatalf("重查 staff: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("Staff-Pwd-2")); err != nil {
		t.Error("旧密码校验失败后密码被改动")
	}

	// When: 重置他人密码（管理员场景）。
	if err := svc.ResetPassword(ctx, staff.ID, "Reset-Pwd-9"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	// Then: 重置后的密码可用。
	if err := db.First(&row, staff.ID).Error; err != nil {
		t.Fatalf("重查 staff: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("Reset-Pwd-9")); err != nil {
		t.Errorf("重置后密码校验失败: %v", err)
	}

	// When: 停用账号。
	if err := svc.DisableUser(ctx, staff.ID); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	// Then: status=0 且行仍存在（禁止物理删除用户）。
	if err := db.First(&row, staff.ID).Error; err != nil {
		t.Fatalf("停用后用户行被物理删除: %v", err)
	}
	if row.Status != model.StatusDisabled {
		t.Errorf("停用后 status = %d, want 0", row.Status)
	}

	// When: 对不存在的用户停用/重置密码。
	errDisable := svc.DisableUser(ctx, 99999)
	errReset := svc.ResetPassword(ctx, 99999, "Whatever-Pwd-1")

	// Then: 均返回 404 业务错误。
	if !errors.As(errDisable, &biz) || biz.Status != 404 {
		t.Errorf("停用不存在用户 err = %v, want *BizError(404)", errDisable)
	}
	if !errors.As(errReset, &biz) || biz.Status != 404 {
		t.Errorf("重置不存在用户 err = %v, want *BizError(404)", errReset)
	}

	// When: 创建重名用户。
	dupErr := func() error {
		_, err := svc.CreateUser(ctx, service.CreateUserInput{Username: "staff1", Password: "X-Pwd-123", Role: model.RoleStaff})
		return err
	}()

	// Then: 返回 409 冲突错误。
	if !errors.As(dupErr, &biz) || biz.Status != 409 {
		t.Errorf("重名创建 err = %v, want *BizError(409)", dupErr)
	}
}
