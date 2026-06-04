package wordlist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

const DefaultManifestURL = "https://raw.githubusercontent.com/RowanDark/kitestring/main/wordlists/manifest.json"

var (
	// ManifestURL is the remote manifest location; override in tests.
	ManifestURL = DefaultManifestURL

	// HTTPClient is used for all remote requests; override in tests.
	HTTPClient = &http.Client{Timeout: 30 * time.Second}

	// CacheDirOverride bypasses os.UserCacheDir(); set in tests.
	CacheDirOverride string
)

// Manifest is the top-level structure of wordlists/manifest.json.
type Manifest struct {
	Version   int             `json:"version"`
	UpdatedAt string          `json:"updated_at"`
	Wordlists []WordlistEntry `json:"wordlists"`
}

// WordlistEntry describes a single remote wordlist.
type WordlistEntry struct {
	Alias            string  `json:"alias"`
	Filename         string  `json:"filename"`
	Description      string  `json:"description"`
	Count            int     `json:"count"`
	CompressedSizeMB float64 `json:"compressed_size_mb"`
	URL              string  `json:"url"`
}

// CachedWordlist describes a wordlist present in the local cache.
type CachedWordlist struct {
	Alias    string
	Filename string
	Path     string
}

// FetchManifest downloads and parses the remote manifest.
func FetchManifest() (*Manifest, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ManifestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("wordlist: build manifest request: %w", err)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wordlist: fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordlist: fetch manifest: HTTP %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("wordlist: parse manifest: %w", err)
	}
	return &m, nil
}

// ListRemote fetches the manifest and returns all wordlist entries.
func ListRemote() ([]WordlistEntry, error) {
	m, err := FetchManifest()
	if err != nil {
		return nil, err
	}
	return m.Wordlists, nil
}

// ListCached scans the cache directory and returns all cached wordlists.
// Returns nil (not an error) when the cache directory does not yet exist.
func ListCached() ([]CachedWordlist, error) {
	dir, err := getCacheDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("wordlist: read cache dir: %w", err)
	}
	var cached []CachedWordlist
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ks") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		alias := strings.TrimSuffix(e.Name(), ".ks")
		cached = append(cached, CachedWordlist{
			Alias:    alias,
			Filename: e.Name(),
			Path:     path,
		})
	}
	return cached, nil
}

// Update downloads wordlists from the manifest. If aliases is non-empty only
// those aliases are downloaded; otherwise all manifest entries are fetched.
// Already-cached files are skipped unless force is true.
func Update(aliases []string, force bool) error {
	m, err := FetchManifest()
	if err != nil {
		return err
	}

	dir, err := getCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("wordlist: create cache dir: %w", err)
	}

	targets := m.Wordlists
	if len(aliases) > 0 {
		targets = filterByAlias(m.Wordlists, aliases)
		if len(targets) == 0 {
			return fmt.Errorf("wordlist: no wordlists matched aliases: %s", strings.Join(aliases, ", "))
		}
	}

	for _, entry := range targets {
		dest := filepath.Join(dir, entry.Filename)
		if !force {
			if _, err := os.Stat(dest); err == nil {
				fmt.Printf("  %-20s already cached, skipping (use --force to re-download)\n", entry.Alias)
				continue
			}
		}
		fmt.Printf("Downloading %s — %s\n", entry.Alias, entry.Description)
		if err := downloadWithProgress(entry.URL, dest); err != nil {
			return fmt.Errorf("wordlist: download %s: %w", entry.Alias, err)
		}
		fmt.Printf("\n  Saved → %s\n", dest)
	}
	return nil
}

// ResolveAlias resolves an alias (optionally with :N head syntax) to the
// cached .ks file path and an optional head limit (0 = all).
func ResolveAlias(aliasSpec string) (path string, headLimit int, err error) {
	alias := aliasSpec
	if idx := strings.LastIndex(aliasSpec, ":"); idx != -1 {
		n := 0
		if _, scanErr := fmt.Sscanf(aliasSpec[idx+1:], "%d", &n); scanErr != nil {
			return "", 0, fmt.Errorf("wordlist: invalid head limit in %q", aliasSpec)
		}
		alias = aliasSpec[:idx]
		headLimit = n
	}

	dir, err := getCacheDir()
	if err != nil {
		return "", 0, err
	}
	path = filepath.Join(dir, alias+".ks")
	if _, statErr := os.Stat(path); statErr != nil {
		return "", 0, fmt.Errorf("wordlist: alias %q not cached — run: ks wordlist update %s", alias, alias)
	}
	return path, headLimit, nil
}

func getCacheDir() (string, error) {
	if CacheDirOverride != "" {
		return CacheDirOverride, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("wordlist: resolve cache dir: %w", err)
	}
	return filepath.Join(base, "kitestring", "wordlists"), nil
}

func filterByAlias(entries []WordlistEntry, aliases []string) []WordlistEntry {
	want := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		want[a] = true
	}
	var out []WordlistEntry
	for _, e := range entries {
		if want[e.Alias] {
			out = append(out, e)
		}
	}
	return out
}

func downloadWithProgress(url, dest string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	bar := progressbar.DefaultBytes(resp.ContentLength, "")
	_, err = io.Copy(io.MultiWriter(f, bar), resp.Body)
	return err
}
