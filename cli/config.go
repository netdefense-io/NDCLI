package cli

import (
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management commands",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration",
	RunE:  runConfigShow,
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration to defaults",
	RunE:  runConfigReset,
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set configuration values",
}

var configSetOrgCmd = &cobra.Command{
	Use:               "org [name]",
	Short:             "Set default organization",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeOrganizations,
	RunE:              runConfigSetOrg,
}

var configSetOutputCmd = &cobra.Command{
	Use:   "output [format]",
	Short: "Set default output format (table, simple, detailed, json)",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return []string{"table", "simple", "detailed", "json"}, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: runConfigSetOutput,
}

var configSetTimezoneCmd = &cobra.Command{
	Use:   "timezone [timezone]",
	Short: "Set display timezone (e.g., UTC, America/New_York, Local)",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Common timezones for shell completion
		return []string{
			"Local",
			"UTC",
			"America/New_York",
			"America/Chicago",
			"America/Denver",
			"America/Los_Angeles",
			"America/Detroit",
			"America/Toronto",
			"Europe/London",
			"Europe/Paris",
			"Europe/Berlin",
			"Asia/Tokyo",
			"Asia/Shanghai",
			"Australia/Sydney",
		}, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: runConfigSetTimezone,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configSetCmd)

	configSetCmd.AddCommand(configSetOrgCmd)
	configSetCmd.AddCommand(configSetOutputCmd)
	configSetCmd.AddCommand(configSetTimezoneCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg := config.Get()

	color.Cyan("Configuration:")
	fmt.Printf("  Config file: %s\n", config.GetConfigFilePath())
	fmt.Printf("  Auth file:   %s\n", config.GetAuthFilePath())
	fmt.Println()

	color.Cyan("Controlplane:")
	fmt.Printf("  Host:       %s\n", cfg.Controlplane.Host)
	fmt.Printf("  SSL Verify: %v\n", cfg.Controlplane.SSLVerify)
	fmt.Println()

	color.Cyan("Pathfinder:")
	if cfg.Pathfinder.Host != "" {
		fmt.Printf("  Host:       %s\n", cfg.Pathfinder.Host)
		fmt.Printf("  SSL Verify: %v\n", cfg.Pathfinder.SSLVerify)
	} else {
		fmt.Println("  Host:       (not configured)")
	}
	fmt.Println()

	color.Cyan("Organization:")
	if cfg.Organization.Name != "" {
		fmt.Printf("  Name: %s\n", cfg.Organization.Name)
	} else {
		fmt.Println("  Name: (not set)")
	}
	fmt.Println()

	color.Cyan("Output:")
	outputFormat := cfg.Output.Format
	if outputFormat == "" {
		outputFormat = config.DefaultOutputFormat
	}
	fmt.Printf("  Format:   %s\n", outputFormat)
	timezone := cfg.Output.Timezone
	if timezone == "" {
		timezone = config.DefaultTimezone
	}
	fmt.Printf("  Timezone: %s\n", timezone)

	return nil
}

func runConfigReset(cmd *cobra.Command, args []string) error {
	if err := config.CreateDefaultConfig(); err != nil {
		return fmt.Errorf("failed to reset configuration: %w", err)
	}

	color.Green("✓ Configuration reset to defaults")
	fmt.Printf("  Config file: %s\n", config.GetConfigFilePath())
	return nil
}

func runConfigSetOrg(cmd *cobra.Command, args []string) error {
	orgName := args[0]

	if err := config.UpdateValue("organization.name", orgName); err != nil {
		return fmt.Errorf("failed to set organization: %w", err)
	}

	color.Green("✓ Default organization set to: %s", orgName)
	return nil
}

func runConfigSetOutput(cmd *cobra.Command, args []string) error {
	format := args[0]

	// Validate format
	valid := false
	for _, f := range config.ValidOutputFormats {
		if format == f {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid format: %s. Must be one of: table, simple, detailed, json", format)
	}

	if err := config.UpdateValue("output.format", format); err != nil {
		return fmt.Errorf("failed to set output format: %w", err)
	}

	color.Green("✓ Default output format set to: %s", format)
	return nil
}

func runConfigSetTimezone(cmd *cobra.Command, args []string) error {
	tz := args[0]

	// Validate timezone by trying to load it
	if tz != "Local" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid timezone: %s. Use IANA timezone names (e.g., America/New_York, UTC, Local)", tz)
		}
	}

	if err := config.UpdateValue("output.timezone", tz); err != nil {
		return fmt.Errorf("failed to set timezone: %w", err)
	}

	color.Green("✓ Display timezone set to: %s", tz)
	return nil
}
