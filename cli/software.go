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

var softwareAddRepoCmd = &cobra.Command{
	Use:   "add-repo [policy]",
	Short: "Add or update a custom package repository in a software policy",
	Long: `Add or update a custom package repository in a software policy.

Repositories are configured on the device before any package work
happens, so packages served only by a custom repository can be named in
require-package. Re-running with identical values is a no-op, which
makes this safe to drive from a script.

The repository is identified by --name; running add-repo again with the
same name updates that entry in place rather than adding a second one.
Flags you leave out keep their stored values, so you can change a URL
without restating the priority or re-enabling a repository you disabled.
--url and the signature flags are required only when adding a repository
that does not exist yet.

Signature configuration is required when adding a repository. It is
inferred from the flags you pass (--fingerprint implies fingerprints,
--pubkey-file implies pubkey); an unverified repository must be
requested explicitly with --signature-type none, and the organization
must have unverified repositories enabled for the server to accept it.`,
	Example: `  ndcli software add-repo baseline --name mimugmail \
    --url 'https://opn-repo.routerperformance.net/repo/${ABI}' \
    --priority 5 --fingerprint sha256:<64-hex>`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareAddRepo,
}

var softwareRemoveRepoCmd = &cobra.Command{
	Use:   "remove-repo [policy] [repository]",
	Short: "Remove a custom package repository from a software policy",
	Long: `Remove a custom package repository from a software policy.

The repository configuration is removed from every device the policy
reaches on the next sync. Packages already installed from it stay
installed — removing a repository is not an uninstall.`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSoftwarePolicyRepos,
	RunE:              runSoftwareRemoveRepo,
}

var softwareAddExternalCmd = &cobra.Command{
	Use:   "add-external [policy]",
	Short: "Add or update an external (URL-installed) package in a software policy",
	Long: `Add or update an external package in a software policy.

External packages are installed straight from a URL with pkg add, for
software that is not served by any repository. --version and --url are
required when adding a package that does not exist yet: pkg add takes a
URL, not a package identity, so the policy has to state which package
the URL is expected to deliver for the agent to tell "already installed"
from "not yet installed".

The entry is identified by --name; the same name at a new --version
updates the entry rather than adding a second one. Flags you leave out
keep their stored values.`,
	Example: `  ndcli software add-external baseline --name ookla-speedtest --version 1.2.0 \
    --url https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-freebsd13-x86_64.pkg`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSoftwarePolicies,
	RunE:              runSoftwareAddExternal,
}

