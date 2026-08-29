package selfupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRepositoryIsOwnerSlashRepo(t *testing.T) {
	parts := strings.Split(Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Errorf("Repository = %q, want owner/repo form", Repository)
	}
}

func TestUpdaterDownloadsValidatesAndReplacesLatestRelease(t *testing.T) {
	binary := []byte("new binary")
	assetName := "chatxgo_v1.2.3_linux_amd64"
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repository + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.3","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`, assetName, server.URL+"/asset", server.URL+"/checksums")
		case "/asset":
			_, _ = w.Write(binary)
		case "/checksums":
			_, _ = fmt.Fprintf(w, "%s  %s\n", digest, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target, err := os.CreateTemp(t.TempDir(), "chatxgo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.WriteString("old binary"); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}

	u := testUpdater(server, target.Name())
	message, err := u.update(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if message != "updated to version v1.2.3" {
		t.Errorf("message = %q", message)
	}
	got, err := os.ReadFile(target.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Errorf("updated data = %q, want %q", got, binary)
	}
}

func TestUpdaterRejectsChecksumMismatchWithoutReplacing(t *testing.T) {
	assetName := "chatxgo_v1.2.3_linux_amd64"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repository + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.3","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`, assetName, server.URL+"/asset", server.URL+"/checksums")
		case "/asset":
			_, _ = io.WriteString(w, "tampered binary")
		case "/checksums":
			_, _ = io.WriteString(w, strings.Repeat("0", 64)+"  "+assetName+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target, err := os.CreateTemp(t.TempDir(), "chatxgo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.WriteString("original binary"); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = testUpdater(server, target.Name()).update(context.Background(), "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
	got, err := os.ReadFile(target.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original binary" {
		t.Errorf("target changed after checksum failure: %q", got)
	}
}

func TestUpdaterSkipsCurrentRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+Repository+"/releases/latest" {
			_, _ = io.WriteString(w, `{"tag_name":"v1.2.3","assets":[]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	target, err := os.CreateTemp(t.TempDir(), "chatxgo")
	if err != nil {
		t.Fatal(err)
	}
	_ = target.Close()

	message, err := testUpdater(server, target.Name()).update(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if want := "already up to date (current v1.2.3, latest v1.2.3)"; message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
}

func testUpdater(server *httptest.Server, target string) updater {
	client := server.Client()
	baseTransport := client.Transport
	client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			clone := r.Clone(r.Context())
			clone.URL.Scheme = "http"
			clone.URL.Host = strings.TrimPrefix(server.URL, "http://")
			r = clone
		}
		return baseTransport.RoundTrip(r)
	})
	return updater{client: client, goos: "linux", goarch: "amd64", executablePath: func() (string, error) { return target, nil }}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
