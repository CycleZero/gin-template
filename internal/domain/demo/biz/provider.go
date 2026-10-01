package biz

import "github.com/google/wire"

// ProviderSet biz 层的 Wire ProviderSet。
var ProviderSet = wire.NewSet(NewDemoBiz)
