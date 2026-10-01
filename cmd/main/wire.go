//go:build wireinject
// +build wireinject

package main

import (
	"log/slog"

	"gin-template/internal"
	"gin-template/internal/conf"
	"gin-template/pkg/infra"

	"github.com/google/wire"
)

func initApp(cfg *conf.Bootstrap, logger *slog.Logger) *internal.MainApp {
	panic(wire.Build(
		internal.NewMainApp,
		infra.ProviderSet,
		internal.ProviderSet,
	))
}
