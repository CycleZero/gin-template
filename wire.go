//go:build wireinject
// +build wireinject

package main

import (
	"gin-template/internal"
	"gin-template/pkg/infra"
	"gin-template/pkg/log"

	"github.com/google/wire"
	"github.com/spf13/viper"
)

func initApp(vc *viper.Viper, logger *log.Logger) *MainApp {
	panic(wire.Build(
		NewMainApp,
		infra.ProviderSet,
		internal.ProviderSet,
	))
}