var softwareRemoveExternalCmd = &cobra.Command{
	Use:   "remove-external [policy] [package]",
	Short: "Remove an external package from a software policy",
	Long: `Remove an external package from a software policy.

The device stops being asked to install it. Like remove-repo, this is
not an uninstall — an already-installed package stays installed. Use
block-package if you want it removed from the device.`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSoftwarePolicyExternals,
	RunE:              runSoftwareRemoveExternal,
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
	softwareCmd.AddCommand(softwareAddRepoCmd)
	softwareCmd.AddCommand(softwareRemoveRepoCmd)
	softwareCmd.AddCommand(softwareAddExternalCmd)
	softwareCmd.AddCommand(softwareRemoveExternalCmd)

	softwareListCmd.Flags().String("name", "", "Filter by name (regex pattern)")
	softwareListCmd.Flags().String("sort-by", "name:asc", "Sort field and direction (name, created_at, updated_at)")
	softwareListCmd.Flags().Int("page", 1, "Page number")
	softwareListCmd.Flags().Int("per-page", 50, "Items per page (max 100)")

	softwareCreateCmd.Flags().String("content", "", `Optional inline JSON content for bulk seed. When omitted, the policy is created empty ({"present":[],"absent":[]}) and you fill it with require-package / block-package.`)
	softwareCreateCmd.Flags().String("file", "", "Read content from a file instead of --content (bulk-seed alternative)")

	softwareAddRepoCmd.Flags().String("name", "", "Repository name (required) — also the identity used to update an existing entry")
	softwareAddRepoCmd.Flags().String("url", "", "Repository URL (required). ${ABI} is substituted on the device, so quote it to keep the shell off it")
	softwareAddRepoCmd.Flags().Int("priority", 0, "Repository priority, 0-10. Must be unique within the policy")
	softwareAddRepoCmd.Flags().Bool("enabled", true, "Whether the repository is enabled on the device")
	softwareAddRepoCmd.Flags().String("signature-type", "", "Signature type: fingerprints, pubkey, or none. Inferred from --fingerprint / --pubkey-file when omitted")
	softwareAddRepoCmd.Flags().StringArray("fingerprint", nil, "Trusted fingerprint as sha256:<64-hex> (repeatable). Implies --signature-type fingerprints")
	softwareAddRepoCmd.Flags().String("pubkey-file", "", "Path to a PEM public key. Implies --signature-type pubkey; the key is sent by value, not by path")
	_ = softwareAddRepoCmd.MarkFlagRequired("name")

	softwareAddExternalCmd.Flags().String("name", "", "Package name the URL is expected to deliver (required)")
	softwareAddExternalCmd.Flags().String("version", "", "Package version the URL is expected to deliver (required)")
	softwareAddExternalCmd.Flags().String("url", "", "Package URL to install from (required)")
	softwareAddExternalCmd.Flags().Bool("force", false, "Pass -f to pkg add — reinstall even when pkg considers it installed")
	_ = softwareAddExternalCmd.MarkFlagRequired("name")
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
	warnings, err := svc.SoftwarePolicyUpdateContent(ctx, org, name, newContent)
	if err != nil {
		return err
	}
	color.Green("✓ Software policy updated: %s", name)
	renderPolicyWarnings(warnings)
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
	warnings, err := svc.SoftwarePolicyUpdateContent(context.Background(), org, name, string(data))
	if err != nil {
		return err
	}
	color.Green("✓ Software policy content updated: %s", name)
	renderPolicyWarnings(warnings)
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
		warnings []string
		err      error
	)
	switch op {
	case "require":
		outcomes, warnings, err = svc.SoftwarePolicyRequirePackages(ctx, org, policy, packages)
	case "block":
		outcomes, warnings, err = svc.SoftwarePolicyBlockPackages(ctx, org, policy, packages)
	case "waive":
		outcomes, warnings, err = svc.SoftwarePolicyWaivePackages(ctx, org, policy, packages)
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
	renderPolicyWarnings(warnings)
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

func runSoftwareAddRepo(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	policy := args[0]
	name, _ := cmd.Flags().GetString("name")

	// Only the flags actually given are sent. On an existing repository
	// the rest keep their stored values, so changing a URL does not
	// reset the priority or re-enable something the operator turned off.
	patch := models.RepositoryPatch{Name: name}
	if cmd.Flags().Changed("url") {
		url, _ := cmd.Flags().GetString("url")
		patch.URL = &url
	}
	if cmd.Flags().Changed("priority") {
		priority, _ := cmd.Flags().GetInt("priority")
		patch.Priority = &priority
	}
	if cmd.Flags().Changed("enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		patch.Enabled = &enabled
	}

	signature, err := buildRepositorySignature(cmd)
	if err != nil {
		return err
	}
	patch.Signature = signature

	outcome, warnings, err := svc.SoftwarePolicySetRepository(context.Background(), org, policy, patch)
	if err != nil {
		return err
	}
	renderEntryOutcome(outcome, "Repository")
	renderPolicyWarnings(warnings)
	return nil
}

// buildRepositorySignature turns the signature flags into the wire
// shape, or returns nil when the operator passed none — meaning "leave
// the signature configuration alone", which is only valid for a
// repository that already exists.
//
// The type is inferred when it can be, but never inferred as "none":
// disabling signature verification is a decision the operator states
// out loud, not something that falls out of omitting a flag.
func buildRepositorySignature(cmd *cobra.Command) (*models.RepositorySignature, error) {
	sigType, _ := cmd.Flags().GetString("signature-type")
	fingerprints, _ := cmd.Flags().GetStringArray("fingerprint")
	pubkeyFile, _ := cmd.Flags().GetString("pubkey-file")

	if sigType == "" && len(fingerprints) == 0 && pubkeyFile == "" {
		return nil, nil
	}
	if len(fingerprints) > 0 && pubkeyFile != "" {
		return nil, fmt.Errorf("--fingerprint and --pubkey-file are mutually exclusive")
	}
	if sigType == "" {
		if len(fingerprints) > 0 {
			sigType = "fingerprints"
		} else {
			sigType = "pubkey"
		}
	}

	switch sigType {
	case "fingerprints":
		parsed := make([]models.RepositoryFingerprint, 0, len(fingerprints))
		for _, raw := range fingerprints {
			fp, err := models.ParseFingerprint(raw)
			if err != nil {
				return nil, err
			}
			parsed = append(parsed, fp)
		}
		return &models.RepositorySignature{Type: sigType, Fingerprints: parsed}, nil

	case "pubkey":
		if pubkeyFile == "" {
			return nil, fmt.Errorf("--signature-type pubkey requires --pubkey-file")
		}
		data, err := os.ReadFile(pubkeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read public key: %w", err)
		}
		return &models.RepositorySignature{Type: sigType, Pubkey: string(data)}, nil

	case "none":
		return &models.RepositorySignature{Type: sigType}, nil

	default:
		return nil, fmt.Errorf("unknown --signature-type %q: expected fingerprints, pubkey, or none", sigType)
	}
}

func runSoftwareRemoveRepo(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	outcome, warnings, err := svc.SoftwarePolicyRemoveRepository(context.Background(), org, args[0], args[1])
	if err != nil {
		return err
	}
	renderEntryOutcome(outcome, "Repository")
	renderPolicyWarnings(warnings)
	return nil
}

func runSoftwareAddExternal(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name, _ := cmd.Flags().GetString("name")
	patch := models.ExternalPatch{Name: name}
	if cmd.Flags().Changed("version") {
		version, _ := cmd.Flags().GetString("version")
		patch.Version = &version
	}
	if cmd.Flags().Changed("url") {
		url, _ := cmd.Flags().GetString("url")
		patch.URL = &url
	}
	if cmd.Flags().Changed("force") {
		force, _ := cmd.Flags().GetBool("force")
		patch.Force = &force
	}

	outcome, warnings, err := svc.SoftwarePolicySetExternal(context.Background(), org, args[0], patch)
	if err != nil {
		return err
	}
	renderEntryOutcome(outcome, "External package")
	renderPolicyWarnings(warnings)
	return nil
}

func runSoftwareRemoveExternal(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	outcome, warnings, err := svc.SoftwarePolicyRemoveExternal(context.Background(), org, args[0], args[1])
	if err != nil {
		return err
	}
	renderEntryOutcome(outcome, "External package")
	renderPolicyWarnings(warnings)
	return nil
}

// renderEntryOutcome prints one line for a repository / external-package
// mutation, in the same shape renderPackageOutcomes uses.
func renderEntryOutcome(o models.EntryActionOutcome, kind string) {
	switch o.Action {
	case "added":
		color.Green("✓ %s added: %s", kind, o.Name)
	case "updated":
		color.Green("✓ %s updated: %s", kind, o.Name)
	case "removed":
		color.Green("✓ %s removed: %s", kind, o.Name)
	default:
		color.Cyan("ℹ %s %s: no change", kind, o.Name)
	}
}

// renderPolicyWarnings surfaces the server's advisory conflict notices.
// The write already succeeded — these say that this policy and another
// one landing on the same device disagree, which will hard-fail at sync
// time. Yellow, not red, and never an error return: the operator asked
// for a write and got one.
func renderPolicyWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Println()
	color.Yellow("⚠ %d conflict warning(s) — the change was saved, but sync will fail until they are resolved:", len(warnings))
	for _, w := range warnings {
		color.Yellow("  • %s", w)
	}
}
