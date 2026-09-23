package repository

import (
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
