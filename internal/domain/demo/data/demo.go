package data

import "gorm.io/gorm"

// Demo 示例模型（数据层 PO）- 展示 GORM 模型定义。
//
// 模型定义在所属业务模块的 data 层，与 repo.go 同包：仓储直接使用，无需单独的
// model 包；其它层需要引用时写 data.Demo。
//
// 替换或删除此模型以定义你自己的业务模型。
type Demo struct {
	gorm.Model
	Name        string `gorm:"type:varchar(100);not null;comment:名称" json:"name"`
	Description string `gorm:"type:text;comment:描述" json:"description"`
	Status      int    `gorm:"type:tinyint;default:1;comment:状态 1=正常 0=禁用" json:"status"`
	CreatedBy   uint   `gorm:"comment:创建人ID" json:"created_by"`
}
