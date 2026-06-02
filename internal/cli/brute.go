package cli

import (
	"fmt"

	"github.com/RowanDark/kitestring/internal/wordlist"
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
		wordlistFiles, _ := cmd.Flags().GetStringArray("wordlist")
		headN, _ := cmd.Flags().GetInt("head")
		seclistsAlias, _ := cmd.Flags().GetString("seclists")

		// Resolve --seclists alias, fetching and caching on demand.
		if seclistsAlias != "" {
			path, err := wordlist.ResolveSecList(seclistsAlias)
			if err != nil {
				return err
			}
			wordlistFiles = append(wordlistFiles, path)
		}

		_ = wordlistFiles
		_ = headN

		fmt.Println("ks brute: not yet implemented")
		return nil
	},
}

func init() {
	bruteCmd.Flags().StringArrayP("wordlist", "w", nil, "wordlist file(s) to use (.ks, .txt, or .json); repeatable")
	bruteCmd.Flags().Int("head", 0, "use only the first N routes from each wordlist (0 = all)")
	bruteCmd.Flags().StringP("status", "s", "200,204,301,302,307,401,403", "comma-separated status codes to report")
	bruteCmd.Flags().IntP("threads", "t", 40, "number of concurrent threads")
	bruteCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")
	bruteCmd.Flags().StringP("extension", "e", "", "append extensions to each wordlist entry (e.g. .php,.html)")
	bruteCmd.Flags().BoolP("follow-redirects", "r", false, "follow HTTP redirects")
	bruteCmd.Flags().IntP("timeout", "", 10, "request timeout in seconds")
	bruteCmd.Flags().StringP("seclists", "S", "", "SecLists alias to fetch on demand and use as wordlist (e.g. api-endpoints)")
}
