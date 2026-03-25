package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/update"
)

var (
	cfgFile   string
	orgName   string
	outputFmt string
)

// Global instances
var (
	authManager *auth.Manager
	apiClient   *api.Client
	formatter   output.Formatter
)

var rootCmd = &cobra.Command{
	Use:   "ndcli",
	Short: "NetDefense Command Line Interface",
	Long: `NDCLI is a command-line interface for managing NetDefense firewall infrastructure.

It provides commands for managing devices, organizations, templates, and more.`,
	SilenceUsage: true,
	// Run is called when no subcommand is provided
	Run: func(cmd *cobra.Command, args []string) {
		// If no config exists, auto-trigger setup wizard
		if !config.ConfigExists() {
			runSetup(cmd, args)
			return
		}
		// Otherwise show help
		cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for help, version, and setup commands
		if cmd.Name() == "help" || cmd.Name() == "version" || cmd.Name() == "setup" {
			return nil
		}

		// Load configuration
		if err := config.Load(cfgFile); err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Initialize auth manager
		authManager = auth.GetManager()

		// Initialize API client
		apiClient = api.NewClientFromConfig(authManager)

		// Initialize timezone from config
		timezone := config.Get().Output.Timezone
		if timezone == "" {
			timezone = config.DefaultTimezone
		}
		if err := output.SetTimezone(timezone); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Invalid timezone '%s', using system local\n", timezone)
			output.SetTimezone("Local")
		}

		// Determine output format: flag > config > default
		format := outputFmt
		if format == "" {
			format = config.Get().Output.Format
		}
		if format == "" {
			format = config.DefaultOutputFormat
		}

		// Initialize formatter
		formatter = output.GetFormatter(format)

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Clean up auth manager
		if authManager != nil {
			authManager.Close()
		}

		// Display version update notification if needed
		if msg := update.GetUpdateNotification(); msg != "" {
			fmt.Fprint(os.Stderr, msg)
		}
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Base usage template without global flags
	usageTemplate := `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
	rootCmd.SetUsageTemplate(usageTemplate)

	// Custom help function that only shows global flags when --help was explicitly used
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		// Check if help flag was explicitly set
		helpFlag := cmd.Flags().Lookup("help")
		showGlobalFlags := helpFlag != nil && helpFlag.Changed

		// Print description
		if cmd.Long != "" {
			fmt.Println(cmd.Long)
		} else if cmd.Short != "" {
			fmt.Println(cmd.Short)
		}
		fmt.Println()

		// Print usage
		fmt.Print(cmd.UsageString())

		// Only show global flags if --help was explicitly used
		if showGlobalFlags && cmd.HasAvailableInheritedFlags() {
			fmt.Println()
			fmt.Println("Global Flags:")
			fmt.Print(cmd.InheritedFlags().FlagUsages())
		}
	})

	// Persistent flags (available to all commands)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "conf", "",
		"config file path")
	rootCmd.PersistentFlags().StringVarP(&orgName, "org", "o", "",
		"organization name (overrides config)")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "format", "f", "",
		"output format: table, simple, detailed, json")

	// Add subcommands
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(deviceCmd)
	rootCmd.AddCommand(networkCmd)
	rootCmd.AddCommand(orgCmd)
	rootCmd.AddCommand(ouCmd)
	rootCmd.AddCommand(taskCmd)
	rootCmd.AddCommand(templateCmd)
	rootCmd.AddCommand(snippetCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(variableCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(setupCmd)
}

// getOrganization returns the organization from flag or config
func getOrganization() string {
	if orgName != "" {
		return orgName
	}
	return config.Get().Organization.Name
}

// requireOrganization returns the organization or exits with an error
func requireOrganization() string {
	org := getOrganization()
	if org == "" {
		fmt.Fprintln(os.Stderr, "Error: Organization is required. Use --org flag or set in config.")
		os.Exit(1)
	}
	return org
}

// requireAuth checks if the user is authenticated, attempting token refresh if needed
func requireAuth() {
	// Use GetAccessToken() which attempts token refresh if expired
	_, err := authManager.GetAccessToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: Not authenticated. Please run 'ndcli auth login' first.")
		os.Exit(1)
	}
}

// versionCmd displays version information
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ndcli version %s\n", config.Version)
		fmt.Printf("Build time: %s\n", config.BuildTime)
	},
}
