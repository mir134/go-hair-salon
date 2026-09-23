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

// SettingsRepository 提供 settings 表的读取访问（03-DATABASE.md:242-263）。
//
// 设置项修改 API 由 todo 41-43 提供；本文件当前只实现消费事务所需的按键读取。
type SettingsRepository struct {
	db *gorm.DB
}

// NewSettingsRepository 构造系统设置仓储。
func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
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
