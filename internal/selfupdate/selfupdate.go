// Package selfupdate updates chatxgo from its GitHub release assets.
package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"

	updaterlib "github.com/sig9org/selfupdate-go"
)

// Repository is the GitHub owner/repository containing the release assets.
const Repository = "sig9org/chatxgo"

type updater struct {
	client         *http.Client
	goos           string
	goarch         string
	executablePath func() (string, error)
}

func defaultUpdater() updater {
	return updater{client: http.DefaultClient, goos: runtime.GOOS, goarch: runtime.GOARCH, executablePath: os.Executable}
}

// Update checks GitHub for a release newer than currentVersion and, when its
// binary's SHA-256 matches checksums.txt, replaces the running binary.
func Update(ctx context.Context, currentVersion string) (string, error) {
	return defaultUpdater().update(ctx, currentVersion)
}

func (u updater) update(ctx context.Context, currentVersion string) (string, error) {
	executable, err := u.executablePath()
	if err != nil {
		return "", fmt.Errorf("selfupdate: locate running executable: %w", err)
	}
	client := u.client
	if client == nil {
		client = http.DefaultClient
	}
	updater, err := updaterlib.New(updaterlib.Config{
		Repository: Repository,
		HTTPClient: client,
		Executable: executable,
		GOOS:       u.goos,
		GOARCH:     u.goarch,
		Validator:  updaterlib.SHA256Validator{AssetName: "checksums.txt"},
	})
	if err != nil {
		return "", fmt.Errorf("selfupdate: configure updater: %w", err)
	}
	result, err := updater.Update(ctx, currentVersion)
	if err != nil {
		return "", err
	}
	if !result.Updated {
		return fmt.Sprintf("already up to date (current %s, latest %s)", currentVersion, result.LatestVersion), nil
	}
	return fmt.Sprintf("updated to version %s", result.LatestVersion), nil
}
