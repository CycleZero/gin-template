// Package data 是 demo 模块的数据访问层（DDD 的 infrastructure/repository 层）。
//
// 职责：
//   - 定义数据库模型（PO，demo.go 的 Demo）
//   - 实现 biz 层声明的仓储接口（DemoRepo），并在 PO ↔ 领域模型之间转换
//
// 约束：可以 import biz（实现其接口、产出其领域模型），但不能反过来——
// biz 不 import data，依赖方向单向，无循环导入。
package data

import "gorm.io/gorm"

// Demo 是 demo 表的**数据库模型（PO）**。
//
// 只在 data 层内部使用：对外的数据形态是 biz.Demo，
// 二者的转换集中在 repo.go 的 toBiz / applyBiz。
type Demo struct {
	gorm.Model
	Name        string `gorm:"type:varchar(100);not null;comment:名称" json:"name"`
	Description string `gorm:"type:text;comment:描述" json:"description"`
	Status      int    `gorm:"type:tinyint;default:1;comment:状态 1=正常 0=禁用" json:"status"`
	CreatedBy   uint   `gorm:"comment:创建人ID" json:"created_by"`
}

// TableName 固定表名，避免依赖 GORM 的复数化规则（未来改类型名不会换表）。
func (Demo) TableName() string { return "demo" }
