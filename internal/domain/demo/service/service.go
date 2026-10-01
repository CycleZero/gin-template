// Package service 是 demo 模块的 HTTP 层（DDD 的 interface/transport 层）。
//
// 职责：
//   - 解析请求、校验参数（DTO 定义见 dto.go）
//   - 调用 biz 用例，并把 biz 领域模型转换为响应 DTO（转换函数在 dto.go）
//   - 用 pkg/response 渲染：成功走 OK（业务数据统一放在 data 字段下），
//     失败时 4xx 走 Fail、5xx 走 Error（见 respondBizError）
//
// 约束：只依赖 biz，**不 import data**——HTTP 层不会泄漏数据库模型。
package service

import (
	"log/slog"
	"net/http"
	"strconv"

	"gin-template/internal/domain/demo/biz"
	"gin-template/pkg/errs"
	"gin-template/pkg/response"

	"github.com/gin-gonic/gin"
)

// DemoService HTTP 服务层 - 处理请求解析、参数校验、响应格式化
type DemoService struct {
	demoBiz *biz.DemoBiz
	logger  *slog.Logger
}

func NewDemoService(demoBiz *biz.DemoBiz, logger *slog.Logger) *DemoService {
	return &DemoService{
		demoBiz: demoBiz,
		logger:  logger,
	}
}

// respondBizError 按错误性质选择响应通道：
//   - 4xx（如领域错误 ErrDemoNotFound）→ response.Fail，不产生错误日志
//   - 5xx（依赖故障、超时、未归一化错误）→ response.Error，记录错误日志
//
// 这样 handler 不必逐个判断错误类型，同时保证监控面板里的 4xx/5xx 归类正确。
func respondBizError(c *gin.Context, err error) {
	if status, _, _ := errs.Resolve(err); status >= http.StatusInternalServerError {
		response.Error(c, err)
		return
	}
	response.Fail(c, err)
}

// Create 创建 Demo
// @Summary 创建 Demo
// @Tags demo
// @Accept json
// @Produce json
// @Param request body CreateDemoRequest true "创建请求"
// @Success 200 {object} DemoResponse
// @Router /api/demo [post]
func (s *DemoService) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateDemoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.ErrorContext(ctx, "解析创建请求失败", "error", err)
		response.Fail(c, errs.InvalidArgument("请求参数错误"))
		return
	}

	demo, err := s.demoBiz.Create(ctx, req.Name, req.Description, 0)
	if err != nil {
		respondBizError(c, err)
		return
	}

	response.OK(c, newDemoResponse(demo))
}

// GetByID 获取 Demo 详情
// @Summary 获取 Demo
// @Tags demo
// @Produce json
// @Param id path int true "Demo ID"
// @Success 200 {object} DemoResponse
// @Router /api/demo/{id} [get]
func (s *DemoService) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Fail(c, errs.InvalidArgument("ID 格式错误"))
		return
	}

	// 领域错误（ErrDemoNotFound → 404）与服务端错误在此分流，
	// handler 里不再出现 if errors.Is(...) 分支。
	demo, err := s.demoBiz.GetByID(c.Request.Context(), uint(id))
	if err != nil {
		respondBizError(c, err)
		return
	}

	response.OK(c, newDemoResponse(demo))
}

// List 获取 Demo 列表
// @Summary 获取 Demo 列表
// @Tags demo
// @Produce json
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(10)
// @Success 200 {object} ListDemoResponse
// @Router /api/demo [get]
func (s *DemoService) List(c *gin.Context) {
	var req ListDemoRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, errs.InvalidArgument("请求参数错误"))
		return
	}

	// 缺省值与上限：未传时用默认值；上限由 DTO 的 binding(max=100) 保证
	page, pageSize := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	demos, total, err := s.demoBiz.List(c.Request.Context(), page, pageSize)
	if err != nil {
		respondBizError(c, err)
		return
	}

	responses := make([]*DemoResponse, 0, len(demos))
	for _, d := range demos {
		responses = append(responses, newDemoResponse(d))
	}

	// 分页结构由本模块的 DTO 定义，整体作为 data 返回（pkg/response 不规定业务结构）
	response.OK(c, ListDemoResponse{
		List:  responses,
		Total: total,
		Page:  page,
	})
}

// Update 更新 Demo
// @Summary 更新 Demo
// @Tags demo
// @Accept json
// @Produce json
// @Param id path int true "Demo ID"
// @Param request body UpdateDemoRequest true "更新请求"
// @Success 200 {object} DemoResponse
// @Router /api/demo/{id} [put]
func (s *DemoService) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Fail(c, errs.InvalidArgument("ID 格式错误"))
		return
	}

	var req UpdateDemoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.InvalidArgument("请求参数错误"))
		return
	}

	demo, err := s.demoBiz.Update(c.Request.Context(), uint(id), req.Name, req.Description)
	if err != nil {
		respondBizError(c, err)
		return
	}

	response.OK(c, newDemoResponse(demo))
}

// Delete 删除 Demo
// @Summary 删除 Demo
// @Tags demo
// @Produce json
// @Param id path int true "Demo ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/demo/{id} [delete]
func (s *DemoService) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Fail(c, errs.InvalidArgument("ID 格式错误"))
		return
	}

	if err := s.demoBiz.Delete(c.Request.Context(), uint(id)); err != nil {
		respondBizError(c, err)
		return
	}

	response.OK(c, gin.H{"message": "删除成功"})
}
