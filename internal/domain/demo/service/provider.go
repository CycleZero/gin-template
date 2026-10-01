package service

import "github.com/google/wire"

// ProviderSet service 层的 Wire ProviderSet。
var ProviderSet = wire.NewSet(NewDemoService)
