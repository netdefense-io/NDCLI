package resources

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
	"github.com/netdefense-io/NDCLI/internal/tui/registry"
	"github.com/netdefense-io/NDCLI/internal/tui/uihelp"
)

// softwareResource surfaces software policies — reusable package inventory
// documents attached to templates: required/blocked package names, plus the
// custom pkg repositories and external (URL-installed) packages the policy
// configures on the device. Describe powers the generic detail fallback via
// the single-policy GET.
type softwareResource struct{}

func (softwareResource) Kind() string  { return "software" }
func (softwareResource) Title() string { return "Software Policies" }

func (softwareResource) Columns() []registry.Column {
	return []registry.Column{
		{Title: "NAME", Width: 28},
		{Title: "PRESENT", Width: 10},
		{Title: "ABSENT", Width: 10},
		{Title: "REPOS", Width: 7},
		{Title: "EXT", Width: 6},
		{Title: "UPDATED", Width: 0},
	}
}

func (softwareResource) Fetch(ctx context.Context, svc *service.Service, org string, page, perPage int) ([]registry.Row, int, error) {
	res, err := svc.SoftwarePolicyList(ctx, org, service.SoftwarePolicyListOpts{Page: page, PerPage: perPage})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]registry.Row, 0, len(res.Policies))
	for _, p := range res.Policies {
		counts := policyCounts(p.Content)
		rows = append(rows, registry.Row{
			ID: p.Name,
			Cells: []string{
				p.Name,
				fmt.Sprintf("%d", counts.present),
				fmt.Sprintf("%d", counts.absent),
				fmt.Sprintf("%d", counts.repositories),
				fmt.Sprintf("%d", counts.external),
				ago(p.UpdatedAt),
			},
		})
	}
	return rows, res.Total, nil
}

func (softwareResource) Actions() []registry.Action {
	return []registry.Action{
		{Key: "n", Label: "create", TargetsAll: true, Form: []registry.FormField{
			{Key: "name", Label: "Name", Required: true},
			{Key: "content", Label: "Content", Placeholder: `{"present":[],"absent":[]}`},
		}},
		{Key: "e", Label: "edit", Shell: []string{"software", "edit", "{id}", "-o", "{org}"}},
		{Key: "m", Label: "rename", Form: []registry.FormField{
			{Key: "new_name", Label: "New name", Required: true},
		}},
		{Key: "R", Label: "require-pkg", Form: []registry.FormField{
			{Key: "packages", Label: "Packages", Required: true, Placeholder: "pkg1 pkg2"},
		}},
		{Key: "B", Label: "block-pkg", Form: []registry.FormField{
			{Key: "packages", Label: "Packages", Required: true, Placeholder: "pkg1 pkg2"},
		}},
		{Key: "W", Label: "waive-pkg", Form: []registry.FormField{
			{Key: "packages", Label: "Packages", Required: true, Placeholder: "pkg1 pkg2"},
		}},
		{Key: "y", Label: "add-repo", Form: []registry.FormField{
			{Key: "name", Label: "Repository name", Required: true},
			{Key: "url", Label: "URL", Placeholder: "https://host/repo/${ABI} (blank = unchanged)"},
			{Key: "priority", Label: "Priority (0-10)", Placeholder: "blank = unchanged"},
			// (unchanged) is the repo-wide sentinel for tri-state form
			// fields. Without it these default to their first option and
			// an edit that only meant to change the URL would silently
			// re-enable a repository the operator had disabled.
			{Key: "enabled", Label: "Enabled", Options: []string{"(unchanged)", "yes", "no"}, Default: "(unchanged)"},
			{Key: "signature_type", Label: "Signature", Options: []string{"(unchanged)", "fingerprints", "pubkey", "none"}, Default: "(unchanged)"},
			{Key: "fingerprint", Label: "Fingerprint", Placeholder: "sha256:<64-hex>"},
			{Key: "pubkey", Label: "PEM public key", Placeholder: "-----BEGIN PUBLIC KEY-----…"},
		}},
		{Key: "Y", Label: "remove-repo", Form: []registry.FormField{
			{Key: "name", Label: "Repository name", Required: true},
		}},
		{Key: "u", Label: "add-external", Form: []registry.FormField{
			{Key: "name", Label: "Package name", Required: true},
			{Key: "version", Label: "Version", Placeholder: "blank = unchanged"},
			{Key: "url", Label: "URL", Placeholder: "blank = unchanged"},
			{Key: "force", Label: "Force reinstall", Options: []string{"(unchanged)", "no", "yes"}, Default: "(unchanged)"},
		}},
		{Key: "U", Label: "remove-external", Form: []registry.FormField{
			{Key: "name", Label: "Package name", Required: true},
		}},
		{Key: "x", Label: "delete", Destructive: true,
			Prompt: "Delete software policy {id}?"},
	}
}

