package repository_test

// 本文件验证默认服务分类的首次启动播种（03-DATABASE.md:87-96）：
//   - 空表时播种产品负责人确认的 5 个默认分类（剪发/烫发/染发/护理/造型）；
//   - 重复调用幂等（不产生重复行）；
//   - 表非空（含管理员自定义分类）时跳过播种，保持原数据不变。

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
	"github.com/mir134/go-hair-salon/server/internal/repository"
)

// openMigratedTestDB 打开一个全新的临时 SQLite 数据库并完成迁移（测试自包含辅助）。
func openMigratedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "servicecategory-test.db")
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

func TestEnsureDefaultCategories(t *testing.T) {
	// Given: 全新迁移后的数据库，service_categories 为空。
	db := openMigratedTestDB(t)

	// When: 首次播种默认分类。
	if err := repository.EnsureDefaultCategories(db); err != nil {
		t.Fatalf("repository.EnsureDefaultCategories: %v", err)
	}

	// Then: 恰好 5 个分类，名称/排序/状态与产品确认的默认值一致。
	var items []model.ServiceCategory
	if err := db.Order("sort ASC, id ASC").Find(&items).Error; err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("分类数 = %d, want 5", len(items))
	}
	wantNames := []string{"剪发", "烫发", "染发", "护理", "造型"}
	wantSorts := []int{10, 20, 30, 40, 50}
	for i, item := range items {
		if item.Name != wantNames[i] {
			t.Errorf("items[%d].Name = %q, want %q", i, item.Name, wantNames[i])
		}
		if item.Sort != wantSorts[i] {
			t.Errorf("items[%d].Sort = %d, want %d", i, item.Sort, wantSorts[i])
		}
		if item.Status != model.StatusEnabled {
			t.Errorf("items[%d].Status = %d, want %d", i, item.Status, model.StatusEnabled)
		}
	}
}

func TestEnsureDefaultCategoriesIdempotent(t *testing.T) {
	// Given: 已播种默认分类的数据库。
	db := openMigratedTestDB(t)
	if err := repository.EnsureDefaultCategories(db); err != nil {
		t.Fatalf("first EnsureDefaultCategories: %v", err)
	}

	// When: 再次调用（模拟重复启动）。
	if err := repository.EnsureDefaultCategories(db); err != nil {
		t.Fatalf("second EnsureDefaultCategories: %v", err)
	}

	// Then: 仍是 5 行，没有重复播种。
	var count int64
	if err := db.Model(&model.ServiceCategory{}).Count(&count).Error; err != nil {
		t.Fatalf("count categories: %v", err)
	}
	if count != 5 {
		t.Errorf("分类数 = %d, want 5", count)
	}
}

func TestEnsureDefaultCategoriesSkipsWhenNotEmpty(t *testing.T) {
	// Given: 管理员已自建一个分类。
	db := openMigratedTestDB(t)
	custom := model.ServiceCategory{Name: "管理员自定义", Sort: 99, Status: model.StatusEnabled}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create custom category: %v", err)
	}

	// When: 执行默认播种。
	if err := repository.EnsureDefaultCategories(db); err != nil {
		t.Fatalf("repository.EnsureDefaultCategories: %v", err)
	}

	// Then: 仅保留自定义行，不插入任何默认分类、不修改原行。
	var items []model.ServiceCategory
	if err := db.Order("id ASC").Find(&items).Error; err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("分类数 = %d, want 1（非空表不得播种）", len(items))
	}
	if items[0].Name != "管理员自定义" || items[0].Sort != 99 || items[0].Status != model.StatusEnabled {
		t.Errorf("自定义分类被改动: %+v", items[0])
	}
}
