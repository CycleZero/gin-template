package data

import (
	"context"
	"errors"

	"gin-template/internal/domain/demo/biz"
	"gin-template/pkg/infra"

	"gorm.io/gorm"
)

// demoRepo 是 biz.DemoRepo 接口的数据库实现。
//
// 注意 NewDemoRepo 直接返回 biz.DemoRepo（接口），因此 Wire 无需 wire.Bind：
// 依赖倒置由「接口定义在 biz、实现在 data」自然完成。
type demoRepo struct {
	db        *gorm.DB
	infraData *infra.Data
}

func NewDemoRepo(infraData *infra.Data) biz.DemoRepo {
	// AutoMigrate 自动创建/更新表结构
	if err := infraData.DB.AutoMigrate(&Demo{}); err != nil {
		panic(err)
	}
	return &demoRepo{db: infraData.DB, infraData: infraData}
}

// Create 写入一条记录；成功后把自增主键与时间戳回填到领域模型。
func (r *demoRepo) Create(ctx context.Context, demo *biz.Demo) error {
	po := &Demo{
		Name:        demo.Name,
		Description: demo.Description,
		Status:      demo.Status,
		CreatedBy:   demo.CreatedBy,
	}
	if err := r.db.WithContext(ctx).Create(po).Error; err != nil {
		return err
	}

	demo.ID = po.ID
	demo.CreatedAt = po.CreatedAt
	demo.UpdatedAt = po.UpdatedAt
	return nil
}

// GetByID 按主键查询；未命中时返回领域错误 biz.ErrDemoNotFound，
// 使上层不依赖 gorm.ErrRecordNotFound。
func (r *demoRepo) GetByID(ctx context.Context, id uint) (*biz.Demo, error) {
	var po Demo
	if err := r.db.WithContext(ctx).First(&po, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, biz.ErrDemoNotFound
		}
		return nil, err
	}
	return po.toBiz(), nil
}

// List 分页查询，返回领域模型切片与总数。
func (r *demoRepo) List(ctx context.Context, page, pageSize int) ([]*biz.Demo, int64, error) {
	var (
		pos   []Demo
		total int64
	)

	query := r.db.WithContext(ctx).Model(&Demo{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&pos).Error; err != nil {
		return nil, 0, err
	}

	demos := make([]*biz.Demo, 0, len(pos))
	for i := range pos {
		demos = append(demos, pos[i].toBiz())
	}
	return demos, total, nil
}

// Update 按主键更新可变字段。
//
// 领域模型 → PO 的转换集中在 applyBiz，避免字段映射散落在各方法里。
func (r *demoRepo) Update(ctx context.Context, demo *biz.Demo) error {
	var po Demo
	if err := r.db.WithContext(ctx).First(&po, demo.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return biz.ErrDemoNotFound
		}
		return err
	}

	po.applyBiz(demo)
	if err := r.db.WithContext(ctx).Save(&po).Error; err != nil {
		return err
	}

	demo.UpdatedAt = po.UpdatedAt
	return nil
}

// Delete 按主键软删除（GORM gorm.Model 带 DeletedAt）。
func (r *demoRepo) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&Demo{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return biz.ErrDemoNotFound
	}
	return nil
}

// toBiz 把数据库模型转换为领域模型。
func (d *Demo) toBiz() *biz.Demo {
	return &biz.Demo{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Status:      d.Status,
		CreatedBy:   d.CreatedBy,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

// applyBiz 把领域模型的可变字段写回数据库模型（不改主键与创建信息）。
func (d *Demo) applyBiz(src *biz.Demo) {
	d.Name = src.Name
	d.Description = src.Description
	d.Status = src.Status
}
