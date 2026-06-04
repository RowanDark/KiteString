package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate a shell completion script for KiteString (ks) and print it to stdout.

Bash:
  # Load for current session:
  source <(ks completion bash)

  # Load permanently (requires bash-completion package):
  ks completion bash > /etc/bash_completion.d/ks

Zsh:
  # If shell completion is not already enabled, enable it first:
  echo "autoload -U compinit; compinit" >> ~/.zshrc

  # Then install the completion script:
  ks completion zsh > "${fpath[1]}/_ks"

Fish:
  # Load for current session:
  ks completion fish | source

  # Load permanently:
  ks completion fish > ~/.config/fish/completions/ks.fish

PowerShell:
  ks completion powershell | Out-String | Invoke-Expression

Examples:
  ks completion bash > /etc/bash_completion.d/ks
  ks completion zsh > "${fpath[1]}/_ks"
  ks completion fish > ~/.config/fish/completions/ks.fish`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell %q: choose bash, zsh, fish, or powershell", args[0])
		}
	},
}
