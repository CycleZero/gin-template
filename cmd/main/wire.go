//go:build wireinject
// +build wireinject

package main

import (
	"log/slog"

	"gin-template/conf"
	"gin-template/internal"
	"gin-template/pkg/infra"

	"github.com/google/wire"
)

func initApp(cfg *conf.Config, logger *slog.Logger) *internal.MainApp {
	panic(wire.Build(
		internal.NewMainApp,
		infra.ProviderSet,
		internal.ProviderSet,
	))
}
