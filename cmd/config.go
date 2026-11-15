package cmd

import (
	"log/slog"
	"os"
)

// logLevel is the dynamic level for the slog default handler installed at
// package init. Start raises it to Debug when the config file sets `debug`.
var logLevel = new(slog.LevelVar)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
}
