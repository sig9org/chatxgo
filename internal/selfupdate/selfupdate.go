// Package selfupdate updates chatxgo from its GitHub release assets.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	binaryupdate "github.com/creativeprojects/go-selfupdate/update"
	"github.com/sig9org/chatxgo/internal/debugx"
)

// Repository is "owner/repo" on GitHub, where release binaries are
// published (see Taskfile.yml build-all for the naming convention).
const Repository = "sig9org/chatxgo"

const githubAPIBase = "https://api.github.com"

type githubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type updater struct {
	client         *http.Client
	apiBase        string
	goos           string
	goarch         string
	executablePath func() (string, error)
	apply          func(io.Reader, string) error
}

func defaultUpdater() updater {
	return updater{
		client:         http.DefaultClient,
		apiBase:        githubAPIBase,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
		executablePath: executablePath,
		apply: func(source io.Reader, target string) error {
			return binaryupdate.Apply(source, binaryupdate.Options{TargetPath: target})
		},
	}
}

// Update checks GitHub for a release newer than currentVersion and, if
// found, replaces the running binary in place. It reports the outcome as a
// human-readable message.
func Update(ctx context.Context, currentVersion string) (string, error) {
	return defaultUpdater().update(ctx, currentVersion)
}

func (u updater) update(ctx context.Context, currentVersion string) (string, error) {
	debugx.Printf("checking %s for a release newer than %s", Repository, currentVersion)

	release, err := u.latestRelease(ctx)
	if err != nil {
		return "", fmt.Errorf("selfupdate: detect latest release: %w", err)
	}
	latestVersion, err := semver.NewVersion(release.TagName)
	if err != nil {
		return "", fmt.Errorf("selfupdate: invalid release version %q: %w", release.TagName, err)
	}

	assetName := releaseAssetName(release.TagName, u.goos, u.goarch)
	asset, found := findAsset(release.Assets, assetName)
	if !found {
		return "", fmt.Errorf("selfupdate: no release asset %q found for %s/%s", assetName, u.goos, u.goarch)
	}
	debugx.Printf("latest release found: %s (%s)", release.TagName, asset.Name)

	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion != "" && currentVersion != "dev" {
		current, err := semver.NewVersion(currentVersion)
		if err != nil {
			return "", fmt.Errorf("selfupdate: invalid current version %q: %w", currentVersion, err)
		}
		if !latestVersion.GreaterThan(current) {
			return fmt.Sprintf("already up to date (current %s, latest %s)", currentVersion, release.TagName), nil
		}
	}

	target, err := u.executablePath()
	if err != nil {
		return "", fmt.Errorf("selfupdate: locate running executable: %w", err)
	}
	debugx.Printf("updating executable at %s", target)

	response, err := u.get(ctx, asset.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("selfupdate: download %s: %w", asset.Name, err)
	}
	defer response.Body.Close()
	if err := u.apply(response.Body, target); err != nil {
		return "", fmt.Errorf("selfupdate: update failed: %w", err)
	}
	return fmt.Sprintf("updated to version %s", release.TagName), nil
}

func (u updater) latestRelease(ctx context.Context) (githubRelease, error) {
	response, err := u.get(ctx, strings.TrimRight(u.apiBase, "/")+"/repos/"+Repository+"/releases/latest")
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()

	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub response: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("GitHub response has no release tag")
	}
	return release, nil
}

func (u updater) get(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "chatxgo")

	response, err := u.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("unexpected status %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return response, nil
}

func releaseAssetName(version, goos, goarch string) string {
	name := "chatxgo_" + version + "_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func findAsset(assets []githubAsset, name string) (githubAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name && strings.TrimSpace(asset.DownloadURL) != "" {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
