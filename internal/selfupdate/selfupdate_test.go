package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestReleaseAssetName(t *testing.T) {
	cases := []struct {
		goos string
		want string
	}{
		{goos: "linux", want: "chatxgo_v1.2.3_linux_amd64"},
		{goos: "windows", want: "chatxgo_v1.2.3_windows_amd64.exe"},
	}
	for _, tc := range cases {
		if got := releaseAssetName("v1.2.3", tc.goos, "amd64"); got != tc.want {
			t.Errorf("releaseAssetName = %q, want %q", got, tc.want)
		}
	}
}

func TestUpdaterDownloadsAndAppliesLatestRelease(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repository + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.3","assets":[{"name":"chatxgo_v1.2.3_linux_amd64","browser_download_url":%q}]}`, server.URL+"/asset")
		case "/asset":
			_, _ = io.WriteString(w, "new binary")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var applied bytes.Buffer
	u := updater{
		client:  server.Client(),
		apiBase: server.URL,
		goos:    "linux",
		goarch:  "amd64",
		executablePath: func() (string, error) {
			return "/tmp/chatxgo", nil
		},
		apply: func(source io.Reader, target string) error {
			if target != "/tmp/chatxgo" {
				t.Errorf("target = %q, want /tmp/chatxgo", target)
			}
			_, err := io.Copy(&applied, source)
			return err
		},
	}

	message, err := u.update(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if message != "updated to version v1.2.3" {
		t.Errorf("message = %q", message)
	}
	if applied.String() != "new binary" {
		t.Errorf("applied data = %q", applied.String())
	}
}

func TestUpdaterSkipsCurrentRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v1.2.3","assets":[{"name":"chatxgo_v1.2.3_linux_amd64","browser_download_url":"https://example.invalid/asset"}]}`)
	}))
	defer server.Close()

	u := updater{
		client:  server.Client(),
		apiBase: server.URL,
		goos:    "linux",
		goarch:  "amd64",
		executablePath: func() (string, error) {
			t.Fatal("executablePath should not be called")
			return "", nil
		},
		apply: func(io.Reader, string) error {
			t.Fatal("apply should not be called")
			return nil
		},
	}

	message, err := u.update(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if want := "already up to date (current v1.2.3, latest v1.2.3)"; message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
}

func TestUpdaterReportsMissingPlatformAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v1.2.3","assets":[]}`)
	}))
	defer server.Close()

	u := updater{client: server.Client(), apiBase: server.URL, goos: "linux", goarch: "arm64"}
	_, err := u.update(context.Background(), "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "no release asset") {
		t.Errorf("error = %v, want missing asset error", err)
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
