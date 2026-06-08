package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/JulienTant/blogwatcher-cli/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "blogwatcher-cli",
		Short:         "BlogWatcher - Track blog articles and detect new posts.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig(cmd)
		},
	}
	rootCmd.Version = version.Version
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	rootCmd.PersistentFlags().String("db", "", "Path to the SQLite database file (default: ~/.blogwatcher-cli/blogwatcher-cli.db)")
	rootCmd.PersistentFlags().Bool("unsafe-client", false, "Disable SSRF protection (allow requests to private/loopback IPs)")
	rootCmd.PersistentFlags().String("format", outputFormatText, "Output format: text or json")

	rootCmd.AddCommand(newAddCommand())
	rootCmd.AddCommand(newRemoveCommand())
	rootCmd.AddCommand(newBlogsCommand())
	rootCmd.AddCommand(newScanCommand())
	rootCmd.AddCommand(newArticlesCommand())
	rootCmd.AddCommand(newReadCommand())
	rootCmd.AddCommand(newReadAllCommand())
	rootCmd.AddCommand(newUnreadCommand())
	rootCmd.AddCommand(newImportCommand())
	return rootCmd
}

func initConfig(cmd *cobra.Command) error {
	viper.SetEnvPrefix("BLOGWATCHER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	if err := viper.BindPFlags(cmd.Flags()); err != nil {
		return err
	}
	return validateOutputFormat(viper.GetString("format"))
}

func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		if !isPrinted(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
