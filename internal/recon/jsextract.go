package recon

import (
	"log"
	"regexp"

	"github.com/RowanDark/kitestring/pkg/proute"
)

var scriptSrcRe = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)

// ExtractScriptURLs returns all script src URLs found in body that are in scope.
// Out-of-scope URLs are silently dropped unless verbose logging is enabled.
func ExtractScriptURLs(body []byte, scope proute.ScopeChecker, verbose string) []string {
	matches := scriptSrcRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	var urls []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		u := string(m[1])
		if scope != nil && scope.IsOutOfScope(u) {
			if verbose == "warn" || verbose == "info" || verbose == "debug" || verbose == "trace" {
				log.Printf("[WARN] skipping out-of-scope script: %s", u)
			}
			continue
		}
		urls = append(urls, u)
	}
	return urls
}
