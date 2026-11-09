package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeFetcher struct {
	release    *Release
	releaseErr error

	downloadBody string
	bodies       map[string]string
	downloadErr  error
	requestedURL string
}

func (f *fakeFetcher) FetchLatestRelease() (*Release, error) {
	return f.release, f.releaseErr
}

func (f *fakeFetcher) Download(url string) (io.ReadCloser, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	if body, ok := f.bodies[url]; ok {
		return io.NopCloser(strings.NewReader(body)), nil
	}
	f.requestedURL = url
	return io.NopCloser(strings.NewReader(f.downloadBody)), nil
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"linux", "amd64", "agentboard-linux-x86_64", false},
		{"linux", "arm64", "agentboard-linux-arm64", false},
		{"darwin", "amd64", "agentboard-darwin-x86_64", false},
		{"darwin", "arm64", "agentboard-darwin-arm64", false},
		{"android", "arm64", "agentboard-android-arm64", false},
		{"windows", "amd64", "", true},
		{"linux", "386", "", true},
	}
	for _, tt := range tests {
		got, err := AssetName(tt.goos, tt.goarch)
		if tt.wantErr {
			if err == nil {
				t.Errorf("AssetName(%s, %s) = %q, want error", tt.goos, tt.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("AssetName(%s, %s) unexpected error: %v", tt.goos, tt.goarch, err)
		}
		if got != tt.want {
			t.Errorf("AssetName(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestCheck_NewerVersionAvailable(t *testing.T) {
	f := &fakeFetcher{release: &Release{TagName: "v0.2.0"}}

	rel, needsUpdate, err := Check(f, "v0.1.0")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needsUpdate {
		t.Fatal("needsUpdate = false, want true")
	}
	if rel.TagName != "v0.2.0" {
		t.Fatalf("TagName = %q", rel.TagName)
	}
}

func TestCheck_AlreadyUpToDate(t *testing.T) {
	f := &fakeFetcher{release: &Release{TagName: "v0.1.0"}}

	_, needsUpdate, err := Check(f, "v0.1.0")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needsUpdate {
		t.Fatal("needsUpdate = true, want false")
	}
}

func TestCheck_DevBuildAlwaysNeedsUpdate(t *testing.T) {
	f := &fakeFetcher{release: &Release{TagName: "v0.1.0"}}

	_, needsUpdate, err := Check(f, "dev")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needsUpdate {
		t.Fatal("needsUpdate = false, want true for dev build")
	}
}

func TestCheck_FetchError(t *testing.T) {
	f := &fakeFetcher{releaseErr: errors.New("network down")}

	_, _, err := Check(f, "v0.1.0")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestApply_ReplacesExecutable(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	if err := os.WriteFile(execPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed exec file: %v", err)
	}

	rel, f := signedRelease("new binary", sha256Hex("new binary"))

	if err := Apply(f, rel, execPath, "linux", "amd64"); err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read updated binary: %v", err)
	}
	if string(got) != "new binary" {
		t.Fatalf("updated binary contents = %q, want %q", got, "new binary")
	}

	info, err := os.Stat(execPath)
	if err != nil {
		t.Fatalf("stat updated binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("updated binary is not executable: mode=%v", info.Mode())
	}

	if f.requestedURL != "https://example.com/agentboard-linux-x86_64" {
		t.Fatalf("requested URL = %q", f.requestedURL)
	}
}

func TestApply_NoMatchingAsset(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	os.WriteFile(execPath, []byte("old"), 0o755)

	rel := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: "agentboard-darwin-arm64", BrowserDownloadURL: "https://example.com/agentboard-darwin-arm64"},
		},
	}
	f := &fakeFetcher{downloadBody: "new binary"}

	err := Apply(f, rel, execPath, "linux", "amd64")

	if err == nil {
		t.Fatal("expected error for missing asset, got nil")
	}

	got, _ := os.ReadFile(execPath)
	if string(got) != "old" {
		t.Fatalf("executable should be untouched on failure, got %q", got)
	}
}

func TestApply_UnsupportedPlatform(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	os.WriteFile(execPath, []byte("old"), 0o755)

	rel := &Release{TagName: "v0.2.0"}
	f := &fakeFetcher{}

	err := Apply(f, rel, execPath, "windows", "amd64")

	if err == nil {
		t.Fatal("expected error for unsupported platform, got nil")
	}
}

func TestApply_DownloadError(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	os.WriteFile(execPath, []byte("old"), 0o755)

	rel := &Release{
		Assets: []Asset{
			{Name: "agentboard-linux-x86_64", BrowserDownloadURL: "https://example.com/x"},
		},
	}
	f := &fakeFetcher{downloadErr: errors.New("connection reset")}

	err := Apply(f, rel, execPath, "linux", "amd64")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "old" {
		t.Fatalf("executable should be untouched on download failure, got %q", got)
	}
}

const checksumsURL = "https://example.com/checksums.txt"

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func signedRelease(body, sum string) (*Release, *fakeFetcher) {
	rel := &Release{
		TagName: "v0.2.0",
		Assets: []Asset{
			{Name: "agentboard-linux-x86_64", BrowserDownloadURL: "https://example.com/agentboard-linux-x86_64"},
			{Name: "checksums.txt", BrowserDownloadURL: checksumsURL},
		},
	}
	f := &fakeFetcher{
		downloadBody: body,
		bodies: map[string]string{
			checksumsURL: sum + "  agentboard-darwin-arm64\n" + sum + "  agentboard-linux-x86_64\n",
		},
	}
	return rel, f
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.9", true},
		{"v0.1.10", "v0.1.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.9", "v0.1.9", false},
		{"v0.1.9", "v0.2.0", false},
		{"v0.2.0", "0.1.0", true},
		{"v0.2.0", "dev", true},
		{"v0.2.0-rc1", "v0.2.0", false},
	}
	for _, tt := range tests {
		if got := IsNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestCheck_NeverDowngradesANewerBuild(t *testing.T) {
	f := &fakeFetcher{release: &Release{TagName: "v0.1.9"}}

	_, needsUpdate, err := Check(f, "v0.2.0")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needsUpdate {
		t.Fatal("needsUpdate = true, want false when the running build is newer")
	}
}

func TestApply_ChecksumMismatchLeavesExecutable(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	os.WriteFile(execPath, []byte("old"), 0o755)
	rel, f := signedRelease("tampered binary", sha256Hex("new binary"))

	err := Apply(f, rel, execPath, "linux", "amd64")

	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "old" {
		t.Fatalf("executable should be untouched on checksum mismatch, got %q", got)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".agentboard-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestApply_ReleaseWithoutChecksumsIsRefused(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "agentboard")
	os.WriteFile(execPath, []byte("old"), 0o755)
	rel := &Release{
		TagName: "v0.2.0",
		Assets:  []Asset{{Name: "agentboard-linux-x86_64", BrowserDownloadURL: "https://example.com/x"}},
	}
	f := &fakeFetcher{downloadBody: "new binary"}

	if err := Apply(f, rel, execPath, "linux", "amd64"); err == nil {
		t.Fatal("expected an error for a release with no checksums.txt")
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "old" {
		t.Fatalf("executable should be untouched, got %q", got)
	}
}

func TestApply_ReplacesTheTargetOfASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "versions", "agentboard")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old"), 0o755)
	link := filepath.Join(dir, "agentboard")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	rel, f := signedRelease("new binary", sha256Hex("new binary"))

	if err := Apply(f, rel, link, "linux", "amd64"); err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("launcher symlink was replaced: %v %v", fi, err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new binary" {
		t.Fatalf("symlink target = %q, want new binary", got)
	}
}
