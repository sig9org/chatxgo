package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFileFlat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	content := `MSTEAMS_DST=

SLACK_DST="https://example.com/slack"
SLACK_TOKEN="xoxb-token"
SLACK_CHANNEL="C123"

WEBEX_TOKEN="webex-token"
WEBEX_DST="room-id"

PROXY="http://proxy.example:8080"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(path, "")
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}

	if cfg.Webex.Token != "webex-token" || cfg.Webex.Dest != "room-id" {
		t.Errorf("webex = %+v", cfg.Webex)
	}
	if cfg.Teams.Dest != "" {
		t.Errorf("teams = %+v", cfg.Teams)
	}
	if cfg.Slack.Dest != "https://example.com/slack" ||
		cfg.Slack.Token != "xoxb-token" || cfg.Slack.Channel != "C123" {
		t.Errorf("slack = %+v", cfg.Slack)
	}
	if cfg.Proxy != "http://proxy.example:8080" {
		t.Errorf("proxy = %q", cfg.Proxy)
	}
}

func TestLoadConfigFileProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	content := `[default]
WEBEX_TOKEN="webex-token"
WEBEX_DST="room-id"

[work]
MSTEAMS_DST="https://example.com/teams"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(path, "")
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Webex.Token != "webex-token" || cfg.Webex.Dest != "room-id" {
		t.Errorf("default profile webex = %+v", cfg.Webex)
	}
	if cfg.Teams.Dest != "" {
		t.Errorf("default profile teams = %+v, want empty (belongs to [work])", cfg.Teams)
	}

	cfg, err = LoadConfigFile(path, "work")
	if err != nil {
		t.Fatalf("LoadConfigFile(work): %v", err)
	}
	if cfg.Teams.Dest != "https://example.com/teams" {
		t.Errorf("work profile teams = %+v", cfg.Teams)
	}
	if cfg.Webex.Dest != "" {
		t.Errorf("work profile webex = %+v, want empty (belongs to [default])", cfg.Webex)
	}

	// Case-insensitive profile match.
	cfg, err = LoadConfigFile(path, "WORK")
	if err != nil {
		t.Fatalf("LoadConfigFile(WORK): %v", err)
	}
	if cfg.Teams.Dest != "https://example.com/teams" {
		t.Errorf("WORK profile teams = %+v", cfg.Teams)
	}

	if _, err := LoadConfigFile(path, "missing"); err == nil {
		t.Error("expected an error for an unknown profile")
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	if _, err := LoadConfigFile(filepath.Join(t.TempDir(), "missing.ini"), ""); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestCleanValue(t *testing.T) {
	cases := map[string]string{
		`"quoted"`:    "quoted",
		`'quoted'`:    "quoted",
		"unquoted":    "unquoted",
		`  "spaced" `: "spaced",
		``:            "",
		`"`:           `"`,
	}
	for in, want := range cases {
		if got := cleanValue(in); got != want {
			t.Errorf("cleanValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnixAndWindowsConfigPath(t *testing.T) {
	if got, want := unixConfigPath("/home/alice"), filepath.Join("/home/alice", ".config", "chatxgo", "config.ini"); got != want {
		t.Errorf("unixConfigPath = %q, want %q", got, want)
	}
	if got, want := windowsConfigPath(`C:\Users\alice\AppData\Roaming`), filepath.Join(`C:\Users\alice\AppData\Roaming`, "chatxgo", "config.ini"); got != want {
		t.Errorf("windowsConfigPath = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathPrefersCurrentDir(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(local, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if got != local {
		t.Errorf("DefaultConfigPath = %q, want %q", got, local)
	}
}

func TestDefaultConfigPathFallsBackWhenCurrentDirHasNoConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	want, _ := userConfigPath()
	if got != want {
		t.Errorf("DefaultConfigPath = %q, want %q", got, want)
	}
}
