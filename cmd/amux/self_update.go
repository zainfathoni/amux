package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubLatestReleaseURL     = "https://api.github.com/repos/zainfathoni/amux/releases/latest"
	githubLatestReleasePageURL = "https://github.com/zainfathoni/amux/releases/latest"
	selfUpdateTimeout          = 2 * time.Minute
)

var (
	selfUpdateHTTPClient     = http.DefaultClient
	selfUpdateReleaseURL     = githubLatestReleaseURL
	selfUpdateReleasePageURL = githubLatestReleasePageURL
	executablePath           = os.Executable
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type httpStatusError struct {
	URL                string
	Status             string
	Code               int
	RateLimitRemaining string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s returned %s", e.URL, e.Status)
}

func (a app) selfUpdate(opts options, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: amux update")
	}
	if !supportedSelfUpdatePlatform(runtime.GOOS, runtime.GOARCH) {
		return fmt.Errorf("self-update is unsupported on %s/%s: no release asset is published for this platform", runtime.GOOS, runtime.GOARCH)
	}

	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	installPath, err := filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve current executable path: %w", err)
	}
	installPath, err = resolveSelfUpdateTarget(installPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), selfUpdateTimeout)
	defer cancel()

	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	versionComparison, versionsComparable := compareReleaseVersions(version, release.TagName)
	if version == release.TagName || (versionsComparable && versionComparison >= 0) {
		fmt.Fprintf(a.stdout, "amux is already up to date (%s)\n", version)
		if warning := selfUpdateShadowWarning(installPath); warning != "" {
			fmt.Fprintln(a.stdout, warning)
		}
		return nil
	}
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archiveAsset, checksumAsset, err := findSelfUpdateAssets(release, archiveName)
	if err != nil {
		return err
	}

	if opts.dryRun {
		fmt.Fprintf(a.stdout, "Would update %s to %s using %s\n", installPath, release.TagName, archiveAsset.Name)
		if warning := selfUpdateShadowWarning(installPath); warning != "" {
			fmt.Fprintln(a.stdout, warning)
		}
		return nil
	}
	if err := ensureDirectoryWritable(filepath.Dir(installPath)); err != nil {
		return fmt.Errorf("self-update cannot replace %s: %w", installPath, err)
	}

	archiveBytes, err := downloadURL(ctx, archiveAsset.DownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveAsset.Name, err)
	}
	checksumBytes, err := downloadURL(ctx, checksumAsset.DownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", checksumAsset.Name, err)
	}
	if err := verifySHA256(archiveBytes, checksumBytes, archiveAsset.Name); err != nil {
		return err
	}
	binaryBytes, err := extractAmuxBinary(archiveBytes)
	if err != nil {
		return err
	}
	if err := replaceCurrentBinary(installPath, binaryBytes); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Updated amux to %s at %s\n", release.TagName, installPath)
	if warning := selfUpdateShadowWarning(installPath); warning != "" {
		fmt.Fprintln(a.stdout, warning)
	}
	return nil
}

func selfUpdateShadowWarning(installPath string) string {
	pathTarget, err := exec.LookPath("amux")
	if err != nil || pathTarget == "" {
		return ""
	}
	pathTarget, err = filepath.Abs(pathTarget)
	if err != nil {
		return ""
	}
	pathTarget = resolvePathForComparison(pathTarget)
	installPath = resolvePathForComparison(installPath)
	if pathTarget == installPath {
		return ""
	}
	return fmt.Sprintf("Warning: updated %s, but `amux` on PATH resolves to %s. Update or remove the shadowing binary so `amux version` uses the updated install.", installPath, pathTarget)
}

