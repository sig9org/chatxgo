package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "v1.2.3"
	got := String()
	if !strings.Contains(got, Name) || !strings.Contains(got, "v1.2.3") {
		t.Errorf("String() = %q, want it to contain %q and %q", got, Name, "v1.2.3")
	}
}
