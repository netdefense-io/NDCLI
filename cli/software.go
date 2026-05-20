package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	internalHelpers "github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Software policies are NetDefense's reusable package inventory: a named
// pair of {present, absent} package lists that attach to templates and
// drive the sync flow to install/uninstall OPNsense plugins and FreeBSD
// packages on each device.

var softwareCmd = &cobra.Command{
	Use:   "software",
	Short: "Software policy management commands",
	Long: `Manage reusable lists of OPNsense plugins / FreeBSD packages
that NetDefense will install or uninstall on devices when their
template attaches the policy.`,
}

var softwareListCmd = &cobra.Command{
	Use:   "list",
	Short: "List software policies",
	RunE:  runSoftwareList,
}

var softwareCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new software policy",
	Args:  cobra.ExactArgs(1),
	RunE:  runSoftwareCreate,
}

var softwareDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show software policy details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareDescribe,
}

var softwareEditCmd = &cobra.Command{
	Use:               "edit [name]",
	Short:             "Edit software policy content in an external editor",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareEdit,
}

var softwareUpdateContentCmd = &cobra.Command{
	Use:               "update-content [name] [file]",
	Short:             "Update software policy content from a file",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareUpdateContent,
}

var softwareRenameCmd = &cobra.Command{
	Use:               "rename [name] [new-name]",
	Short:             "Rename a software policy",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareRename,
}

var softwareDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete a software policy",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareDelete,
}

var softwareRequirePackageCmd = &cobra.Command{
	Use:   "require-package [policy] [package...]",
	Short: "Mark one or more packages as required by a software policy",
	Long: `Mark one or more packages as required by a software policy.

Required packages are installed on every device that picks up the policy
through its templates. A package already required is a no-op; a package
currently blocked by the same policy is moved (block → require) with a
notice.`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareRequirePackage,
}

var softwareBlockPackageCmd = &cobra.Command{
	Use:   "block-package [policy] [package...]",
	Short: "Mark one or more packages as blocked by a software policy",
	Long: `Mark one or more packages as blocked by a software policy.

Blocked packages are uninstalled on every device that picks up the policy
through its templates. A package already blocked is a no-op; a package
currently required by the same policy is moved (require → block) with a
notice.`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareBlockPackage,
}

var softwareWaivePackageCmd = &cobra.Command{
	Use:   "waive-package [policy] [package...]",
	Short: "Stop having an opinion about one or more packages",
	Long: `Stop having an opinion about one or more packages.

Removes each package from whichever list (required or blocked) it sits
in. A package not specified in either list is a no-op. Devices keep
whatever they currently have installed — waive does not uninstall or
re-install anything.`,
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareWaivePackage,
}

func init() {
	softwareCmd.AddCommand(softwareListCmd)
	softwareCmd.AddCommand(softwareCreateCmd)
	softwareCmd.AddCommand(softwareDescribeCmd)
	softwareCmd.AddCommand(softwareEditCmd)
	softwareCmd.AddCommand(softwareUpdateContentCmd)
	softwareCmd.AddCommand(softwareRenameCmd)
	softwareCmd.AddCommand(softwareDeleteCmd)
	softwareCmd.AddCommand(softwareRequirePackageCmd)
	softwareCmd.AddCommand(softwareBlockPackageCmd)
	softwareCmd.AddCommand(softwareWaivePackageCmd)

	softwareListCmd.Flags().String("name", "", "Filter by name (regex pattern)")
	softwareListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction (name, created_at, updated_at)")
	softwareListCmd.Flags().Int("page", 1, "Page number")
	softwareListCmd.Flags().Int("per-page", 50, "Items per page (max 100)")

	softwareCreateCmd.Flags().String("content", "", `Optional inline JSON content for bulk seed. When omitted, the policy is created empty ({"present":[],"absent":[]}) and you fill it with require-package / block-package.`)
	softwareCreateCmd.Flags().String("file", "", "Read content from a file instead of --content (bulk-seed alternative)")
}

func runSoftwareList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.SoftwarePolicyListOpts{}
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")

	result, err := svc.SoftwarePolicyList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatSoftwarePolicies(result.Policies); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runSoftwareCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	content, _ := cmd.Flags().GetString("content")
	file, _ := cmd.Flags().GetString("file")
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	}
	emptyDefault := false
	if content == "" {
		// Empty-by-default is the whole point of the require/block/waive
		// surface — the operator doesn't need to know the JSON shape.
		content = models.EmptySoftwarePolicyContent
		emptyDefault = true
	}

	if _, err := svc.SoftwarePolicyCreate(context.Background(), org, service.SoftwarePolicyCreateOpts{
		Name:    name,
		Content: content,
	}); err != nil {
		return err
	}
	color.Green("✓ Software policy created: %s", name)
	if emptyDefault {
		fmt.Printf("  Use 'ndcli software require-package %s <pkg>' or 'block-package %s <pkg>' to populate it.\n", name, name)
	}
	return nil
}

func runSoftwareDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	sp, err := svc.SoftwarePolicyGet(context.Background(), org, name)
	if err != nil {
		return err
	}
	if err := formatter.FormatSoftwarePolicy(sp); err != nil {
		return err
	}
	fmt.Println()
	color.Cyan("Content:")
	fmt.Println(internalHelpers.PrettyJSON(sp.Content))
	return nil
}

