package main

import (
	"context"
	"os"

	"github.com/stoicsoft/vpsbox/internal/app"
)

func main() {
	ctx := context.Background()
	if err := app.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
