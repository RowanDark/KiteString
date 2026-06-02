package wordlist

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RowanDark/kitestring/pkg/ksfile"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// SecListsBaseURL is the base URL for raw SecLists content; override in tests.
var SecListsBaseURL = "https://raw.githubusercontent.com/danielmiessler/SecLists/master/"

// SecListsHTTPClient is used for all SecLists HTTP requests; override in tests.
var SecListsHTTPClient = &http.Client{Timeout: 60 * time.Second}

// SecListsAliases maps short alias names to their SecLists repository paths.
var SecListsAliases = map[string]string{
	"api-endpoints":              "Discovery/Web-Content/api/api-endpoints.txt",
	"api-seen-in-wild":           "Discovery/Web-Content/api/api-seen-in-wild.txt",
	"raft-large-words":           "Discovery/Web-Content/raft-large-words.txt",
	"raft-medium-words":          "Discovery/Web-Content/raft-medium-words.txt",
	"raft-small-words":           "Discovery/Web-Content/raft-small-words.txt",
	"common-api-endpoints":       "Discovery/Web-Content/common-api-endpoints-mazen160.txt",
	"swagger-wordlist":           "Discovery/Web-Content/swagger.txt",
	"burp-parameter-names":       "Discovery/Web-Content/burp-parameter-names.txt",
	"dirsearch":                  "Discovery/Web-Content/dirsearch.txt",
	"directory-list-2.3-medium":  "Discovery/Web-Content/directory-list-2.3-medium.txt",
}

// SecListEntry is a printable alias-to-path pair returned by ListSecListAliases.
type SecListEntry struct {
	Alias    string
	RepoPath string
}

// ListSecListAliases returns all defined SecLists aliases as a sorted slice.
func ListSecListAliases() []SecListEntry {
	entries := make([]SecListEntry, 0, len(SecListsAliases))
	for alias, path := range SecListsAliases {
		entries = append(entries, SecListEntry{Alias: alias, RepoPath: path})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Alias < entries[j].Alias
	})
	return entries
}

// FetchSecList resolves alias to its upstream URL, fetches the raw .txt content,
// and compiles it into a slice of GET routes.
func FetchSecList(alias string) ([]proute.Route, error) {
	repoPath, ok := SecListsAliases[alias]
	if !ok {
		return nil, fmt.Errorf("seclists: unknown alias %q — run: ks wordlist seclists list", alias)
	}

	url := SecListsBaseURL + repoPath
	resp, err := SecListsHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("seclists: fetch %q: %w", alias, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("seclists: fetch %q: HTTP %d from %s", alias, resp.StatusCode, url)
	}

	return parseSecListBody(resp.Body, alias), nil
}

// CompileSecList fetches the SecLists alias and writes the compiled routes to
// outputPath as a .ks file.
func CompileSecList(alias string, outputPath string) error {
	routes, err := FetchSecList(alias)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("seclists: create output dir: %w", err)
	}

	kf := ksfile.FromRoutes(routes, ksfile.KSFileMeta{
		Name:        "sl-" + alias,
		Description: "SecLists: " + SecListsAliases[alias],
		Source:      SecListsBaseURL + SecListsAliases[alias],
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	})

	if err := ksfile.Write(outputPath, kf); err != nil {
		return fmt.Errorf("seclists: write %s: %w", outputPath, err)
	}
	return nil
}

// ResolveSecList resolves an alias, fetching and caching it if not already
// present. Returns the path to the cached .ks file.
func ResolveSecList(alias string) (string, error) {
	dir, err := getCacheDir()
	if err != nil {
		return "", err
	}

	outputPath := filepath.Join(dir, "sl-"+alias+".ks")
	if _, err := os.Stat(outputPath); err == nil {
		return outputPath, nil
	}

	if err := CompileSecList(alias, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func parseSecListBody(r io.Reader, source string) []proute.Route {
	var routes []proute.Route
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		routes = append(routes, proute.Route{
			Method: "GET",
			Path:   line,
			Source: "seclists:" + source,
		})
	}
	return routes
}
