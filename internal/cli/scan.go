package cli

import (
	"fmt"

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
  ks scan https://api.example.com/v1 --depth 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks scan: not yet implemented")
		return nil
	},
}

func init() {
	scanCmd.Flags().IntP("depth", "d", 2, "crawl depth for context discovery")
	scanCmd.Flags().StringP("wordlist", "w", "", "supplemental wordlist to use during scan")
	scanCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")
	scanCmd.Flags().IntP("threads", "t", 10, "number of concurrent threads")
	scanCmd.Flags().BoolP("follow-redirects", "r", true, "follow HTTP redirects")
}
