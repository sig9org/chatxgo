package notify

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// DefaultProfile is the profile used when LoadConfigFile is given an empty
// profile name.
const DefaultProfile = "default"

// LoadConfigFile parses the TOML file at path and returns the Config for the
// given profile (a TOML table). An empty profile means DefaultProfile.
// The config.toml file can hold several named profiles, e.g.:
//
//	[default]
//	WEBEX_DST = "..."
//
//	[work]
//	WEBEX_DST = "..."
//
// Recognized keys (same names as the historical .env variables):
//
//	WEBEX_TOKEN, WEBEX_DST
//	MSTEAMS_DST
//	SLACK_DST, SLACK_TOKEN, SLACK_CHANNEL
//	DISCORD_DST
//	PROXY
func LoadConfigFile(path, profile string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("notify: read config file %s: %w", path, err)
	}
	profiles := make(map[string]fileProfile)
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&profiles); err != nil {
		return Config{}, fmt.Errorf("notify: parse config file %s: %w", path, err)
	}
	selected, err := configProfile(profiles, profile)
	if err != nil {
		return Config{}, fmt.Errorf("notify: config file %s: %w", path, err)
	}
	return Config{
		Webex: WebexConfig{
			Token: selected.WebexToken,
			Dest:  selected.WebexDest,
		},
		Teams: TeamsConfig{
			Dest: selected.TeamsDest,
		},
		Slack: SlackConfig{
			Dest:    selected.SlackDest,
			Token:   selected.SlackToken,
			Channel: selected.SlackChannel,
		},
		Discord: DiscordConfig{
			Dest: selected.DiscordDest,
		},
		Proxy: selected.Proxy,
	}, nil
}

type fileProfile struct {
	Proxy        string `toml:"PROXY"`
	DiscordDest  string `toml:"DISCORD_DST"`
	TeamsDest    string `toml:"MSTEAMS_DST"`
	SlackDest    string `toml:"SLACK_DST"`
	SlackToken   string `toml:"SLACK_TOKEN"`
	SlackChannel string `toml:"SLACK_CHANNEL"`
	WebexToken   string `toml:"WEBEX_TOKEN"`
	WebexDest    string `toml:"WEBEX_DST"`
}

// configProfile picks the table holding the given profile's settings.
func configProfile(profiles map[string]fileProfile, profile string) (fileProfile, error) {
	if profile == "" {
		profile = DefaultProfile
	}
	for name, selected := range profiles {
		if strings.EqualFold(name, profile) {
			return selected, nil
		}
	}
	return fileProfile{}, fmt.Errorf("profile %q not found", profile)
}

// DefaultConfigPath resolves the config.toml path to use when none is given
// explicitly:
//
//  1. config.toml in the current working directory, if present.
//  2. Otherwise a per-user, OS-specific location:
//     - Linux/macOS: ~/.config/chatxgo/config.toml
//     - Windows:     %AppData%\chatxgo\config.toml
//
// The returned path is not guaranteed to exist; callers should os.Stat it
// before loading.
func DefaultConfigPath() (string, error) {
	if dir, err := os.Getwd(); err == nil {
		candidate := filepath.Join(dir, "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return userConfigPath()
}

func userConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		appData, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("notify: resolve user config dir: %w", err)
		}
		return windowsConfigPath(appData), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("notify: resolve home dir: %w", err)
	}
	return unixConfigPath(home), nil
}

func windowsConfigPath(appData string) string {
	return filepath.Join(appData, "chatxgo", "config.toml")
}

func unixConfigPath(home string) string {
	return filepath.Join(home, ".config", "chatxgo", "config.toml")
}
