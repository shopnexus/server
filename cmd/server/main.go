package main

import (
	"shopnexus-server/internal/app"

	"go.uber.org/fx"
)

func main() {
	fx.New(app.Module).Run()
}
