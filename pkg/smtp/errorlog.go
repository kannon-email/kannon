package smtp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// noisyConnErrors are failures of a single inbound connection that say nothing about Kannon: port 25
// is open to the internet, and scanners and impatient clients drop connections all day.
var noisyConnErrors = []string{
	"connection reset by peer",
	"broken pipe",
	"i/o timeout",
	"EOF",
	"use of closed network connection",
	"tls: ",
}

// errorLog routes go-smtp's own logger, which otherwise writes unstructured lines straight to stderr,
// through slog: per-connection noise at debug, a panic in a session at error, anything else at warn.
type errorLog struct {
	log *slog.Logger
}

func newErrorLog() errorLog {
	return errorLog{log: slog.With("component", "smtp")}
}

func (l errorLog) Printf(format string, v ...any) {
	l.write(fmt.Sprintf(format, v...))
}

func (l errorLog) Println(v ...any) {
	l.write(strings.TrimSuffix(fmt.Sprintln(v...), "\n"))
}

func (l errorLog) write(detail string) {
	l.log.Log(context.Background(), levelOf(detail), "SMTP server error", "detail", detail)
}

func levelOf(detail string) slog.Level {
	if strings.HasPrefix(detail, "panic serving") {
		return slog.LevelError
	}
	for _, noise := range noisyConnErrors {
		if strings.Contains(detail, noise) {
			return slog.LevelDebug
		}
	}
	return slog.LevelWarn
}
