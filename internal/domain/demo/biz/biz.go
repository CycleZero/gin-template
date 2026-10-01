// Package biz 是 demo 模块的业务逻辑层：业务规则、流程编排与数据转换。
//
// 只依赖 data 层，不感知 HTTP（因此可被 HTTP / gRPC / 定时任务等复用）。
package biz

import (
	"log/slog"

	"gin-template/internal/domain/demo/data"
)

// DemoBiz 业务逻辑层 - 处理业务规则和数据转换
type DemoBiz struct {
	logger   *slog.Logger
	demoRepo *data.DemoRepo
}

func NewDemoBiz(logger *slog.Logger, demoRepo *data.DemoRepo) *DemoBiz {
	return &DemoBiz{
		logger:   logger,
		demoRepo: demoRepo,
	}
}

// Create 创建新记录
func (b *DemoBiz) Create(name, description string, createdBy uint) (*data.Demo, error) {
	demo := &data.Demo{
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
func (b *DemoBiz) GetByID(id uint) (*data.Demo, error) {
	demo, err := b.demoRepo.GetByID(id)
	if err != nil {
		b.logger.Error("获取 Demo 失败", "error", err, "id", id)
		return nil, err
	}
	return demo, nil
}

// List 获取列表
func (b *DemoBiz) List(page, pageSize int) ([]*data.Demo, int64, error) {
	return b.demoRepo.List(page, pageSize)
}

// Update 更新记录
func (b *DemoBiz) Update(id uint, name, description string) (*data.Demo, error) {
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
