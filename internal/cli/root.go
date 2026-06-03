package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	output  string
	quiet   bool
	verbose string
)

var rootCmd = &cobra.Command{
	Use:   "ks",
	Short: "KiteString — context-aware API recon and fuzzing tool",
	Long: `KiteString is a security research tool for discovering API endpoints,
fuzzing paths, and replaying captured HTTP requests.

Use 'ks <command> --help' for more information about a command.`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: $HOME/.kitestring.yaml)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "pretty", "output format: pretty, text, jsonl")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress decorative output")
	rootCmd.PersistentFlags().StringVarP(&verbose, "verbose", "v", "info", "verbosity level: error, info, debug, trace")

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(bruteCmd)
	rootCmd.AddCommand(wordlistCmd)
	rootCmd.AddCommand(replayCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(profileCmd)
	rootCmd.AddCommand(completionCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kitestring")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		if !quiet {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}
