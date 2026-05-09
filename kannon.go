package main

import (
	"log/slog"
	"os"

	"github.com/kannon-email/kannon/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		slog.Error("Error", "err", err)
		os.Exit(1)
	}
}
