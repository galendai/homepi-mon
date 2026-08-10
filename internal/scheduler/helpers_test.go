package scheduler_test

import (
	"io"
	"log/slog"
)

// quietLogger discards scheduler diagnostics so failure-path tests do not spam
// the test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
