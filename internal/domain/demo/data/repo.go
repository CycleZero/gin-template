// Package data 是 demo 模块的数据访问层：封装对 demo 表的 GORM 操作。
//
// 只依赖 pkg/infra 与 model，不感知业务规则与 HTTP。
package data

import (
	"gin-template/model"
	"gin-template/pkg/infra"

	"gorm.io/gorm"
)

// DemoRepo 数据访问层 - 封装对 demo 表的数据库操作
type DemoRepo struct {
	db        *gorm.DB
	infraData *infra.Data
}

func NewDemoRepo(infraData *infra.Data) *DemoRepo {
	// AutoMigrate 自动创建/更新表结构
	if err := infraData.DB.AutoMigrate(&model.Demo{}); err != nil {
		panic(err)
	}
	return &DemoRepo{db: infraData.DB, infraData: infraData}
}

// Create 创建记录
func (r *DemoRepo) Create(demo *model.Demo) error {
	return r.db.Create(demo).Error
}

// GetByID 根据 ID 获取记录
func (r *DemoRepo) GetByID(id uint) (*model.Demo, error) {
	var demo model.Demo
	err := r.db.First(&demo, id).Error
	if err != nil {
		return nil, err
	}
	return &demo, nil
}

// List 获取列表，支持分页
func (r *DemoRepo) List(page, pageSize int) ([]*model.Demo, int64, error) {
	var demos []*model.Demo
	var total int64

	db := r.db.Model(&model.Demo{})
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Offset(offset).Limit(pageSize).Order("id DESC").Find(&demos).Error
	return demos, total, err
}

// Update 更新记录
func (r *DemoRepo) Update(demo *model.Demo) error {
	return r.db.Save(demo).Error
}

// Delete 删除记录
func (r *DemoRepo) Delete(id uint) error {
	return r.db.Delete(&model.Demo{}, id).Error
}
