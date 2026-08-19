package main

import (
	"os"

	appworker "github.com/torgnexa/torgnexa/internal/app/worker"
	"github.com/torgnexa/torgnexa/internal/platform/bootstrap"
	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func main() {
	if err := bootstrap.Run(config.ServiceWorker, appworker.Run); err != nil {
		os.Exit(1)
	}
}
