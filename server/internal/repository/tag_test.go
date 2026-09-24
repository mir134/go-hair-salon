package repository_test

// 本文件验证默认客户标签的首次启动播种（03-DATABASE.md:69-77）：
//   - 空表时播种产品负责人确认的 4 个默认标签（老客户/新客/会员/意向客户），color 留空；
//   - 重复调用幂等（不产生重复行）；
//   - 表非空（含管理员自定义标签）时跳过播种，保持原数据不变；
//   - 全部软删除后重启不复活（物理行仍占用 tags 表，不再播种）。

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

func TestEnsureDefaultTags(t *testing.T) {
	// Given: 全新迁移后的数据库，tags 为空。
	db := openMigratedTagTestDB(t)

	// When: 首次播种默认标签。
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("repository.EnsureDefaultTags: %v", err)
	}

	// Then: 恰好 4 个标签，名称按 id 升序与产品确认的默认值一致；未软删除、颜色为空。
	var items []model.Tag
	if err := db.Order("id ASC").Find(&items).Error; err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("标签数 = %d, want 4", len(items))
	}
	wantNames := []string{"老客户", "新客", "会员", "意向客户"}
	for i, item := range items {
		if item.Name != wantNames[i] {
			t.Errorf("items[%d].Name = %q, want %q", i, item.Name, wantNames[i])
		}
		if item.DeletedAt.Valid {
			t.Errorf("items[%d].DeletedAt = %v, want 零值（未软删除）", i, item.DeletedAt.Time)
		}
		if item.Color != "" {
			t.Errorf("items[%d].Color = %q, want 空字符串", i, item.Color)
		}
	}
}

func TestEnsureDefaultTagsIdempotent(t *testing.T) {
	// Given: 已播种默认标签的数据库。
	db := openMigratedTagTestDB(t)
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("first EnsureDefaultTags: %v", err)
	}

	// When: 再次调用（模拟重复启动）。
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("second EnsureDefaultTags: %v", err)
	}

	// Then: 仍是 4 行，没有重复播种。
	var count int64
	if err := db.Model(&model.Tag{}).Count(&count).Error; err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if count != 4 {
		t.Errorf("标签数 = %d, want 4", count)
	}
}

func TestEnsureDefaultTagsSkipsWhenNotEmpty(t *testing.T) {
	// Given: 管理员已自建一个标签。
	db := openMigratedTagTestDB(t)
	custom := model.Tag{Name: "管理员自定义"}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create custom tag: %v", err)
	}

	// When: 执行默认播种。
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("repository.EnsureDefaultTags: %v", err)
	}

	// Then: 仅保留自定义行，不插入任何默认标签、不修改原行。
	var items []model.Tag
	if err := db.Order("id ASC").Find(&items).Error; err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("标签数 = %d, want 1（非空表不得播种）", len(items))
	}
	if items[0].Name != "管理员自定义" || items[0].ID != custom.ID {
		t.Errorf("自定义标签被改动: %+v", items[0])
	}
}

func TestEnsureDefaultTagsSkipsWhenAllSoftDeleted(t *testing.T) {
	// Given: 默认标签已播种、随后被管理员全部软删除（tags 表物理上仍有 4 行）。
	db := openMigratedTagTestDB(t)
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("first EnsureDefaultTags: %v", err)
	}
	if err := db.Delete(&model.Tag{}, "1=1").Error; err != nil {
		t.Fatalf("soft delete all tags: %v", err)
	}

	// When: 再次执行默认播种（模拟重启）。
	if err := repository.EnsureDefaultTags(db); err != nil {
		t.Fatalf("second EnsureDefaultTags: %v", err)
	}

	// Then: 不复活任何标签——物理行总数仍 4，未删除数为 0。
	var physical, active int64
	if err := db.Unscoped().Model(&model.Tag{}).Count(&physical).Error; err != nil {
		t.Fatalf("count tags (unscoped): %v", err)
	}
	if physical != 4 {
		t.Errorf("物理标签数 = %d, want 4（软删除行不得触发重复播种）", physical)
	}
	if err := db.Model(&model.Tag{}).Count(&active).Error; err != nil {
		t.Fatalf("count active tags: %v", err)
	}
	if active != 0 {
		t.Errorf("未删除标签数 = %d, want 0（全部软删除后不得复活）", active)
	}
}

// --- helpers ---

// openMigratedTagTestDB 打开一个全新的临时 SQLite 数据库并完成迁移（测试自包含辅助）。
func openMigratedTagTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tag-test.db")
	db, err := repository.Open(dbPath)
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
	return db
}
