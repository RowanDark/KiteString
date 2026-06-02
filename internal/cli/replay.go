package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
	Use:   "replay [file]",
	Short: "Reconstruct and replay a captured HTTP request",
	Long: `Replay a captured HTTP request from a .ks capture file or raw request text.

Supports modifying headers, body parameters, and query strings before
replaying. Useful for manual testing after an automated scan has identified
interesting endpoints.

Example:
  ks replay capture.ks
  ks replay capture.ks --header "Authorization: Bearer <token>"
  ks replay capture.ks --param "id=99"`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks replay: not yet implemented")
		return nil
	},
}

func init() {
	replayCmd.Flags().StringArrayP("header", "H", nil, "add or override a request header (can be used multiple times)")
	replayCmd.Flags().StringArrayP("param", "P", nil, "add or override a query/body parameter")
	replayCmd.Flags().StringP("proxy", "p", "", "HTTP proxy URL")
	replayCmd.Flags().BoolP("follow-redirects", "r", true, "follow HTTP redirects")
	replayCmd.Flags().IntP("times", "n", 1, "number of times to replay the request")
}
