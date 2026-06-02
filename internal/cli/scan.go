package cli

import (
	"fmt"

	"github.com/RowanDark/kitestring/internal/wordlist"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [url]",
	Short: "Context-aware API endpoint discovery",
	Long: `Scan a target URL for API endpoints using context-aware analysis.

KiteString inspects JavaScript bundles, OpenAPI specs, and response patterns
to intelligently discover API routes rather than blindly fuzzing paths.

Example:
  ks scan https://example.com
  ks scan https://api.example.com/v1 --depth 3
  ks scan https://api.example.com -A apiroutes
  ks scan https://api.example.com -A apiroutes:20000
  ks scan https://api.example.com --openapi-url https://api.example.com/openapi.json
  ks scan https://api.example.com --openapi-file ./local-spec.yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		aliases, _ := cmd.Flags().GetStringArray("wordlist-alias")
		wordlistFiles, _ := cmd.Flags().GetStringArray("wordlist")
		headN, _ := cmd.Flags().GetInt("head")
		seclistsAlias, _ := cmd.Flags().GetString("seclists")
		openapiURL, _ := cmd.Flags().GetString("openapi-url")
		openapiFile, _ := cmd.Flags().GetString("openapi-file")

		// Resolve any alias specs (e.g. "apiroutes" or "apiroutes:20000")
		// and append the resulting file paths to wordlistFiles.
		for _, spec := range aliases {
			path, limit, err := wordlist.ResolveAlias(spec)
			if err != nil {
				return err
			}
			wordlistFiles = append(wordlistFiles, path)
			// Per-alias head limit overrides --head when non-zero.
			if limit > 0 && headN == 0 {
				headN = limit
			}
		}

		// Resolve --seclists alias, fetching and caching on demand.
		if seclistsAlias != "" {
			path, err := wordlist.ResolveSecList(seclistsAlias)
			if err != nil {
				return err
			}
			wordlistFiles = append(wordlistFiles, path)
		}

		// Fetch OpenAPI spec at scan time (no caching).
		if openapiURL != "" {
			fmt.Printf("Fetching OpenAPI spec from %s ...\n", openapiURL)
			routes, err := wordlist.FetchFromURL(openapiURL)
			if err != nil {
				return fmt.Errorf("openapi-url: %w", err)
			}
			fmt.Printf("  Loaded %d routes from spec\n", len(routes))
			_ = routes // routes will feed into the scanner once implemented
		}

		if openapiFile != "" {
			fmt.Printf("Loading OpenAPI spec from %s ...\n", openapiFile)
			routes, err := wordlist.FetchFromFile(openapiFile)
			if err != nil {
				return fmt.Errorf("openapi-file: %w", err)
			}
			fmt.Printf("  Loaded %d routes from spec\n", len(routes))
			_ = routes // routes will feed into the scanner once implemented
		}

		_ = wordlistFiles
		_ = headN

		fmt.Println("ks scan: not yet implemented")
		return nil
	},
}

func init() {
	scanCmd.Flags().IntP("depth", "d", 2, "crawl depth for context discovery")
	scanCmd.Flags().StringArrayP("wordlist", "w", nil, "wordlist file(s) to use (.ks, .txt, or .json); repeatable")
	scanCmd.Flags().StringArrayP("wordlist-alias", "A", nil, "cached wordlist alias, e.g. apiroutes or apiroutes:20000; repeatable")
	scanCmd.Flags().Int("head", 0, "use only the first N routes from each wordlist (0 = all)")
	scanCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")
	scanCmd.Flags().IntP("threads", "t", 10, "number of concurrent threads")
	scanCmd.Flags().BoolP("follow-redirects", "r", true, "follow HTTP redirects")
	scanCmd.Flags().StringP("seclists", "S", "", "SecLists alias to fetch on demand and use as wordlist (e.g. api-endpoints)")
	scanCmd.Flags().String("openapi-url", "", "fetch and use an OpenAPI/Swagger spec from URL at scan time (no caching)")
	scanCmd.Flags().String("openapi-file", "", "load and use a local OpenAPI/Swagger spec file at scan time")
}