func (softwareResource) Execute(ctx context.Context, svc *service.Service, org, id, actionKey string, args map[string]string) (string, error) {
	switch actionKey {
	case "n":
		content := args["content"]
		if content == "" {
			content = `{"present":[],"absent":[]}`
		}
		if _, err := svc.SoftwarePolicyCreate(ctx, org, service.SoftwarePolicyCreateOpts{
			Name:    args["name"],
			Content: content,
		}); err != nil {
			return "", err
		}
		return "created policy " + args["name"], nil
	case "m":
		newName := args["new_name"]
		if err := svc.SoftwarePolicyRename(ctx, org, id, newName); err != nil {
			return "", err
		}
		return "renamed " + id + " to " + newName, nil
	case "R":
		pkgs := strings.Fields(strings.ReplaceAll(args["packages"], ",", " "))
		if len(pkgs) == 0 {
			return "", fmt.Errorf("at least one package is required")
		}
		if _, _, err := svc.SoftwarePolicyRequirePackages(ctx, org, id, pkgs); err != nil {
			return "", err
		}
		return fmt.Sprintf("required %d package(s) on %s", len(pkgs), id), nil
	case "B":
		pkgs := strings.Fields(strings.ReplaceAll(args["packages"], ",", " "))
		if len(pkgs) == 0 {
			return "", fmt.Errorf("at least one package is required")
		}
		if _, _, err := svc.SoftwarePolicyBlockPackages(ctx, org, id, pkgs); err != nil {
			return "", err
		}
		return fmt.Sprintf("blocked %d package(s) on %s", len(pkgs), id), nil
	case "W":
		pkgs := strings.Fields(strings.ReplaceAll(args["packages"], ",", " "))
		if len(pkgs) == 0 {
			return "", fmt.Errorf("at least one package is required")
		}
		if _, _, err := svc.SoftwarePolicyWaivePackages(ctx, org, id, pkgs); err != nil {
			return "", err
		}
		return fmt.Sprintf("waived %d package(s) on %s", len(pkgs), id), nil
	case "y":
		patch, err := repositoryPatchFromForm(args)
		if err != nil {
			return "", err
		}
		outcome, _, err := svc.SoftwarePolicySetRepository(ctx, org, id, patch)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("repository %s: %s", outcome.Name, outcome.Action), nil
	case "Y":
		outcome, _, err := svc.SoftwarePolicyRemoveRepository(ctx, org, id, args["name"])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("repository %s: %s", outcome.Name, outcome.Action), nil
	case "u":
		outcome, _, err := svc.SoftwarePolicySetExternal(ctx, org, id, externalPatchFromForm(args))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("external %s: %s", outcome.Name, outcome.Action), nil
	case "U":
		outcome, _, err := svc.SoftwarePolicyRemoveExternal(ctx, org, id, args["name"])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("external %s: %s", outcome.Name, outcome.Action), nil
	case "x":
		if err := svc.SoftwarePolicyDelete(ctx, org, id); err != nil {
			return "", err
		}
		return "deleted " + id, nil
	}
	return "", fmt.Errorf("unknown action %q", actionKey)
}

