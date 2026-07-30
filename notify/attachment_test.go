package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/a.png": true,
		"http://example.com/a.png":  true,
		"/tmp/a.png":                false,
		"a.png":                     false,
		"ftp://example.com/a.png":   false,
	}
	for in, want := range cases {
		if got := isURL(in); got != want {
			t.Errorf("isURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestReadLocalAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.png")
	if err := os.WriteFile(path, []byte("fake-png-bytes"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	att, err := readLocalAttachment(path)
	if err != nil {
		t.Fatalf("readLocalAttachment: %v", err)
	}
	if att.Name != "report.png" {
		t.Errorf("Name = %q, want %q", att.Name, "report.png")
	}
	if att.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want %q", att.ContentType, "image/png")
	}
	if string(att.Data) != "fake-png-bytes" {
		t.Errorf("Data = %q, want %q", att.Data, "fake-png-bytes")
	}
}

func TestReadLocalAttachmentMissing(t *testing.T) {
	if _, err := readLocalAttachment("/no/such/file"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestAttachmentName(t *testing.T) {
	if got := attachmentName("https://example.com/path/to/report.pdf"); got != "report.pdf" {
		t.Errorf("attachmentName(url) = %q, want %q", got, "report.pdf")
	}
	if got := attachmentName("/local/dir/report.pdf"); got != "report.pdf" {
		t.Errorf("attachmentName(path) = %q, want %q", got, "report.pdf")
	}
}

func TestFormatAttachmentLine(t *testing.T) {
	if got := formatAttachmentLine("https://example.com/report.pdf"); got != "- [report.pdf](https://example.com/report.pdf)" {
		t.Errorf("unexpected markdown: %q", got)
	}
	if got := formatAttachmentLine("/local/report.pdf"); got != "- report.pdf (/local/report.pdf)" {
		t.Errorf("unexpected markdown: %q", got)
	}
}
