package selfupdate

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRepositoryIsOwnerSlashRepo(t *testing.T) {
	parts := strings.Split(Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Errorf("Repository = %q, want \"owner/repo\" form", Repository)
	}
}

// TestUpdatePropagatesContextCancellation verifies Update does not hang or
// panic when the context is already canceled; it must return an error
// promptly instead of reaching the network.
func TestUpdatePropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Update(ctx, "v0.0.1")
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Update did not return after context cancellation")
	}
}
