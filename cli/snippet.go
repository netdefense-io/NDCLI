package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	internalHelpers "github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

var snippetCmd = &cobra.Command{
	Use:   "snippet",
	Short: "Snippet management commands",
}

var snippetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List snippets",
	RunE:  runSnippetList,
}

var snippetCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new snippet",
	Long:  snippetCreateLong,
	Args:  cobra.ExactArgs(1),
	RunE:  runSnippetCreate,
}

// snippetCreateLong documents AUTH_SERVER, AUTH_ORDER, TRUST_CA and
// TRUST_CERT inline, since their content is free-form JSON with no
// dedicated flags of its own. models.SnippetUUIDGuidance,
// models.SnippetFieldContract, models.AuthOrderConsistencyRule,
// models.AuthPrerequisites and models.TrustPrerequisites are the sentences
// other surfaces (MCP tools, docs) must state identically, so they are
// shared constants rather than literals here.
const snippetCreateLong = `Create a new snippet.

` + models.SnippetUUIDGuidance + `

` + models.SnippetFieldContract + `

AUTH_SERVER and AUTH_ORDER content:

AUTH_SERVER defines one external authentication server (LDAP or Active
Directory) that NetDefense will create and manage on a device
(type: "ldap"). Content keys mirror OPNsense's own field names (host,
ldap_port, ldap_basedn, ...). ldap_bindpw must be exactly ${VAR_NAME},
naming an org-scope secret variable created with
'ndcli variable org create AD_BIND_PW --secret --value-stdin' (pipe or
redirect the password in; omit --value-stdin to be prompted for it
without echo instead) — a literal password is rejected. Single-quote the
${...} reference so your own shell does not try to expand it before
ndcli ever sees it, e.g.:

  ndcli snippet create my-ad --type AUTH_SERVER --content '{"ldap_bindpw":"${AD_BIND_PW}", ...}'

Cleartext LDAP ("TCP - Standard") requires an explicit human
acknowledgement in the content (nd_allow_cleartext_ldap: true) — it sends
every user's password unencrypted. Prefer StartTLS or SSL.

AUTH_ORDER defines a login-order policy for one facility. v1 defines only
"webadmin", which controls the web GUI and password login over SSH, the
console, su and sudo — one OPNsense setting. Key-based SSH, the Local API
and a device connect session are not affected.

  {"facilities": {"webadmin": {"order": ["Local Database", "Corp-AD"]}}}

- "Local Database" must always be first in a webadmin order — enforced on
  write, so local-database accounts keep working even when every external
  server is unreachable.
- Consistency rule: ` + models.AuthOrderConsistencyRule + `. A mistyped
  name is caught when the device syncs, not when this snippet is saved.
- A server configured by hand on the device (one NetDefense did not
  create) can be named in an order policy, but it gets none of
  NetDefense's guardrails — no reserved-name exclusion, no Limit-groups
  requirement, no protected-group check, no cleartext acknowledgement.
  Check it yourself before naming it.
- Recovery if password login breaks: push a policy with
  {"facilities": {"webadmin": {"order": ["Local Database"]}}} (needs
  reject_dangerous_snippets off on the device), open a device connect
  session while the device allows full remote access (remote_access_policy
  = full) with an org:rw or org:su account, or use key-based SSH or a
  local console password.

Attaching or detaching an AUTH_SERVER/AUTH_ORDER snippet, its template, or
moving a device into an OU that carries one, only needs org:rw — even
though authoring the content itself needs org:su. That lets an org:rw
user place an org:su-authored policy, a webadmin reset included, onto
more devices.

Prerequisites: ` + models.AuthPrerequisites + `.

TRUST_CA and TRUST_CERT content:

TRUST_CA syncs a CA certificate, and TRUST_CERT a certificate with its
private key, into each device's trust store (System > Trust). Binding one
to a service is still done on the device; after a renewal, the device
reloads the services that use it, or warns when it can't. Leave uuid out:
NetDefense adds one on create and refuses one supplied there. An edit may
keep it or leave it out, but never change it.

  TRUST_CA:   {"name": "Corp Root CA", "crt": "<PEM>"}
  TRUST_CERT: {"name": "fw-wildcard", "crt": "<PEM>", "key": "${FW_KEY}"}

- name is the object's description on the device: 1 to 255 bytes (UTF-8),
  with no ${ and no control characters. A CA or certificate created by hand
  under the same name is never adopted or touched; the sync reports
  NAME_COLLISION_UNMANAGED instead.
- crt is exactly one PEM CERTIFICATE block, or exactly ${VAR_NAME}.
  Overriding the crt and key variables per device gives each device its
  own certificate. The certificate's key must be RSA of 2048 to 4096 bits
  for a TRUST_CERT or 2048 to 8192 for a TRUST_CA, or EC on P-256, P-384
  or P-521. A shorter RSA key is refused with TRUST_KEY_WEAK. Anything
  else, including a longer RSA key, Ed25519, Ed448 and DSA, is refused
  with TRUST_KEY_UNSUPPORTED.
- key must be exactly ${VAR_NAME}, naming an org-scope secret variable that
  holds the certificate's unencrypted PEM private key. A key is never
  accepted in the content itself. That variable may appear only in
  TRUST_CERT key fields.
  Redirect the key file into it; the no-echo prompt reads only one line, so
  it can't take a PEM key:

    ndcli variable org create FW_KEY --secret --value-stdin < fw-wildcard.key

- Every variable a TRUST snippet uses must be written by a Superuser,
  including a crt variable and every override.

Wrap a PEM file into JSON content with jq (-R reads it raw, -s keeps the
whole file as one string), then create the snippet from that file.
Single-quote the jq program so your shell leaves ${FW_KEY} alone:

  jq -Rs '{name: "Corp Root CA", crt: .}' corp-root-ca.pem > corp-root-ca.json
  ndcli snippet create corp-root-ca --type TRUST_CA --file corp-root-ca.json

  jq -Rs '{name: "fw-wildcard", crt: ., key: "${FW_KEY}"}' fw-wildcard.crt > fw-wildcard.json
  ndcli snippet create fw-wildcard --type TRUST_CERT --file fw-wildcard.json

To rotate a certificate, update its key variable and the certificate back
to back. A sync that runs in between fails with TRUST_KEY_MISMATCH, and the
device keeps its current pair. A CA renews in place only with the same
subject and key (otherwise TRUST_CA_REKEYED). A CA rollover is a new
TRUST_CA snippet whose CA has a new subject DN (for example "... G2"): two
CAs with one subject and different keys are refused with
TRUST_CA_DN_CONFLICT.

Superuser only: creating, updating, renaming, re-prioritizing or deleting a
TRUST_CA or TRUST_CERT snippet needs org:su from an interactive login
(ndcli auth login). A token is not enough: personal access tokens are
capped at org:rw.

Prerequisites: ` + models.TrustPrerequisites + `.`

