package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/mir134/go-hair-salon/server/internal/model"
)

// defaultSettings 是首次启动必须存在的系统设置默认值（03-DATABASE.md:256-261）。
var defaultSettings = []model.Setting{
	{Key: model.SettingPointsPerYuan, Value: "1", Description: "每消费 1 元获得的积分"},
	{Key: model.SettingShopName, Value: model.DefaultShopName, Description: "门店名称（顶部显示）"},
}

// SettingsRepository 提供 settings 表的读写访问（03-DATABASE.md:242-263）。
//
// 设置项修改 API 由 todo 41-43 提供；消费事务按键读取积分比例。
type SettingsRepository struct {
	db *gorm.DB
}

// NewSettingsRepository 构造系统设置仓储。
func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

// List 返回全部设置项（按 id 升序，顺序稳定）；公开键过滤由 service 层负责。
func (r *SettingsRepository) List(ctx context.Context) ([]model.Setting, error) {
	var rows []model.Setting
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindByKey 按键查询设置项；不存在返回 ErrNotFound。
func (r *SettingsRepository) FindByKey(ctx context.Context, key string) (*model.Setting, error) {
	var setting model.Setting
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &setting, nil
}

// UpdateValue 按键更新设置值与最后修改人（updated_at 由 GORM 自动维护）。
//
// 不新增键：键不存在（RowsAffected=0）返回 ErrNotFound，调用方译为 404
// （新增设置项必须同步 03-DATABASE.md 与 06-BUSINESS-RULES.md，不在本波范围）。
func (r *SettingsRepository) UpdateValue(ctx context.Context, key, value string, updatedBy *int64) error {
	result := r.db.WithContext(ctx).Model(&model.Setting{}).Where("key = ?", key).Updates(map[string]any{
		"value":      value,
		"updated_by": updatedBy,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureDefaultSettings 幂等播种 settings 默认值：已存在的键保留现值，不覆盖管理员修改。
func EnsureDefaultSettings(db *gorm.DB) error {
	for i := range defaultSettings {
		setting := defaultSettings[i]
		var existing model.Setting
		err := db.Where("key = ?", setting.Key).First(&existing).Error
		switch {
		case err == nil:
			continue // 已存在，保留现值
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := db.Create(&setting).Error; err != nil {
				return fmt.Errorf("播种 settings[%s] 失败: %w", setting.Key, err)
			}
		default:
			return fmt.Errorf("读取 settings[%s] 失败: %w", setting.Key, err)
		}
	}
	return nil
}
