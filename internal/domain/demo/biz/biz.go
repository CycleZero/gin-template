// Package biz 是 demo 模块的业务逻辑层（DDD 的 domain/service 层）。
//
// 职责：
//   - 定义领域模型（demo.go 的 Demo）与仓储接口（DemoRepo）——依赖倒置
//   - 编排业务流程、执行业务规则，返回领域模型或领域错误
//
// 约束：只依赖标准库与仓储**接口**，不 import data，也不感知 HTTP，
// 因此同一份业务可被 HTTP / gRPC / 定时任务等复用，并可注入假仓储做单元测试。
package biz

import (
	"context"
	"log/slog"
)

// DemoBiz demo 模块的业务逻辑实现。
type DemoBiz struct {
	logger *slog.Logger
	repo   DemoRepo
}

// NewDemoBiz 由 Wire 注入仓储接口实现（实际是 data 层的 demoRepo）。
func NewDemoBiz(logger *slog.Logger, repo DemoRepo) *DemoBiz {
	return &DemoBiz{
		logger: logger,
		repo:   repo,
	}
}

// Create 创建记录，返回新建的领域模型。
func (b *DemoBiz) Create(ctx context.Context, name, description string, createdBy uint) (*Demo, error) {
	demo := &Demo{
		Name:        name,
		Description: description,
		Status:      1, // 新建默认启用
		CreatedBy:   createdBy,
	}
	if err := b.repo.Create(ctx, demo); err != nil {
		b.logger.ErrorContext(ctx, "创建 Demo 失败", "error", err)
		return nil, err
	}
	return demo, nil
}

// GetByID 获取单条记录；不存在时返回 ErrDemoNotFound。
func (b *DemoBiz) GetByID(ctx context.Context, id uint) (*Demo, error) {
	demo, err := b.repo.GetByID(ctx, id)
	if err != nil {
		b.logger.ErrorContext(ctx, "获取 Demo 失败", "error", err, "id", id)
		return nil, err
	}
	return demo, nil
}

// List 分页获取记录。
func (b *DemoBiz) List(ctx context.Context, page, pageSize int) ([]*Demo, int64, error) {
	return b.repo.List(ctx, page, pageSize)
}

// Update 更新记录：先取出领域模型，改字段后再交给仓储持久化。
func (b *DemoBiz) Update(ctx context.Context, id uint, name, description string) (*Demo, error) {
	demo, err := b.repo.GetByID(ctx, id)
	if err != nil {
		b.logger.ErrorContext(ctx, "获取 Demo 失败", "error", err, "id", id)
		return nil, err
	}

	demo.Name = name
	demo.Description = description
	if err := b.repo.Update(ctx, demo); err != nil {
		b.logger.ErrorContext(ctx, "更新 Demo 失败", "error", err, "id", id)
		return nil, err
	}
	return demo, nil
}

// Delete 删除记录。
func (b *DemoBiz) Delete(ctx context.Context, id uint) error {
	return b.repo.Delete(ctx, id)
}