func runSoftwareEdit(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	ctx := context.Background()
	sp, err := svc.SoftwarePolicyGet(ctx, org, name)
	if err != nil {
		return err
	}

	pretty := internalHelpers.PrettyJSON(sp.Content)
	edited, err := internalHelpers.EditContent(pretty, ".json")
	if err != nil {
		return fmt.Errorf("failed to edit content: %w", err)
	}
	// Minify and compare against the server's minified form so a pure
	// re-indent is treated as a no-op.
	newContent := internalHelpers.MinifyJSON(edited)
	if newContent == internalHelpers.MinifyJSON(sp.Content) {
		fmt.Println("No changes made")
		return nil
	}
	if err := svc.SoftwarePolicyUpdateContent(ctx, org, name, newContent); err != nil {
		return err
	}
	color.Green("✓ Software policy updated: %s", name)
	return nil
}

func runSoftwareUpdateContent(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	file := args[1]

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	if err := svc.SoftwarePolicyUpdateContent(context.Background(), org, name, string(data)); err != nil {
		return err
	}
	color.Green("✓ Software policy content updated: %s", name)
	return nil
}

func runSoftwareRename(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name, newName := args[0], args[1]
	if err := svc.SoftwarePolicyRename(context.Background(), org, name, newName); err != nil {
		return err
	}
	color.Green("✓ Software policy renamed: %s -> %s", name, newName)
	return nil
}

func runSoftwareDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	if !internalHelpers.Confirm(fmt.Sprintf("Delete software policy '%s'?", name)) {
		fmt.Println("Cancelled")
		return nil
	}
	if err := svc.SoftwarePolicyDelete(context.Background(), org, name); err != nil {
		return err
	}
	color.Green("✓ Software policy deleted: %s", name)
	return nil
}

func runSoftwareRequirePackage(cmd *cobra.Command, args []string) error {
	return runSoftwareMutate(cmd, args, "require")
}

func runSoftwareBlockPackage(cmd *cobra.Command, args []string) error {
	return runSoftwareMutate(cmd, args, "block")
}

func runSoftwareWaivePackage(cmd *cobra.Command, args []string) error {
	return runSoftwareMutate(cmd, args, "waive")
}

// runSoftwareMutate is the shared body for require-package /
// block-package / waive-package. Variadic args after the policy name
// each become one outcome; the API round-trip is skipped if every
// outcome is a no-op.
func runSoftwareMutate(cmd *cobra.Command, args []string, op string) error {
	requireAuth()
	org := requireOrganization()

	policy := args[0]
	packages := args[1:]
	ctx := context.Background()

	var (
		outcomes []models.PackageActionOutcome
		err      error
	)
	switch op {
	case "require":
		outcomes, err = svc.SoftwarePolicyRequirePackages(ctx, org, policy, packages)
	case "block":
		outcomes, err = svc.SoftwarePolicyBlockPackages(ctx, org, policy, packages)
	case "waive":
		outcomes, err = svc.SoftwarePolicyWaivePackages(ctx, org, policy, packages)
	default:
		return fmt.Errorf("internal error: unknown op %q", op)
	}
	if err != nil {
		// The local mutation was rolled back the moment the PUT was
		// rejected (we never re-fetch and the in-memory struct goes
		// out of scope). Don't render the outcomes here — a green
		// "✓ Required: …" line above an Error message would imply the
		// change landed when it didn't.
		return err
	}
	renderPackageOutcomes(outcomes)
	return nil
}

// renderPackageOutcomes prints one line per outcome plus a trailing
// summary. Output is deliberately stable so it's easy to scan in a
// transcript and to grep in a script.
func renderPackageOutcomes(outcomes []models.PackageActionOutcome) {
	changed, moved, noop := 0, 0, 0
	for _, o := range outcomes {
		switch o.Action {
		case "required":
			color.Green("✓ Required: %s", o.Package)
			changed++
		case "blocked":
			color.Green("✓ Blocked: %s", o.Package)
			changed++
		case "waived":
			color.Green("✓ Waived: %s (was: %s)", o.Package, o.From)
			changed++
		case "moved":
			// Marshal "moved" as the new state in the verb, with a
			// trailing arrow to the prior state. Reads naturally.
			//   ↻ Required: bash (was: blocked)
			//   ↻ Blocked: nano (was: required)
			newState := "Required"
			if o.From == models.PackageStateRequired {
				newState = "Blocked"
			}
			color.Yellow("↻ %s: %s (was: %s)", newState, o.Package, o.From)
			moved++
		case "no-change":
			if o.From != "" {
				color.Cyan("ℹ %s: already %s (no change)", o.Package, o.From)
			} else {
				color.Cyan("ℹ %s: not specified (no change)", o.Package)
			}
			noop++
		}
	}
	total := changed + moved + noop
	if total == 0 {
		return
	}
	if changed+moved == 0 {
		fmt.Println("No changes.")
		return
	}
	fmt.Printf("Applied %d change(s)", changed+moved)
	if noop > 0 {
		fmt.Printf(" (%d no-op)", noop)
	}
	fmt.Println(".")
}