var snippetDescribeCmd = &cobra.Command{
	Use:               "describe [name]",
	Short:             "Show snippet details",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetDescribe,
}

var snippetEditCmd = &cobra.Command{
	Use:               "edit [name]",
	Short:             "Edit snippet content in external editor",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetEdit,
}

var snippetUpdateContentCmd = &cobra.Command{
	Use:               "update-content [name] [file]",
	Short:             "Update snippet content from file",
	Long:              "Replace a snippet's content with the contents of a file.\n\n" + models.SnippetUUIDGuidance + "\n\n" + models.SnippetFieldContract,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetUpdateContent,
}

var snippetRenameCmd = &cobra.Command{
	Use:               "rename [name] [new-name]",
	Short:             "Rename a snippet",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetRename,
}

var snippetSetPriorityCmd = &cobra.Command{
	Use:               "set-priority [name] [priority]",
	Short:             "Set snippet priority",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetSetPriority,
}

var snippetDeleteCmd = &cobra.Command{
	Use:               "delete [name]",
	Short:             "Delete a snippet",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSnippets,
	RunE:              runSnippetDelete,
}

var snippetPullCmd = &cobra.Command{
	Use:   "pull [device] [match-key]",
	Short: "Pull snippet content from a device",
	Long: `Pull snippet content from a device's configuration.

This creates an asynchronous task that retrieves the specified configuration
from the device and optionally stores it as a snippet in the database.

The positional argument is the MATCH KEY — what identifies the object on the
device — and may contain any characters, spaces included. Rule descriptions
almost always do.

The created snippet's name is derived from the match key unless --name is
given: lowercased, with runs of anything outside [a-z0-9._-] collapsed to a
single hyphen. Pass --name when the derived name is not what you want, or when
the match key has nothing usable to derive from.

Matching behavior:
- For USER: Exact name match on the username
- For GROUP: Exact name match on the group name
- For ALIAS: Exact name match on the alias name field
- For RULE: Partial description match (case-insensitive)
  - If multiple rules match, the task will fail with an error
  - Use a more specific description to identify a unique rule
- For UNBOUND_HOST_OVERRIDE: Match by hostname.domain (e.g., server1.local)
- For UNBOUND_DOMAIN_FORWARD: Match by domain name (e.g., internal.corp)
- For UNBOUND_HOST_ALIAS: Match by hostname.domain (e.g., www.local)
- For UNBOUND_ACL: Match by ACL name (e.g., lan-clients)
- For ZABBIX_SETTINGS: Singleton — there is nothing to match on the device, so
  the match key only feeds the derived snippet name (--name sets it exactly);
  the agent returns the full Zabbix Agent settings tree
- For ZABBIX_USERPARAMETER: Exact UserParameter key (e.g., nd-cpu-temp)
- For ZABBIX_ALIAS: Exact item-alias key (e.g., nd-uname)

By default, the command returns immediately with the task ID.
Use --wait (-w) to wait for the task to complete and display the result.`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeDevices,
	RunE:              runSnippetPull,
}

