package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const RepositoryURL = "https://github.com/zJay26/codex-usage"
const LatestURL = "https://api.github.com/repos/zJay26/codex-usage/releases/latest"
const maxBinarySize = 128 << 20

var stableTag = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Newer(candidate, current string) bool {
	a := stableTag.FindStringSubmatch(candidate)
	b := stableTag.FindStringSubmatch(strings.SplitN(current, "-", 2)[0])
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, e1 := strconv.ParseUint(a[i], 10, 32)
		y, e2 := strconv.ParseUint(b[i], 10, 32)
		if e1 != nil || e2 != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

func AssetName(goos, arch string) string {
	if (goos != "windows" && goos != "linux" && goos != "darwin") || (arch != "amd64" && arch != "arm64") {
		return ""
	}
	name := "codex-usage-" + goos + "-" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func HTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("unsafe update redirect")
		}
		switch req.URL.Hostname() {
		case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		}
		return fmt.Errorf("unexpected update host")
	}}
}

func get(ctx context.Context, client *http.Client, raw string, limit int64, out io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "codex-usage-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("update server returned HTTP %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return fmt.Errorf("update response exceeds size limit")
	}
	n, err := io.Copy(out, io.LimitReader(res.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("update response exceeds size limit")
	}
	return nil
}

func fetchRelease(ctx context.Context, client *http.Client, endpoint, assetName string) (Release, error) {
	var body strings.Builder
	if err := get(ctx, client, endpoint, 1<<20, &body); err != nil {
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal([]byte(body.String()), &r); err != nil {
		return r, err
	}
	if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "v") || !stableTag.MatchString(r.Tag) {
		return r, fmt.Errorf("no stable release available")
	}
	if assetName != "" {
		_, _, err := r.assets(assetName)
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

func (r Release) assets(name string) (Asset, Asset, error) {
	var binary, sums Asset
	counts := map[string]int{}
	for _, a := range r.Assets {
		if a.Name != name && a.Name != "SHA256SUMS" {
			continue
		}
		counts[a.Name]++
		want := RepositoryURL + "/releases/download/" + r.Tag + "/" + a.Name
		u, err := url.Parse(a.URL)
		if err != nil || a.URL != want || u.User != nil || a.Size <= 0 || a.Size > maxBinarySize {
			return binary, sums, fmt.Errorf("invalid release asset")
		}
		if a.Name == name {
			binary = a
		} else {
			sums = a
		}
	}
	if counts[name] != 1 || counts["SHA256SUMS"] != 1 {
		return binary, sums, fmt.Errorf("release assets are incomplete")
	}
	return binary, sums, nil
}

func checksum(body, name string) (string, error) {
	value := ""
	scan := bufio.NewScanner(strings.NewReader(body))
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		if value != "" {
			return "", fmt.Errorf("duplicate checksum")
		}
		value = strings.ToLower(f[0])
	}
	decoded, err := hex.DecodeString(value)
	if scan.Err() != nil || err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("missing or invalid SHA256 checksum")
	}
	return value, nil
}

func download(ctx context.Context, client *http.Client, r Release, name, path string) (string, error) {
	binary, sums, err := r.assets(name)
	if err != nil {
		return "", err
	}
	var body strings.Builder
	if err = get(ctx, client, sums.URL, 64<<10, &body); err != nil {
		return "", err
	}
	want, err := checksum(body.String(), name)
	if err != nil {
		return "", err
	}
	if binary.Digest != "" && binary.Digest != "sha256:"+want {
		return "", fmt.Errorf("release digest disagrees with checksum")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	hash := sha256.New()
	if err = get(ctx, client, binary.URL, binary.Size, io.MultiWriter(f, hash)); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() != binary.Size || hex.EncodeToString(hash.Sum(nil)) != want {
		return "", fmt.Errorf("downloaded file failed SHA256 verification")
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Chmod(path, 0700); err != nil {
		return "", err
	}
	ok = true
	return want, nil
}
