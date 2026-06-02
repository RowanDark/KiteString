package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var bruteCmd = &cobra.Command{
	Use:   "brute [url]",
	Short: "Traditional path/directory fuzzing",
	Long: `Brute-force paths and directories against a target URL using a wordlist.

Unlike 'ks scan', brute mode does not perform context analysis — it sends
requests for each entry in the wordlist and reports responses based on
configured status code filters.

Example:
  ks brute https://example.com -w wordlists/common.ks
  ks brute https://api.example.com/v1 -w wordlists/api.ks --status 200,301`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks brute: not yet implemented")
		return nil
	},
}

func init() {
	bruteCmd.Flags().StringP("wordlist", "w", "", "wordlist file to use (.ks, .txt, or .json)")
	bruteCmd.Flags().StringP("status", "s", "200,204,301,302,307,401,403", "comma-separated status codes to report")
	bruteCmd.Flags().IntP("threads", "t", 40, "number of concurrent threads")
	bruteCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")
	bruteCmd.Flags().StringP("extension", "e", "", "append extensions to each wordlist entry (e.g. .php,.html)")
	bruteCmd.Flags().BoolP("follow-redirects", "r", false, "follow HTTP redirects")
	bruteCmd.Flags().IntP("timeout", "", 10, "request timeout in seconds")
}
