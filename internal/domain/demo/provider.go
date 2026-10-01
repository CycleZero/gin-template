// Package demo 聚合 demo 业务模块各层（service / biz / data）的 Wire ProviderSet。
//
// 模块内按层分目录，依赖方向单向向下：
//
//	service/  HTTP 层：请求解析、校验、响应格式化，只依赖 biz
//	biz/      业务层：业务规则与流程编排，只依赖 data 与 model
//	data/     数据层：GORM 操作，只依赖 pkg/infra 与 model
//
// 因此不会出现循环导入，各层也可独立复用于其它传输层（gRPC、定时任务等）。
package demo

import (
	"gin-template/internal/domain/demo/biz"
	"gin-template/internal/domain/demo/data"
	"gin-template/internal/domain/demo/service"

	"github.com/google/wire"
)

// ProviderSet demo 模块的 Wire ProviderSet（按层聚合）。
var ProviderSet = wire.NewSet(
	data.ProviderSet,
	biz.ProviderSet,
	service.ProviderSet,
)
