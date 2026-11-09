// Package selfupdate checks GitHub releases for a newer agentboard build
// and, if one exists, downloads and atomically replaces the running
// executable.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultRepo = "ayandexyz/agent-board"

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Fetcher abstracts the GitHub API and asset download so Check/Apply are
// testable without network access.
type Fetcher interface {
	FetchLatestRelease() (*Release, error)
	Download(url string) (io.ReadCloser, error)
}

type httpFetcher struct {
	repo   string
	client *http.Client
}

// NewHTTPFetcher returns a Fetcher backed by the real GitHub API, e.g.
// NewHTTPFetcher(DefaultRepo).
func NewHTTPFetcher(repo string) Fetcher {
	return &httpFetcher{
		repo:   repo,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *httpFetcher) FetchLatestRelease() (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", f.repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("selfupdate.FetchLatestRelease: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("selfupdate.FetchLatestRelease: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("selfupdate.FetchLatestRelease: unexpected status %s", resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("selfupdate.FetchLatestRelease: decoding response: %w", err)
	}
	return &rel, nil
}

func (f *httpFetcher) Download(url string) (io.ReadCloser, error) {
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("selfupdate.Download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("selfupdate.Download: unexpected status %s", resp.Status)
	}
	return resp.Body, nil
}

// AssetName returns the release asset filename for the given GOOS/GOARCH,
// matching the naming produced by .github/workflows/release.yml.
func AssetName(goos, goarch string) (string, error) {
	if goos == "windows" {
		return "", fmt.Errorf("selfupdate: windows is not supported yet")
	}

	var arch string
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("selfupdate: unsupported architecture %q", goarch)
	}

	return fmt.Sprintf("agentboard-%s-%s", goos, arch), nil
}

// ChecksumsAsset is the release asset listing "<sha256>  <asset>" for
// every binary, written by .github/workflows/release.yml.
const ChecksumsAsset = "checksums.txt"

// IsNewer reports whether latest is a strictly higher version than current,
// comparing dotted numeric parts with any leading "v" dropped. A part that
// is not a number counts as 0, so "dev" is older than every release and a
// prerelease never outranks its own release.
func IsNewer(latest, current string) bool {
	a, b := versionParts(latest), versionParts(current)
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	parts := make([]int, len(fields))
	for i, f := range fields {
		parts[i], _ = strconv.Atoi(f)
	}
	return parts
}

// Check fetches the latest release and reports whether it is newer than
// currentVersion. A "dev" build (no version stamped in) is older than any
// release.
func Check(f Fetcher, currentVersion string) (release *Release, needsUpdate bool, err error) {
	rel, err := f.FetchLatestRelease()
	if err != nil {
		return nil, false, fmt.Errorf("selfupdate.Check: %w", err)
	}
	return rel, IsNewer(rel.TagName, currentVersion), nil
}

// Apply downloads the release asset matching goos/goarch, verifies it
// against the release's checksums.txt and atomically replaces the
// executable at execPath. A symlinked execPath has its target replaced so
// the link survives. The existing executable is left untouched if any step
// fails.
func Apply(f Fetcher, release *Release, execPath, goos, goarch string) error {
	assetName, err := AssetName(goos, goarch)
	if err != nil {
		return fmt.Errorf("selfupdate.Apply: %w", err)
	}

	asset := findAsset(release, assetName)
	if asset == nil {
		return fmt.Errorf("selfupdate.Apply: no release asset named %q", assetName)
	}
	sums := findAsset(release, ChecksumsAsset)
	if sums == nil {
		return fmt.Errorf("selfupdate.Apply: release %s has no %s to verify against", release.TagName, ChecksumsAsset)
	}

	want, err := expectedSum(f, sums.BrowserDownloadURL, assetName)
	if err != nil {
		return fmt.Errorf("selfupdate.Apply: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}

	body, err := f.Download(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("selfupdate.Apply: %w", err)
	}
	defer body.Close()

	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".agentboard-update-*")
	if err != nil {
		return fmt.Errorf("selfupdate.Apply: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), body); err != nil {
		tmp.Close()
		return fmt.Errorf("selfupdate.Apply: downloading %s: %w", assetName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("selfupdate.Apply: %w", err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("selfupdate.Apply: checksum mismatch for %s: got %s, want %s", assetName, got, want)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("selfupdate.Apply: %w", err)
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("selfupdate.Apply: replacing executable: %w", err)
	}

	return nil
}

func findAsset(release *Release, name string) *Asset {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i]
		}
	}
	return nil
}

func expectedSum(f Fetcher, url, assetName string) (string, error) {
	body, err := f.Download(url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", ChecksumsAsset, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s lists no checksum for %s", ChecksumsAsset, assetName)
}
