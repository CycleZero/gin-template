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

// Create 写入一条记录；成功后把数据库生成的字段（自增主键、时间戳）回填到领域模型。
//
// 新建记录的 ID/CreatedAt/UpdatedAt 为零值，由 GORM 填充（见 fromBiz 的说明）。
func (r *demoRepo) Create(ctx context.Context, demo *biz.Demo) error {
	po := fromBiz(demo)
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

// Update 按主键**全字段**写回领域模型（GORM Save 会更新所有字段，含零值字段）。
//
// 期望传入的是 biz 从仓储读出的完整领域模型（GetByID / List 的产物）；
// created_at 等审计字段随之原样回写，不做裁剪。
func (r *demoRepo) Update(ctx context.Context, demo *biz.Demo) error {
	// 先确认记录存在（未命中返回领域错误），并保留软删除标记
	var existing Demo
	if err := r.db.WithContext(ctx).First(&existing, demo.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return biz.ErrDemoNotFound
		}
		return err
	}

	po := fromBiz(demo)
	// deleted_at 是仓储侧的软删除状态，不属于领域模型，按库中原值保留
	po.DeletedAt = existing.DeletedAt

	if err := r.db.WithContext(ctx).Save(po).Error; err != nil {
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

// ============================================================
// PO ↔ 领域模型转换（data 层的职责）
// ============================================================

// toBiz 把数据库模型转换为领域模型（逐字段 1:1）。
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

// fromBiz 把领域模型**完整**转换为数据库模型。
//
// 忠实映射：biz.Demo 上的每个字段都 1:1 写入 PO，不做字段裁剪——
// 这样领域模型新增字段时只需改这一处，不会出现"某个字段被静默丢弃"。
//
// 唯一不来自领域模型的是 deleted_at（软删除是仓储侧状态，由调用方在 Update 中保留原值）。
// created_at 随领域模型原样回写：新建时为零值 → 由 GORM 自动填充；更新时传入的是已加载的
// 完整模型，因此是原值回写而非清零。
// updated_at 虽然也在此映射，但更新时会被 GORM 的 autoUpdateTime 改写为当前时间，
// 写入后由 Update 回填到领域模型。
func fromBiz(src *biz.Demo) *Demo {
	return &Demo{
		Model: gorm.Model{
			ID:        src.ID,
			CreatedAt: src.CreatedAt,
			UpdatedAt: src.UpdatedAt,
		},
		Name:        src.Name,
		Description: src.Description,
		Status:      src.Status,
		CreatedBy:   src.CreatedBy,
	}
}
