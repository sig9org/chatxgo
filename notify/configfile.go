package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/ini.v1"
)

// DefaultProfile is the profile used when LoadConfigFile is given an empty
// profile name.
const DefaultProfile = "default"

// LoadConfigFile parses the INI file at path and returns the Config for the
// given profile (an INI section). An empty profile means DefaultProfile.
// The config.ini file can hold several named profiles, e.g.:
//
//	[default]
//	WEBEX_DST=...
//
//	[work]
//	WEBEX_DST=...
//
// For DefaultProfile, keys in the file's top-level/global section (before
// any "[section]" header) are used as a fallback if there is no explicit
// "[default]" section. Any other requested profile must exist as an
// explicit section, or LoadConfigFile returns an error.
//
// Recognized keys (same names as the historical .env variables):
//
//	WEBEX_TOKEN, WEBEX_DST
//	MSTEAMS_DST
//	SLACK_DST, SLACK_TOKEN, SLACK_CHANNEL
func LoadConfigFile(path, profile string) (Config, error) {
	f, err := ini.Load(path)
	if err != nil {
		return Config{}, fmt.Errorf("notify: parse config file %s: %w", path, err)
	}
	sec, err := configSection(f, profile)
	if err != nil {
		return Config{}, fmt.Errorf("notify: config file %s: %w", path, err)
	}
	return Config{
		Webex: WebexConfig{
			Token: cleanValue(sec.Key("WEBEX_TOKEN").String()),
			Dest:  cleanValue(sec.Key("WEBEX_DST").String()),
		},
		Teams: TeamsConfig{
			Dest: cleanValue(sec.Key("MSTEAMS_DST").String()),
		},
		Slack: SlackConfig{
			Dest:    cleanValue(sec.Key("SLACK_DST").String()),
			Token:   cleanValue(sec.Key("SLACK_TOKEN").String()),
			Channel: cleanValue(sec.Key("SLACK_CHANNEL").String()),
		},
	}, nil
}

// configSection picks the section holding the given profile's settings.
func configSection(f *ini.File, profile string) (*ini.Section, error) {
	if profile == "" {
		profile = DefaultProfile
	}
	for _, name := range f.SectionStrings() {
		// ini.DefaultSection ("DEFAULT") is the implicit section for keys
		// with no section header; it is handled by the fallback below and
		// must not be mistaken for an explicit "[default]" section.
		if name == ini.DefaultSection {
			continue
		}
		if strings.EqualFold(name, profile) {
			return f.Section(name), nil
		}
	}
	if strings.EqualFold(profile, DefaultProfile) {
		return f.Section(ini.DefaultSection), nil
	}
	return nil, fmt.Errorf("profile %q not found", profile)
}

// cleanValue strips a single layer of matching surrounding quotes, as used
// for string values in the example config (e.g. WEBEX_TOKEN="...").
func cleanValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// DefaultConfigPath resolves the config.ini path to use when none is given
// explicitly:
//
//  1. config.ini in the current working directory, if present.
//  2. Otherwise a per-user, OS-specific location:
//     - Linux/macOS: ~/.config/chatxgo/config.ini
//     - Windows:     %AppData%\chatxgo\config.ini
//
// The returned path is not guaranteed to exist; callers should os.Stat it
// before loading.
func DefaultConfigPath() (string, error) {
	if dir, err := os.Getwd(); err == nil {
		candidate := filepath.Join(dir, "config.ini")
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
	return filepath.Join(appData, "chatxgo", "config.ini")
}

func unixConfigPath(home string) string {
	return filepath.Join(home, ".config", "chatxgo", "config.ini")
}
