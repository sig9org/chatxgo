// Package debugx provides a process-wide toggle for verbose debug logging.
package debugx

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/sig9org/chatxgo/internal/colorx"
)

var enabled atomic.Bool

// Writer is where debug output is written. Overridable in tests.
var Writer io.Writer = os.Stdout

// Enable turns debug logging on or off.
func Enable(v bool) {
	enabled.Store(v)
}

// Enabled reports whether debug logging is currently on.
func Enabled() bool {
	return enabled.Load()
}

// Printf writes a debug line to Writer when debug logging is enabled.
func Printf(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	line := fmt.Sprintf("[debug] "+time.Now().Format("2006-01-02T15:04:05.000Z07:00")+" "+format, args...)
	fmt.Fprintln(Writer, colorx.Gray(line))
}
