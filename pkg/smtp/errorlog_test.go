package smtp

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorLogLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"error handling 91.237.124.252:63799: read tcp 10.42.0.134:25->91.237.124.252:63799: read: connection reset by peer": slog.LevelDebug,
		"error handling 1.2.3.4:5: EOF":                      slog.LevelDebug,
		"panic serving 1.2.3.4:5: boom\nstack":               slog.LevelError,
		"accept error: too many open files; retrying in 5ms": slog.LevelWarn,
	}
	for detail, want := range cases {
		assert.Equal(t, want, levelOf(detail), detail)
	}
}
