package cli

import (
	"regexp"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestHelpNamesNoControlPlaneModule: help text says what a part of NetDefense
// does, not the module behind it; device connect said "via Pathfinder". A
// literal config key (pathfinder.ssl_verify, NDCLI_PATHFINDER_HOST) is a name
// the user types, so it stays.
func TestHelpNamesNoControlPlaneModule(t *testing.T) {
	literal := regexp.MustCompile(`pathfinder\.(?:host|ssl_verify)\b|NDCLI_PATHFINDER_(?:HOST|SSL_VERIFY)\b`)
	module := regexp.MustCompile(`(?i)ndmanager|ndbroker|ndpathfinder|pathfinder`)
	names := func(text string) string { return module.FindString(literal.ReplaceAllString(text, "")) }
	check := func(where, text string) {
		if m := names(text); m != "" {
			t.Errorf("%s names %q:\n%s", where, m, text)
		}
	}

	// Only the config keys are excused, not a host name that starts like one.
	if names("pathfinder.ssl_verify=false, pathfinder.host, NDCLI_PATHFINDER_SSL_VERIFY") != "" ||
		names("wss://pathfinder.netdefense.io") == "" || names("pathfinder.hostname") == "" {
		t.Fatal("the config-key exception excuses something other than the config keys")
	}

	prepareRootCommand()
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		path := c.CommandPath()
		check(path, c.Short)
		check(path, c.Long)
		check(path, c.Example)
		visit := func(f *pflag.Flag) { check(path+" --"+f.Name, f.Usage) }
		c.Flags().VisitAll(visit)
		c.PersistentFlags().VisitAll(visit)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	connect := findCommand(t, "device", "connect")
	if connect.Short != "Connect to a device through the connection relay" {
		t.Errorf("device connect short = %q", connect.Short)
	}
}
