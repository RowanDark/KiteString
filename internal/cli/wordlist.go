package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var wordlistCmd = &cobra.Command{
	Use:   "wordlist",
	Short: "Wordlist management",
	Long: `Manage KiteString wordlists (.ks format).

Subcommands allow listing installed wordlists, pulling the latest curated
lists from the CDN, and compiling plain .txt or .json files into the
optimized .ks binary format.`,
}

var wordlistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available wordlists",
	Long: `List all wordlists currently installed in the KiteString wordlist directory.

Displays name, entry count, size, and last-updated timestamp for each file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks wordlist list: not yet implemented")
		return nil
	},
}

var wordlistUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Pull latest .ks wordlists from GitHub CDN",
	Long: `Download the latest curated .ks wordlist files from the KiteString CDN.

Only files newer than the locally cached version are downloaded. Use --force
to re-download all files regardless of local state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks wordlist update: not yet implemented")
		return nil
	},
}

var wordlistCompileCmd = &cobra.Command{
	Use:   "compile [file]",
	Short: "Compile .txt or .json into .ks format",
	Long: `Compile a plain-text (.txt) or JSON (.json) wordlist into the optimized
.ks binary format for faster loading and deduplication.

Example:
  ks wordlist compile mylist.txt
  ks wordlist compile mylist.json --output mylist.ks`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("ks wordlist compile: not yet implemented")
		return nil
	},
}

func init() {
	wordlistUpdateCmd.Flags().BoolP("force", "f", false, "re-download all wordlists regardless of local state")
	wordlistUpdateCmd.Flags().StringP("dir", "d", "", "target directory for downloaded wordlists")

	wordlistCompileCmd.Flags().StringP("output", "o", "", "output .ks file path (default: <input>.ks)")

	wordlistCmd.AddCommand(wordlistListCmd)
	wordlistCmd.AddCommand(wordlistUpdateCmd)
	wordlistCmd.AddCommand(wordlistCompileCmd)
}
