package data

import "github.com/google/wire"

// ProviderSet data 层的 Wire ProviderSet。
var ProviderSet = wire.NewSet(NewDemoRepo)
