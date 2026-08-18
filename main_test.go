package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sig9org/chatxgo/internal/version"
)

// writeConfig writes a TOML config file with the given body under dir and
// returns its path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFlagsAliases(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want func(c cli) bool
	}{
		{"update", []string{"-update"}, func(c cli) bool { return c.update }},
		{"version short", []string{"-v"}, func(c cli) bool { return c.showVersion }},
		{"version long", []string{"-version"}, func(c cli) bool { return c.showVersion }},
		{"help short", []string{"-h"}, func(c cli) bool { return c.showHelp }},
		{"help long", []string{"-help"}, func(c cli) bool { return c.showHelp }},
		{"debug", []string{"-debug"}, func(c cli) bool { return c.debug }},
		{"dryrun", []string{"-dryrun"}, func(c cli) bool { return c.dryrun }},
		{"silent", []string{"-silent"}, func(c cli) bool { return c.silent }},
		{"subject short", []string{"-s", "hi"}, func(c cli) bool { return c.subject == "hi" }},
		{"subject long", []string{"-subject", "hi"}, func(c cli) bool { return c.subject == "hi" }},
		{"proxy", []string{"-proxy", "http://proxy.example:8080"}, func(c cli) bool { return c.proxy == "http://proxy.example:8080" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c cli
			if _, err := parseFlags(tc.args, io.Discard, &c); err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			if !tc.want(c) {
				t.Errorf("flag %v not recognized: %+v", tc.args, c)
			}
		})
	}
}

func TestParseFlagsMentionsAndAttachments(t *testing.T) {
	var c cli
	args := []string{"-mention", "a,b:Bob", "-m", "c", "-attach", "x.png,y.png", "-a", "z.png"}
	if _, err := parseFlags(args, io.Discard, &c); err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	wantMentions := []string{"a", "b:Bob", "c"}
	if !equal(c.mentions.values, wantMentions) {
		t.Errorf("mentions = %v, want %v", c.mentions.values, wantMentions)
	}
	wantAttachments := []string{"x.png", "y.png", "z.png"}
	if !equal(c.attachments.values, wantAttachments) {
		t.Errorf("attachments = %v, want %v", c.attachments.values, wantAttachments)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), version.Name) {
		t.Errorf("help output missing tool name: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("help output missing usage section: %q", stdout.String())
	}
	wantOptions := `Options:
  -a, -attach value    File path or URL to attach (repeatable or comma-separated)
  -b, -body string     Message body, formatted as Markdown
      -config string   Config file path (default: ./config.toml, then the per-user config directory)
      -debug           Print verbose debug output
      -dryrun          Validate and report without sending
  -h, -help            Show usage information
  -m, -mention value   User mention: id or id:label (repeatable or comma-separated)
  -p, -profile string  Config profile to use (default: "default")
      -proxy string    HTTP(S) proxy URL (overrides PROXY in config.toml)
      -silent          Suppress normal output (-debug overrides this)
  -s, -subject string  Message subject/title
      -update          Update chatxgo to the latest release
  -v, -version         Show version information
`
	if !strings.Contains(stdout.String(), wantOptions) {
		t.Errorf("help options are not aligned and alphabetically ordered:\n%s", stdout.String())
	}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		if len(line) > 100 {
			t.Errorf("help line is %d columns, want at most 100: %q", len(line), line)
		}
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("one two three four", 9)
	want := []string{"one two", "three", "four"}
	if !equal(got, want) {
		t.Errorf("wrapText = %q, want %q", got, want)
	}
}

func TestRunVersion(t *testing.T) {
	orig := version.Version
	version.Version = "v0.0.4"
	defer func() { version.Version = orig }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-v"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got, want := stdout.String(), "chatxgo v0.0.4\n"; got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
}

func TestRunNoContentFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", filepath.Join(dir, "missing.ini")}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "message has neither subject nor body") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunSendsToEnabledTools(t *testing.T) {
	var webexHits, teamsHits, slackHits, discordHits int
	webex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webexHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer webex.Close()
	teams := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		teamsHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer teams.Close()
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordHits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discord.Close()

	// The webex sender hard-codes the real API endpoint, so this test
	// only exercises the tools whose destination is fully config-driven
	// (Teams, Slack, Discord) end-to-end through the CLI. Webex's own HTTP
	// behavior is covered in notify/webex_test.go.
	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nMSTEAMS_DST = %q\nSLACK_DST = %q\nDISCORD_DST = %q\n",
		teams.URL, slack.URL, discord.URL))

	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-subject", "hi", "-body", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if teamsHits != 1 {
		t.Errorf("teams hits = %d, want 1", teamsHits)
	}
	if slackHits != 1 {
		t.Errorf("slack hits = %d, want 1", slackHits)
	}
	if discordHits != 1 {
		t.Errorf("discord hits = %d, want 1", discordHits)
	}
	if webexHits != 0 {
		t.Errorf("webex should not have been called, hits = %d", webexHits)
	}
	out := stdout.String()
	if !strings.Contains(out, "Sent to Microsoft Teams") || !strings.Contains(out, "Sent to Slack") ||
		!strings.Contains(out, "Sent to Discord") {
		t.Errorf("unexpected stdout: %q", out)
	}
}

func TestRunLoadsConfigFile(t *testing.T) {
	var slackHits int
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", slack.URL))

	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-body", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if slackHits != 1 {
		t.Errorf("slack hits = %d, want 1", slackHits)
	}
}

