package biz

import (
	"context"
	"time"

	"gin-template/pkg/errs"
)

// ErrDemoNotFound 领域错误：记录不存在。
//
// 由 data 层在查询不到记录时返回。service 层无需再判断类型——
// pkg/response 会依据 errs.Error 携带的状态码与业务码渲染响应（404 / 40400）。
var ErrDemoNotFound = errs.NotFound("记录不存在")

// Demo 是 demo 模块的**领域模型**（业务模型）。
//
// 它是 biz 层对外暴露的唯一数据形态：
//   - data 层负责把它与数据库模型（PO）相互转换
//   - service 层负责把它与 DTO 相互转换
//
// 因此这里不带任何 GORM/JSON 标签——持久化细节留在 data 层，传输细节留在 service 层。
type Demo struct {
	ID          uint
	Name        string
	Description string
	Status      int
	CreatedBy   uint
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DemoRepo 定义 demo 的持久化能力（仓储接口）。
//
// **接口定义在 biz 层是分层的关键**：biz 只依赖抽象，data 层提供实现，
// 于是 biz 不需要 import data，依赖方向为 data → biz，不存在循环导入。
// 好处还有：biz 可用假实现做单元测试，无需真实数据库。
type DemoRepo interface {
	Create(ctx context.Context, demo *Demo) error
	GetByID(ctx context.Context, id uint) (*Demo, error)
	List(ctx context.Context, page, pageSize int) ([]*Demo, int64, error)
	Update(ctx context.Context, demo *Demo) error
	Delete(ctx context.Context, id uint) error
}

// wrapInternal 把基础设施错误归一化为统一的内部错误。
//
// 已经是 *errs.Error 的错误（含领域错误 ErrDemoNotFound）原样返回，
// 保留其业务码与 HTTP 状态码；其余（SQL/网络错误）包装为 500，
// 细节只进日志、不进入响应体（见 pkg/errs.Resolve 的兜底规则）。
func wrapInternal(message string, err error) error {
	if _, ok := errs.As(err); ok {
		return err
	}
	return errs.Internal(message).WithCause(err)
}
