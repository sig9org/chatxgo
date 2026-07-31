package main

import (
	"bytes"
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

// writeConfig writes an INI config file with the given body under dir and
// returns its path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.ini")
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
		{"update short", []string{"-u"}, func(c cli) bool { return c.update }},
		{"update long", []string{"-update"}, func(c cli) bool { return c.update }},
		{"version short", []string{"-v"}, func(c cli) bool { return c.showVersion }},
		{"version long", []string{"-version"}, func(c cli) bool { return c.showVersion }},
		{"help short", []string{"-h"}, func(c cli) bool { return c.showHelp }},
		{"help long", []string{"-help"}, func(c cli) bool { return c.showHelp }},
		{"debug", []string{"-debug"}, func(c cli) bool { return c.debug }},
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
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-v"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), version.Name) {
		t.Errorf("version output missing tool name: %q", stdout.String())
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
	var webexHits, teamsHits, slackHits int
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

	// The webex sender hard-codes the real API endpoint, so this test
	// only exercises the tools whose destination is fully config-driven
	// (Teams, Slack) end-to-end through the CLI. Webex's own HTTP
	// behavior is covered in notify/webex_test.go.
	dir := t.TempDir()
	configPath := writeConfig(t, dir, "MSTEAMS_DST="+teams.URL+"\n"+
		"SLACK_DST="+slack.URL+"\n")

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
	if webexHits != 0 {
		t.Errorf("webex should not have been called, hits = %d", webexHits)
	}
	out := stdout.String()
	if !strings.Contains(out, "Sent to Microsoft Teams") || !strings.Contains(out, "Sent to Slack") {
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
	configPath := writeConfig(t, dir, "SLACK_DST="+slack.URL+"\n")

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
	configPath := writeConfig(t, dir, "[default]\nSLACK_DST="+defaultSrv.URL+"\n\n"+
		"[work]\nSLACK_DST="+workSrv.URL+"\n")

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
// -config flag is given and no config.ini can be found (neither in the
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
// a config.ini in the current directory, the per-user config directory
// (~/.config/chatxgo/config.ini on Linux/macOS) is used as a fallback.
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
	writeConfig(t, cfgDir, "SLACK_DST="+slack.URL+"\n")

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
// a config.ini in the current directory takes priority over the per-user
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
	writeConfig(t, cfgDir, "SLACK_DST="+homeSrv.URL+"\n")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	} else {
		t.Setenv("HOME", home)
	}

	dir := t.TempDir()
	writeConfig(t, dir, "SLACK_DST="+currentDirSrv.URL+"\n")
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
