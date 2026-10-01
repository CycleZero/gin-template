// Package service 是 demo 模块的 HTTP 层（DDD 的 interface/transport 层）。
//
// 职责：
//   - 解析请求、校验参数（DTO 定义见 dto.go）
//   - 调用 biz 用例，并把 biz 领域模型转换为响应 DTO（转换函数也在 dto.go）
//   - 把领域错误映射为 HTTP 状态码
//
// 约束：只依赖 biz，**不 import data**——HTTP 层不会泄漏数据库模型。
package service

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"gin-template/internal/domain/demo/biz"

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
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	demo, err := s.demoBiz.Create(ctx, req.Name, req.Description, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, newDemoResponse(demo))
}

// GetByID 获取 Demo 详情
// @Summary 获取 Demo
// @Tags demo
// @Produce json
// @Param id path int true "Demo ID"
// @Success 200 {object} DemoResponse
// @Router /api/demo/{id} [get]
func (s *DemoService) GetByID(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	demo, err := s.demoBiz.GetByID(ctx, uint(id))
	if err != nil {
		// 领域错误 → HTTP 状态码的映射只发生在 service 层
		if errors.Is(err, biz.ErrDemoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, newDemoResponse(demo))
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
	ctx := c.Request.Context()

	var req ListDemoRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
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

	demos, total, err := s.demoBiz.List(ctx, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, newListDemoResponse(demos, total, page))
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
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	var req UpdateDemoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	demo, err := s.demoBiz.Update(ctx, uint(id), req.Name, req.Description)
	if err != nil {
		if errors.Is(err, biz.ErrDemoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, newDemoResponse(demo))
}

// Delete 删除 Demo
// @Summary 删除 Demo
// @Tags demo
// @Produce json
// @Param id path int true "Demo ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/demo/{id} [delete]
func (s *DemoService) Delete(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID 格式错误"})
		return
	}

	if err := s.demoBiz.Delete(ctx, uint(id)); err != nil {
		if errors.Is(err, biz.ErrDemoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