func TestRunSelectsProfile(t *testing.T) {
	var defaultHits, workHits int
	defaultSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultSrv.Close()
	workSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer workSrv.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n\n[work]\nSLACK_DST = %q\n",
		defaultSrv.URL, workSrv.URL))

	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-profile", "work", "-body", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if workHits != 1 {
		t.Errorf("work profile hits = %d, want 1", workHits)
	}
	if defaultHits != 0 {
		t.Errorf("default profile should not have been used, hits = %d", defaultHits)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-config", configPath, "-p", "missing", "-body", "hello"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for unknown profile, stderr = %q", code, stderr.String())
	}
}

// TestRunDefaultConfigPathMissingDisablesTools verifies that when no
// -config flag is given and no config.toml can be found (neither in the
// current directory nor in the per-user config directory), every chat
// tool stays disabled rather than falling back to real process
// environment variables.
func TestRunDefaultConfigPathMissingDisablesTools(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	} else {
		t.Setenv("HOME", home)
	}
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"-body", "hi"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no chat tool is enabled") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

// TestRunDefaultConfigPathUserDir verifies that, absent a -config flag and
// a config.toml in the current directory, the per-user config directory
// (~/.config/chatxgo/config.toml on Linux/macOS) is used as a fallback.
func TestRunDefaultConfigPathUserDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("per-user config dir resolution differs on windows; covered in notify/configfile_test.go")
	}

	var slackHits int
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()

	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "chatxgo")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, cfgDir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", slack.URL))

	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if slackHits != 1 {
		t.Errorf("slack hits = %d, want 1", slackHits)
	}
}

// TestRunDefaultConfigPathCurrentDir verifies that, absent a -config flag,
// a config.toml in the current directory takes priority over the per-user
// config directory.
func TestRunDefaultConfigPathCurrentDir(t *testing.T) {
	var currentDirHits, homeHits int
	currentDirSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentDirHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer currentDirSrv.Close()
	homeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		homeHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer homeSrv.Close()

	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "chatxgo")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, cfgDir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", homeSrv.URL))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	} else {
		t.Setenv("HOME", home)
	}

	dir := t.TempDir()
	writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", currentDirSrv.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if currentDirHits != 1 {
		t.Errorf("current-directory config hits = %d, want 1", currentDirHits)
	}
	if homeHits != 0 {
		t.Errorf("per-user config should not have been used, hits = %d", homeHits)
	}
}

func TestRunInvalidMention(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", filepath.Join(dir, "missing.ini"), "-body", "hi", "-mention", ":no-id"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2, stderr = %q", code, stderr.String())
	}
}

func TestRunDryRunDoesNotSend(t *testing.T) {
	var slackHits int
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", slack.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-dryrun", "-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if slackHits != 0 {
		t.Errorf("dry run should not send, slack hits = %d", slackHits)
	}
	if !strings.Contains(stdout.String(), "Would send to Slack") {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunDryRunNoRecipientsEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", filepath.Join(dir, "missing.ini"), "-dryrun", "-body", "hi"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no chat tool is enabled") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunSilentSuppressesStdout(t *testing.T) {
	var slackHits int
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", slack.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-silent", "-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if slackHits != 1 {
		t.Errorf("slack hits = %d, want 1", slackHits)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout output with -silent, got %q", stdout.String())
	}
}

func TestRunSilentOverriddenByDebug(t *testing.T) {
	var slackHits int
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slackHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer slack.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\n", slack.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-silent", "-debug", "-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Sent to Slack") {
		t.Errorf("-debug should override -silent, stdout: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[debug]") {
		t.Errorf("expected debug output, stdout: %q", stdout.String())
	}
}

func TestRunProxyFromConfigFile(t *testing.T) {
	var destHits, proxyHits int
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer dest.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	dir := t.TempDir()
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\nPROXY = %q\n", dest.URL, proxy.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if proxyHits != 1 {
		t.Errorf("proxy hits = %d, want 1", proxyHits)
	}
	if destHits != 0 {
		t.Errorf("destination hits = %d, want 0 (request should have gone through the proxy)", destHits)
	}
}

func TestRunProxyFlagOverridesConfigFile(t *testing.T) {
	var configuredProxyHits, flagProxyHits int
	configuredProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configuredProxyHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer configuredProxy.Close()
	flagProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flagProxyHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer flagProxy.Close()

	dir := t.TempDir()
	// A plain http:// (not https://) placeholder destination: with a proxy
	// configured, the request is sent to the proxy in absolute-URI form
	// without the transport ever dialing this host directly, so it need
	// not resolve or accept connections.
	configPath := writeConfig(t, dir, fmt.Sprintf("[default]\nSLACK_DST = %q\nPROXY = %q\n",
		"http://elsewhere.invalid", configuredProxy.URL))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-proxy", flagProxy.URL, "-body", "hi"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if flagProxyHits != 1 {
		t.Errorf("-proxy flag's proxy hits = %d, want 1", flagProxyHits)
	}
	if configuredProxyHits != 0 {
		t.Errorf("config file's proxy hits = %d, want 0 (the -proxy flag should win)", configuredProxyHits)
	}
}

func TestRunInvalidProxyFails(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, "[default]\nSLACK_DST = \"https://example.invalid\"\n")
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", configPath, "-proxy", "://bad", "-body", "hi"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid proxy URL") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunNoRecipientsEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", filepath.Join(dir, "missing.ini"), "-body", "hi"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no chat tool is enabled") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}
