package main

import (
	"os"

	"github.com/torgnexa/torgnexa/internal/platform/background"
	"github.com/torgnexa/torgnexa/internal/platform/bootstrap"
	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func main() {
	if err := bootstrap.Run(config.ServiceScheduler, background.RunScheduler); err != nil {
		os.Exit(1)
	}
}