func resolvePathForComparison(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func resolveSelfUpdateTarget(path string) (string, error) {
	resolved := path
	if target, err := filepath.EvalSymlinks(path); err == nil {
		resolved = target
	}
	if managedInstallPath(resolved) {
		return "", fmt.Errorf("self-update refused for package-managed install at %s; install amux to a user-writable path such as ~/.local/bin/amux to use self-update", resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect current executable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("current executable path is a directory: %s", resolved)
	}
	return resolved, nil
}

func managedInstallPath(path string) bool {
	clean := filepath.Clean(path)
	prefixes := []string{
		"/nix/store/",
		"/gnu/store/",
		"/opt/homebrew/Cellar/",
		"/home/linuxbrew/.linuxbrew/Cellar/",
		"/usr/local/Cellar/",
		"/snap/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(clean+string(os.PathSeparator), prefix) {
			return true
		}
	}
	return false
}

func ensureDirectoryWritable(dir string) error {
	tmp, err := os.CreateTemp(dir, ".amux-write-test-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	closeErr := tmp.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	var release githubRelease
	body, err := downloadURL(ctx, selfUpdateReleaseURL)
	if err != nil {
		var statusErr *httpStatusError
		if !errors.As(err, &statusErr) ||
			(statusErr.Code != http.StatusForbidden && statusErr.Code != http.StatusTooManyRequests) ||
			statusErr.RateLimitRemaining != "0" {
			return release, fmt.Errorf("check latest release: %w", err)
		}
		fallbackRelease, fallbackErr := fetchLatestReleaseFromRedirect(ctx)
		if fallbackErr != nil {
			return release, fmt.Errorf("check latest release: %w (fallback failed: %v)", err, fallbackErr)
		}
		return fallbackRelease, nil
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return release, fmt.Errorf("parse latest release response: %w", err)
	}
	if release.TagName == "" {
		return release, errors.New("latest release response did not include tag_name")
	}
	return release, nil
}

func fetchLatestReleaseFromRedirect(ctx context.Context) (githubRelease, error) {
	var release githubRelease
	releasePageURL, err := url.Parse(selfUpdateReleasePageURL)
	if err != nil {
		return release, fmt.Errorf("parse latest release page URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, selfUpdateReleasePageURL, nil)
	if err != nil {
		return release, err
	}
	req.Header.Set("User-Agent", "amux-self-update")
	resp, err := selfUpdateHTTPClient.Do(req)
	if err != nil {
		return release, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return release, fmt.Errorf("HEAD %s returned %s", selfUpdateReleasePageURL, resp.Status)
	}

	releasePath := strings.TrimSuffix(releasePageURL.EscapedPath(), "/latest")
	tagPrefix := releasePath + "/tag/"
	if resp.Request.URL.Scheme != releasePageURL.Scheme ||
		resp.Request.URL.Host != releasePageURL.Host ||
		!strings.HasPrefix(resp.Request.URL.EscapedPath(), tagPrefix) {
		return release, fmt.Errorf("latest release did not redirect to a tagged release: %s", resp.Request.URL)
	}
	escapedTagName := strings.TrimPrefix(resp.Request.URL.EscapedPath(), tagPrefix)
	if escapedTagName == "" || strings.Contains(escapedTagName, "/") {
		return release, fmt.Errorf("latest release redirect included invalid tag %q", escapedTagName)
	}
	tagName, err := url.PathUnescape(escapedTagName)
	if err != nil || tagName == "" || strings.Contains(tagName, "/") {
		return release, fmt.Errorf("latest release redirect included invalid tag %q", tagName)
	}
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	downloadBase := strings.TrimSuffix(selfUpdateReleasePageURL, "/latest") + "/download/" + url.PathEscape(tagName) + "/"
	release.TagName = tagName
	release.Assets = []githubReleaseAsset{
		{Name: archiveName, DownloadURL: downloadBase + archiveName},
		{Name: archiveName + ".sha256", DownloadURL: downloadBase + archiveName + ".sha256"},
	}
	return release, nil
}

func supportedSelfUpdatePlatform(goos, goarch string) bool {
	return (goos == "linux" || goos == "darwin") && (goarch == "amd64" || goarch == "arm64")
}

func compareReleaseVersions(current, latest string) (int, bool) {
	parse := func(value string) ([3]int, bool) {
		var parsed [3]int
		if !strings.HasPrefix(value, "v") {
			return parsed, false
		}
		parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
		if len(parts) != len(parsed) {
			return parsed, false
		}
		for i, part := range parts {
			if part == "" || (len(part) > 1 && part[0] == '0') {
				return parsed, false
			}
			for _, digit := range part {
				if digit < '0' || digit > '9' {
					return parsed, false
				}
			}
			n, err := strconv.Atoi(part)
			if err != nil {
				return parsed, false
			}
			parsed[i] = n
		}
		return parsed, true
	}
	currentParts, currentOK := parse(current)
	latestParts, latestOK := parse(latest)
	if !currentOK || !latestOK {
		return 0, false
	}
	for i := range currentParts {
		if currentParts[i] < latestParts[i] {
			return -1, true
		}
		if currentParts[i] > latestParts[i] {
			return 1, true
		}
	}
	return 0, true
}

func findSelfUpdateAssets(release githubRelease, archiveName string) (githubReleaseAsset, githubReleaseAsset, error) {
	var archiveAsset githubReleaseAsset
	var checksumAsset githubReleaseAsset
	for _, asset := range release.Assets {
		if asset.Name == archiveName {
			archiveAsset = asset
		}
		if asset.Name == archiveName+".sha256" {
			checksumAsset = asset
		}
	}
	if archiveAsset.DownloadURL == "" {
		return archiveAsset, checksumAsset, fmt.Errorf("latest release %s does not include %s", release.TagName, archiveName)
	}
	if checksumAsset.DownloadURL == "" {
		return archiveAsset, checksumAsset, fmt.Errorf("latest release %s does not include %s.sha256", release.TagName, archiveName)
	}
	return archiveAsset, checksumAsset, nil
}

func downloadURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "amux-self-update")
	resp, err := selfUpdateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{
			URL:                url,
			Status:             resp.Status,
			Code:               resp.StatusCode,
			RateLimitRemaining: resp.Header.Get("X-RateLimit-Remaining"),
		}
	}
	return io.ReadAll(resp.Body)
}

func verifySHA256(contents, checksumBytes []byte, archiveName string) error {
	fields := strings.Fields(string(checksumBytes))
	if len(fields) == 0 {
		return errors.New("checksum file is empty")
	}
	if len(fields) > 1 && filepath.Base(fields[1]) != archiveName {
		return fmt.Errorf("checksum file is for %s, not %s", filepath.Base(fields[1]), archiveName)
	}
	want := strings.ToLower(fields[0])
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("checksum %q is not a SHA-256 digest", fields[0])
	}
	sum := sha256.Sum256(contents)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	return nil
}

func extractAmuxBinary(archiveBytes []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != "amux" {
			continue
		}
		binary, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract amux binary: %w", err)
		}
		return binary, nil
	}
	return nil, errors.New("release archive did not contain an amux binary")
}

func replaceCurrentBinary(path string, binary []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".amux-update-*")
	if err != nil {
		return fmt.Errorf("create temporary binary: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary binary: %w", err)
	}
	if err := tmp.Chmod(info.Mode().Perm() | 0o755); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temporary binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary binary: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
