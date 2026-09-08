package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/service"
)

// TestAttachmentsFromFlags_CombinedCapCheckedBeforeUpload is the regression
// for uploading files that are then abandoned: the per-message cap counts
// --attach and --attachment together, so an over-limit call has to be
// rejected before anything reaches the network.
func TestAttachmentsFromFlags_CombinedCapCheckedBeforeUpload(t *testing.T) {
	cmd := &cobra.Command{Use: "reply"}
	cmd.Flags().StringArray("attach", nil, "")
	cmd.Flags().StringArray("attachment", nil, "")

	for i := 0; i < 6; i++ {
		_ = cmd.Flags().Set("attach", "file.txt")
		_ = cmd.Flags().Set("attachment", "11111111-1111-1111-1111-111111111111")
	}

	uploaded := false
	_, _, err := attachmentsFromFlags(context.Background(), cmd, func(context.Context, []string) ([]string, error) {
		uploaded = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected the combined cap to reject the call")
	}
	if !strings.Contains(err.Error(), "at most 10") {
		t.Errorf("error should name the limit, got %v", err)
	}
	if uploaded {
		t.Error("nothing may be uploaded once the call is over the cap")
	}
}

func TestAttachmentsFromFlags_UuidsPassThroughWithoutUploading(t *testing.T) {
	cmd := &cobra.Command{Use: "reply"}
	cmd.Flags().StringArray("attach", nil, "")
	cmd.Flags().StringArray("attachment", nil, "")
	_ = cmd.Flags().Set("attachment", "11111111-1111-1111-1111-111111111111")

	uploaded := false
	got, _, err := attachmentsFromFlags(context.Background(), cmd, func(context.Context, []string) ([]string, error) {
		uploaded = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("attachmentsFromFlags: %v", err)
	}
	if uploaded {
		t.Error("no upload should happen when only uuids are given")
	}
	if len(got) != 1 || got[0] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("uuids = %v", got)
	}
}

// TestSupportListOrgFlag_KeepsShorthand guards a pflag subtlety: a local
// flag named "org" suppresses inheritance of the root's persistent --org/-o
// for that command, shorthand included. On `support list` the long name is
// deliberately repurposed as a filter, so the shorthand has to be declared
// locally or `support list -o acme` fails to parse everywhere else in the
// CLI it works.
func TestSupportListOrgFlag_KeepsShorthand(t *testing.T) {
	flag := supportListCmd.Flags().Lookup("org")
	if flag == nil {
		t.Fatal("support list has no --org flag")
	}
	if flag.Shorthand != "o" {
		t.Errorf("--org shorthand = %q, want \"o\"", flag.Shorthand)
	}
	if err := supportListCmd.Flags().Parse([]string{"-o", "acme"}); err != nil {
		t.Fatalf("-o must parse: %v", err)
	}
	if got, _ := supportListCmd.Flags().GetString("org"); got != "acme" {
		t.Errorf("--org = %q, want acme", got)
	}
	_ = supportListCmd.Flags().Set("org", "")
}

// TestNamedOverwriteFlag_RestoresTheCliWording covers the split introduced
// so the two surfaces can each give actionable advice: the service message
// is neutral because an MCP caller has no flags, and the CLI puts its flag
// name back.
func TestNamedOverwriteFlag_RestoresTheCliWording(t *testing.T) {
	// Built from the shared constant, not a copy of the wording: if the
	// service message drifts, strings.Replace would silently no-op and the
	// CLI would quietly print the MCP phrasing instead.
	err := namedOverwriteFlag(&service.Error{
		Code:    service.CodeDestinationExists,
		Message: "/tmp/out.txt already exists; " + service.OverwriteHint,
	})
	if !strings.Contains(err.Error(), "pass --overwrite to replace it") {
		t.Errorf("the CLI should name its flag, got %q", err)
	}
	if !strings.Contains(err.Error(), "/tmp/out.txt") {
		t.Errorf("the path must survive the rewrite, got %q", err)
	}

	// Any other failure passes through untouched.
	other := &service.Error{Code: service.CodeAPIError, Message: "checksum mismatch"}
	if got := namedOverwriteFlag(other); got != error(other) {
		t.Errorf("unrelated errors must pass through, got %v", got)
	}
}