// Describe implements registry.Describer.
func (softwareResource) Describe(ctx context.Context, svc *service.Service, org, id string) ([]registry.Section, error) {
	p, err := svc.SoftwarePolicyGet(ctx, org, id)
	if err != nil {
		return nil, err
	}
	content, _ := models.ParseSoftwarePolicyContent(p.Content)
	if content == nil {
		content = &models.SoftwarePolicyContent{}
	}
	fields := []registry.Field{
		{Label: "Name", Value: p.Name},
		{Label: "Organization", Value: uihelp.Default(p.Organization, "—")},
		{Label: "Present", Value: fmt.Sprintf("%d", len(content.Present))},
		{Label: "Absent", Value: fmt.Sprintf("%d", len(content.Absent))},
		{Label: "Repositories", Value: fmt.Sprintf("%d", len(content.Repositories))},
		{Label: "External", Value: fmt.Sprintf("%d", len(content.External))},
		{Label: "Templates", Value: uihelp.Default(strings.Join(p.TemplateNames, ", "), "—")},
		{Label: "Created", Value: fullTime(p.CreatedAt)},
		{Label: "Updated", Value: fullTime(p.UpdatedAt)},
	}
	sections := []registry.Section{{Title: "Software Policy", Fields: fields}}
	if len(content.Present) > 0 {
		sections = append(sections, registry.Section{
			Title: "Required packages",
			Text:  strings.Join(content.Present, "\n"),
		})
	}
	if len(content.Absent) > 0 {
		sections = append(sections, registry.Section{
			Title: "Blocked packages",
			Text:  strings.Join(content.Absent, "\n"),
		})
	}
	if len(content.Repositories) > 0 {
		lines := make([]string, 0, len(content.Repositories))
		for _, r := range content.Repositories {
			state := "enabled"
			if !r.Enabled {
				state = "disabled"
			}
			lines = append(lines, fmt.Sprintf("%s  priority %d  %s  %s  [%s]",
				r.Name, r.Priority, state, r.URL, uihelp.Default(r.Signature.Type, "—")))
		}
		sections = append(sections, registry.Section{
			Title: "Custom repositories",
			Text:  strings.Join(lines, "\n"),
		})
	}
	if len(content.External) > 0 {
		lines := make([]string, 0, len(content.External))
		for _, e := range content.External {
			line := fmt.Sprintf("%s %s  %s", e.Name, e.Version, e.URL)
			if e.Force {
				line += "  (force)"
			}
			lines = append(lines, line)
		}
		sections = append(sections, registry.Section{
			Title: "External packages",
			Text:  strings.Join(lines, "\n"),
		})
	}
	return sections, nil
}

// policyCountSet is what the list row shows. List responses omit
// Content, so a missing or unparseable document yields zero counts
// rather than an error.
type policyCountSet struct {
	present      int
	absent       int
	repositories int
	external     int
}

func policyCounts(raw string) policyCountSet {
	c, err := models.ParseSoftwarePolicyContent(raw)
	if err != nil || c == nil {
		return policyCountSet{}
	}
	return policyCountSet{
		present:      len(c.Present),
		absent:       len(c.Absent),
		repositories: len(c.Repositories),
		external:     len(c.External),
	}
}

// repositoryPatchFromForm turns the add-repo form into a patch. Fields
// left blank, or left on the "(unchanged)" sentinel, are omitted — so
// editing one field of an existing repository does not reset the rest.
// On a repository that does not exist yet the service refuses a patch
// that is missing a URL or a signature, rather than inventing empties.
func repositoryPatchFromForm(args map[string]string) (models.RepositoryPatch, error) {
	patch := models.RepositoryPatch{Name: args["name"]}

	if url := strings.TrimSpace(args["url"]); url != "" {
		patch.URL = &url
	}
	if raw := strings.TrimSpace(args["priority"]); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return models.RepositoryPatch{}, fmt.Errorf("priority must be a number: %q", raw)
		}
		patch.Priority = &n
	}
	switch args["enabled"] {
	case "yes":
		enabled := true
		patch.Enabled = &enabled
	case "no":
		enabled := false
		patch.Enabled = &enabled
	}

	sigType := args["signature_type"]
	if sigType == "" || sigType == unchangedSentinel {
		return patch, nil
	}
	sig := models.RepositorySignature{Type: sigType}
	switch sigType {
	case "fingerprints":
		fp, err := models.ParseFingerprint(args["fingerprint"])
		if err != nil {
			return models.RepositoryPatch{}, err
		}
		sig.Fingerprints = []models.RepositoryFingerprint{fp}
	case "pubkey":
		sig.Pubkey = args["pubkey"]
		if strings.TrimSpace(sig.Pubkey) == "" {
			return models.RepositoryPatch{}, fmt.Errorf("a PEM public key is required for signature type pubkey")
		}
	case "none":
		// Accepted only for organizations with unverified repositories
		// enabled; the server is the one that decides.
	}
	patch.Signature = &sig
	return patch, nil
}

func externalPatchFromForm(args map[string]string) models.ExternalPatch {
	patch := models.ExternalPatch{Name: args["name"]}
	if version := strings.TrimSpace(args["version"]); version != "" {
		patch.Version = &version
	}
	if url := strings.TrimSpace(args["url"]); url != "" {
		patch.URL = &url
	}
	switch args["force"] {
	case "yes":
		force := true
		patch.Force = &force
	case "no":
		force := false
		patch.Force = &force
	}
	return patch
}

// unchangedSentinel is the repo-wide marker for "operator did not touch
// this field" in a tri-state select.
const unchangedSentinel = "(unchanged)"
