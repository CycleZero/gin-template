package service

import "gin-template/internal/domain/demo/biz"

// CreateDemoRequest 创建请求
type CreateDemoRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// UpdateDemoRequest 更新请求
type UpdateDemoRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// ListDemoRequest 列表查询请求。
//
// 分页参数用 omitempty 允许缺省，上限由 binding 强制（max=100），
// 避免 `page_size=100000` 这类请求把整表拉出来。
type ListDemoRequest struct {
	Page     int `form:"page" binding:"omitempty,min=1"`
	PageSize int `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// DemoResponse 响应
type DemoResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      int    `json:"status"`
	CreatedBy   uint   `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ListDemoResponse 列表响应
type ListDemoResponse struct {
	List  []*DemoResponse `json:"list"`
	Total int64           `json:"total"`
	Page  int             `json:"page"`
}

// ============================================================
// biz 领域模型 ↔ DTO 转换（service 层的职责）
// ============================================================

// newDemoResponse 把 biz 领域模型转换为响应 DTO。
//
// 时间统一格式化为字符串（对外契约），不把 time.Time 直接暴露出去。
func newDemoResponse(d *biz.Demo) *DemoResponse {
	if d == nil {
		return nil
	}
	return &DemoResponse{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Status:      d.Status,
		CreatedBy:   d.CreatedBy,
		CreatedAt:   d.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   d.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// newListDemoResponse 批量转换列表响应。
func newListDemoResponse(demos []*biz.Demo, total int64, page int) ListDemoResponse {
	list := make([]*DemoResponse, 0, len(demos))
	for _, d := range demos {
		list = append(list, newDemoResponse(d))
	}
	return ListDemoResponse{
		List:  list,
		Total: total,
		Page:  page,
	}
}