func init() {
	snippetCmd.AddCommand(snippetListCmd)
	snippetCmd.AddCommand(snippetCreateCmd)
	snippetCmd.AddCommand(snippetDescribeCmd)
	snippetCmd.AddCommand(snippetEditCmd)
	snippetCmd.AddCommand(snippetUpdateContentCmd)
	snippetCmd.AddCommand(snippetRenameCmd)
	snippetCmd.AddCommand(snippetSetPriorityCmd)
	snippetCmd.AddCommand(snippetDeleteCmd)
	snippetCmd.AddCommand(snippetPullCmd)

	// List flags
	snippetListCmd.Flags().String("type", "", "Filter by type: "+models.SnippetTypesHelp(models.SnippetCreatableTypes))
	snippetListCmd.Flags().String("name", "", "Filter by name (regex pattern)")
	snippetListCmd.Flags().String("sort-by", "priority:asc", "Sort field and direction (priority, name, created_at, updated_at)")
	snippetListCmd.Flags().Int("page", 1, "Page number")
	snippetListCmd.Flags().Int("per-page", 50, "Items per page (max 100)")
	snippetListCmd.Flags().String("created-after", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("created-before", "", "Filter by created date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("updated-after", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")
	snippetListCmd.Flags().String("updated-before", "", "Filter by updated date (e.g., 30m, 2h, 7d, 2w or ISO 8601)")

	// Create flags
	snippetCreateCmd.Flags().String("type", "", "Snippet type (required): "+models.SnippetTypesHelp(models.SnippetCreatableTypes))
	snippetCreateCmd.Flags().String("content", "", "Snippet content (required)")
	snippetCreateCmd.Flags().String("file", "", "Read content from file instead of --content")
	snippetCreateCmd.Flags().Int("priority", 1000, "Snippet priority 1-60000 (default 1000)")

	// Pull flags
	snippetPullCmd.Flags().String("name", "", "Name for the created snippet (letters, digits, dot, underscore, hyphen; derived from the match key when omitted)")
	snippetPullCmd.Flags().String("type", "ALIAS", "Config type to pull: "+models.SnippetTypesHelp(models.SnippetPullableTypes))
	snippetPullCmd.Flags().Bool("auto-create", false, "Create snippet in DB if it doesn't exist")
	snippetPullCmd.Flags().Bool("overwrite", false, "Update snippet in DB if it already exists")
	snippetPullCmd.Flags().BoolP("wait", "w", false, "Wait for task to complete")
}

// namedSnippetNameFlag fills in the CLI's own way of setting the snippet name.
// The service leaves a marker because an MCP caller has no flags to pass; here,
// naming the flag is the useful advice.
func namedSnippetNameFlag(err error) error {
	var svcErr *service.Error
	if !errors.As(err, &svcErr) || !strings.Contains(svcErr.Message, service.SnippetNameHint) {
		return err
	}
	return &service.Error{
		Code:    svcErr.Code,
		Message: strings.Replace(svcErr.Message, service.SnippetNameHint, "--name", 1),
		Err:     err,
	}
}

// requireSnippetType is the create command's own --type validation, pulled
// out as a pure function so the error text — built from
// models.SnippetCreatableTypes — is testable without cobra/auth/org
// plumbing.
func requireSnippetType(snippetType string) error {
	if snippetType == "" {
		return fmt.Errorf("--type is required (%s)", models.SnippetTypesHelp(models.SnippetCreatableTypes))
	}
	return nil
}

func runSnippetList(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.SnippetListOpts{}
	opts.Type, _ = cmd.Flags().GetString("type")
	opts.Name, _ = cmd.Flags().GetString("name")
	opts.SortBy, _ = cmd.Flags().GetString("sort-by")
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	opts.CreatedAfter, _ = cmd.Flags().GetString("created-after")
	opts.CreatedBefore, _ = cmd.Flags().GetString("created-before")
	opts.UpdatedAfter, _ = cmd.Flags().GetString("updated-after")
	opts.UpdatedBefore, _ = cmd.Flags().GetString("updated-before")

	result, err := svc.SnippetList(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatSnippets(result.Snippets, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}

func runSnippetCreate(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	snippetType, _ := cmd.Flags().GetString("type")
	content, _ := cmd.Flags().GetString("content")
	file, _ := cmd.Flags().GetString("file")
	priority, _ := cmd.Flags().GetInt("priority")

	if err := requireSnippetType(snippetType); err != nil {
		return err
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	}
	if content == "" {
		return fmt.Errorf("--content or --file is required")
	}

	if _, err := svc.SnippetCreate(context.Background(), org, service.SnippetCreateOpts{
		Name:     name,
		Type:     snippetType,
		Content:  content,
		Priority: priority,
	}); err != nil {
		return err
	}
	color.Green("✓ Snippet created: %s", name)
	return nil
}

func runSnippetDescribe(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	snippet, err := svc.SnippetGet(context.Background(), org, name)
	if err != nil {
		return err
	}
	return formatter.FormatSnippet(snippet)
}

func runSnippetEdit(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	ctx := context.Background()
	snippet, err := svc.SnippetGet(ctx, org, name)
	if err != nil {
		return err
	}

	pretty := internalHelpers.PrettyJSON(snippet.Content)
	edited, err := internalHelpers.EditContent(pretty, ".json")
	if err != nil {
		return fmt.Errorf("failed to edit content: %w", err)
	}
	// Minify before sending so the server stores compact JSON (matching the
	// historical wire format). Compare minified-new against the original
	// (also minified server-side) to detect no-op edits — re-indenting alone
	// must not trigger an update.
	newContent := internalHelpers.MinifyJSON(edited)
	if newContent == internalHelpers.MinifyJSON(snippet.Content) {
		fmt.Println("No changes made")
		return nil
	}

	if err := svc.SnippetUpdateContent(ctx, org, name, newContent); err != nil {
		return err
	}
	color.Green("✓ Snippet updated: %s", name)
	return nil
}

func runSnippetUpdateContent(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	file := args[1]

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	if err := svc.SnippetUpdateContent(context.Background(), org, name, string(data)); err != nil {
		return err
	}
	color.Green("✓ Snippet content updated: %s", name)
	return nil
}

func runSnippetRename(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	newName := args[1]
	if err := svc.SnippetRename(context.Background(), org, name, newName); err != nil {
		return err
	}
	color.Green("✓ Snippet renamed: %s -> %s", name, newName)
	return nil
}

func runSnippetSetPriority(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	priorityStr := args[1]
	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		return fmt.Errorf("invalid priority: %s", priorityStr)
	}
	if err := svc.SnippetSetPriority(context.Background(), org, name, priority); err != nil {
		return err
	}
	color.Green("✓ Priority set to %d for: %s", priority, name)
	return nil
}

func runSnippetDelete(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	name := args[0]
	if !internalHelpers.Confirm(fmt.Sprintf("Delete snippet '%s'?", name)) {
		fmt.Fprintln(os.Stderr, "Cancelled")
		return nil
	}
	if err := svc.SnippetDelete(context.Background(), org, name); err != nil {
		return err
	}
	color.Green("✓ Snippet deleted: %s", name)
	return nil
}

func runSnippetPull(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	deviceName := args[0]
	matchKey := args[1]

	snippetName, _ := cmd.Flags().GetString("name")
	configType, _ := cmd.Flags().GetString("type")
	autoCreate, _ := cmd.Flags().GetBool("auto-create")
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	wait, _ := cmd.Flags().GetBool("wait")

	// Fail before the round trip rather than as a server rejection after one.
	if snippetName != "" {
		if err := service.ValidateSnippetName(snippetName); err != nil {
			return err
		}
	}

	ctx := context.Background()
	pull, err := svc.SnippetPull(ctx, org, deviceName, service.SnippetPullOpts{
		Name:        matchKey,
		SnippetName: snippetName,
		ConfigType:  configType,
		AutoCreate:  autoCreate,
		Overwrite:   overwrite,
	})
	if err != nil {
		return namedSnippetNameFlag(err)
	}
	result := &models.SnippetPull{Task: pull.Task, Status: pull.Status, ConfigType: pull.ConfigType, Snippet: pull.Name}

	if !wait {
		return formatter.FormatSnippetPull(result)
	}

	// Progress, not the result: it goes to stderr, where it does not land in
	// a -f json document.
	output.ColorSuccess.Fprintf(os.Stderr, "✓ Pull task created: %s\n", pull.Task)
	fmt.Fprintln(os.Stderr, "Waiting for task to complete...")
	for attempt := 0; attempt < 60; attempt++ {
		time.Sleep(1 * time.Second)
		task, err := svc.TaskGet(ctx, pull.Task)
		if err != nil {
			return fmt.Errorf("failed to check task status: %w", err)
		}
		switch task.Status {
		case models.TaskStatusCompleted:
			// Report the snippet the server actually created, not the match
			// key: with a derived name the two now differ, and naming the
			// match key here would tell the user a snippet exists under a
			// name that does not.
			created := pull.Name
			if created == "" {
				created = matchKey
			}
			result.Status = task.Status
			result.Snippet = created
			result.Content = task.Message
			return formatter.FormatSnippetPull(result)
		case models.TaskStatusFailed:
			errMsg := task.Message
			if errMsg == "" {
				errMsg = task.ErrorMessage
			}
			return fmt.Errorf("pull task failed: %s", errMsg)
		case models.TaskStatusCanceled:
			return fmt.Errorf("pull task was cancelled")
		}
	}
	return fmt.Errorf("timeout waiting for pull task to complete. Check task status with: ndcli task describe %s", pull.Task)
}
