//go:build wireinject
// +build wireinject

package main

import (
	"log/slog"

	"gin-template/internal"
	"gin-template/pkg/infra"

	"github.com/google/wire"
	"github.com/spf13/viper"
)

func initApp(vc *viper.Viper, logger *slog.Logger) *MainApp {
	panic(wire.Build(
		NewMainApp,
		infra.ProviderSet,
		internal.ProviderSet,
	))
}
