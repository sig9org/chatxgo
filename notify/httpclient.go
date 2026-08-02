package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// proxyHTTPClient returns the *http.Client every enabled Sender should use.
// A blank proxy returns http.DefaultClient (requests sent directly).
// Otherwise proxy must be an absolute URL (e.g.
// "http://user:pass@proxy.example:8080"); the returned client routes every
// request through it.
func proxyHTTPClient(proxy string) (*http.Client, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return http.DefaultClient, nil
	}
	u, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("notify: invalid proxy URL %q: %w", proxy, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("notify: invalid proxy URL %q: missing scheme or host", proxy)
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}, nil
}
