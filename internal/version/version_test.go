package version

import (
	"testing"
)

func TestString(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "v1.2.3"
	got := String()
	if want := "chatxgo v1.2.3"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
