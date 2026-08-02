// Package colorx wraps CLI output lines in the ANSI colors chatxgo uses to
// distinguish message severity: warnings in orange, errors in red, and debug
// output in gray. Normal messages are left uncolored.
package colorx

const (
	reset  = "\x1b[0m"
	red    = "\x1b[31m"
	orange = "\x1b[38;5;208m"
	gray   = "\x1b[90m"
)

// Red wraps s for an error message.
func Red(s string) string { return red + s + reset }

// Orange wraps s for a warning message.
func Orange(s string) string { return orange + s + reset }

// Gray wraps s for a debug message.
func Gray(s string) string { return gray + s + reset }
