package cli

import "testing"

// TestPrefixAddValueFlagIsWired guards a silent failure mode rather than a
// loud one. runNetworkPrefixAdd reads the flag with
// cmd.Flags().GetStringSlice("value") and ignores the error, so a rename or a
// typo on either side does not panic or fail — it yields an empty slice, the
// --value branch never fires, and the command quietly falls back to the old
// "the variable must already exist" path. Nothing else in the suite would
// notice, because every other test calls the service directly.
func TestPrefixAddValueFlagIsWired(t *testing.T) {
	prepareRootCommand()

	cmd, _, err := rootCmd.Find([]string{"network", "prefix", "add"})
	if err != nil {
		t.Fatalf("find network prefix add: %v", err)
	}

	flag := cmd.Flags().Lookup("value")
	if flag == nil {
		t.Fatal("the --value flag is not declared; the provisioning branch can never fire")
	}
	// StringSlice, not StringArray: the stored value is comma-separated, so
	// `--value a,b` has to read as two CIDRs.
	if flag.Value.Type() != "stringSlice" {
		t.Errorf("--value type = %q, want stringSlice", flag.Value.Type())
	}

	if _, err := cmd.Flags().GetStringSlice("value"); err != nil {
		t.Errorf("reading --value as a string slice failed: %v", err)
	}
}
