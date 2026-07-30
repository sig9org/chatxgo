package notify

import (
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// isURL reports whether ref points to a remote resource rather than a
// local file path.
func isURL(ref string) bool {
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// localAttachment holds the bytes and metadata of a file attachment read
// from disk, ready to be uploaded to a chat tool.
type localAttachment struct {
	Name        string
	ContentType string
	Data        []byte
}

func readLocalAttachment(path string) (localAttachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return localAttachment{}, fmt.Errorf("notify: read attachment %q: %w", path, err)
	}
	name := filepath.Base(path)
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return localAttachment{Name: name, ContentType: ct, Data: data}, nil
}

// attachmentName returns the display name for an attachment reference,
// whether it is a local path or a URL.
func attachmentName(ref string) string {
	if isURL(ref) {
		u, err := url.Parse(ref)
		if err == nil {
			if base := filepath.Base(u.Path); base != "." && base != "/" {
				return base
			}
		}
		return ref
	}
	return filepath.Base(ref)
}

// formatAttachmentLine renders a single attachment reference as a Markdown
// bullet, used by tools that cannot upload the file directly.
func formatAttachmentLine(ref string) string {
	name := attachmentName(ref)
	if isURL(ref) {
		return fmt.Sprintf("- [%s](%s)", name, ref)
	}
	return fmt.Sprintf("- %s (%s)", name, strings.TrimSpace(ref))
}
