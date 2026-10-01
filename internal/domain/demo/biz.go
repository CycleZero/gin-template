package demo

import (
	"log/slog"

	"gin-template/model"
)

// DemoBiz 业务逻辑层 - 处理业务规则和数据转换
type DemoBiz struct {
	logger   *slog.Logger
	demoRepo *DemoRepo
}

func NewDemoBiz(logger *slog.Logger, demoRepo *DemoRepo) *DemoBiz {
	return &DemoBiz{
		logger:   logger,
		demoRepo: demoRepo,
	}
}

// Create 创建新记录
func (b *DemoBiz) Create(name, description string, createdBy uint) (*model.Demo, error) {
	demo := &model.Demo{
		Name:        name,
		Description: description,
		Status:      1,
		CreatedBy:   createdBy,
	}
	err := b.demoRepo.Create(demo)
	if err != nil {
		b.logger.Error("创建 Demo 失败", "error", err)
		return nil, err
	}
	return demo, nil
}

// GetByID 获取记录
func (b *DemoBiz) GetByID(id uint) (*model.Demo, error) {
	demo, err := b.demoRepo.GetByID(id)
	if err != nil {
		b.logger.Error("获取 Demo 失败", "error", err, "id", id)
		return nil, err
	}
	return demo, nil
}

// List 获取列表
func (b *DemoBiz) List(page, pageSize int) ([]*model.Demo, int64, error) {
	return b.demoRepo.List(page, pageSize)
}

// Update 更新记录
func (b *DemoBiz) Update(id uint, name, description string) (*model.Demo, error) {
	demo, err := b.demoRepo.GetByID(id)
	if err != nil {
		b.logger.Error("获取 Demo 失败", "error", err, "id", id)
		return nil, err
	}
	demo.Name = name
	demo.Description = description
	err = b.demoRepo.Update(demo)
	if err != nil {
		b.logger.Error("更新 Demo 失败", "error", err)
		return nil, err
	}
	return demo, nil
}

// Delete 删除记录
func (b *DemoBiz) Delete(id uint) error {
	return b.demoRepo.Delete(id)
}
