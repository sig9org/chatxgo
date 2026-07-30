package debugx

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintfRespectsEnable(t *testing.T) {
	var buf bytes.Buffer
	orig := Writer
	Writer = &buf
	defer func() { Writer = orig; Enable(false) }()

	Enable(false)
	Printf("hidden %d", 1)
	if buf.Len() != 0 {
		t.Errorf("expected no output while disabled, got %q", buf.String())
	}

	Enable(true)
	Printf("shown %d", 2)
	if !strings.Contains(buf.String(), "shown 2") {
		t.Errorf("expected output to contain %q, got %q", "shown 2", buf.String())
	}
}

func TestEnabled(t *testing.T) {
	defer Enable(false)
	Enable(true)
	if !Enabled() {
		t.Error("Enabled() = false after Enable(true)")
	}
	Enable(false)
	if Enabled() {
		t.Error("Enabled() = true after Enable(false)")
	}
}
