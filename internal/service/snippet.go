package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
)

// SnippetListOpts mirrors the filters accepted by the snippets list endpoint.
// Empty fields are omitted.
type SnippetListOpts struct {
	Type          string
	Name          string
	SortBy        string
	Page          int
	PerPage       int
	CreatedAfter  string
	CreatedBefore string
	UpdatedAfter  string
	UpdatedBefore string
}

// SnippetListResult mirrors the paginated snippet list response with
// pagination defaults applied.
type SnippetListResult struct {
	Snippets []models.Snippet
	Total    int
	Page     int
	PerPage  int
}

// SnippetList returns a paginated list of snippets in an organization.
func (s *Service) SnippetList(ctx context.Context, org string, opts SnippetListOpts) (*SnippetListResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := opts.PerPage
	if perPage < 1 {
		perPage = 50
	}

	params := map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if opts.Type != "" {
		params["type"] = opts.Type
	}
	if opts.Name != "" {
		params["name"] = opts.Name
	}
	if opts.SortBy != "" {
		params["sort_by"] = opts.SortBy
	}
	for _, f := range [][2]string{
		{opts.CreatedAfter, "created_after"},
		{opts.CreatedBefore, "created_before"},
		{opts.UpdatedAfter, "updated_after"},
		{opts.UpdatedBefore, "updated_before"},
	} {
		if f[0] == "" {
			continue
		}
		parsed, err := helpers.ParseTimeFilter(f[0])
		if err != nil {
			return nil, &Error{
				Code:    CodeInvalidInput,
				Message: fmt.Sprintf("invalid %s value: %v", f[1], err),
				Err:     err,
			}
		}
		params[f[1]] = parsed
	}

	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets", org), params)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result models.SnippetListResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &SnippetListResult{
		Snippets: result.Items,
		Total:    result.Total,
		Page:     page,
		PerPage:  perPage,
	}, nil
}

// SnippetGet returns a single snippet (with content) by name.
func (s *Service) SnippetGet(ctx context.Context, org, name string) (*models.Snippet, error) {
	if name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "snippet name is required"}
	}
	resp, err := s.api.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, name), nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &snippet, nil
}

// SnippetCreateOpts holds the fields needed to create a snippet.
type SnippetCreateOpts struct {
	Name     string
	Type     string
	Content  string
	Priority int
}

// SnippetCreate creates a new snippet.
func (s *Service) SnippetCreate(ctx context.Context, org string, opts SnippetCreateOpts) (*models.Snippet, error) {
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "snippet name is required"}
	}
	if opts.Type == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "snippet type is required"}
	}
	if opts.Content == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "snippet content is required"}
	}
	priority := opts.Priority
	if priority == 0 {
		priority = 1000
	}
	payload := map[string]interface{}{
		"name":     opts.Name,
		"type":     opts.Type,
		"content":  opts.Content,
		"priority": priority,
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets", org), payload)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var snippet models.Snippet
	if err := api.ParseResponse(resp, &snippet); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &snippet, nil
}

// SnippetUpdateContent replaces a snippet's content.
func (s *Service) SnippetUpdateContent(ctx context.Context, org, name, content string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "snippet name is required"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/content", org, name), map[string]string{"content": content})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SnippetRename renames a snippet.
func (s *Service) SnippetRename(ctx context.Context, org, name, newName string) error {
	if name == "" || newName == "" {
		return &Error{Code: CodeInvalidInput, Message: "snippet name and new name are required"}
	}
	resp, err := s.api.Post(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/rename", org, name), map[string]string{"new_name": newName})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SnippetSetPriority validates 1<=priority<=60000 and updates it.
func (s *Service) SnippetSetPriority(ctx context.Context, org, name string, priority int) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "snippet name is required"}
	}
	if priority < 1 || priority > 60000 {
		return &Error{Code: CodeInvalidInput, Message: "priority must be between 1 and 60000"}
	}
	resp, err := s.api.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s/priority", org, name), map[string]int{"priority": priority})
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SnippetDelete removes a snippet.
func (s *Service) SnippetDelete(ctx context.Context, org, name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "snippet name is required"}
	}
	resp, err := s.api.Delete(ctx, fmt.Sprintf("/api/v1/organizations/%s/snippets/%s", org, name))
	if err != nil {
		return wrapAPI("%v", err)
	}
	if err := api.ParseResponse(resp, nil); err != nil {
		return wrapAPI("%v", err)
	}
	return nil
}

// SnippetPullOpts collects the optional flags accepted by /devices/{d}/pull.
type SnippetPullOpts struct {
	// Name is the device-side match key, not the snippet's name. It is
	// matched against whatever identifies the object on the device — a
	// username, an alias name, a rule *description* — so it may contain any
	// characters, spaces included. Rule descriptions almost always do
	// (Community #10).
	Name string
	// SnippetName names the snippet that gets created, and is constrained to
	// the platform's identifier charset. Empty means the server derives one
	// from Name.
	SnippetName string
	ConfigType  string
	AutoCreate  bool
	Overwrite   bool
}

// snippetNamePattern is the charset NDManager accepts for a snippet name. The
// client checks it so a bad --name fails before a round trip rather than as a
// server rejection after one.
var snippetNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// snippetNameMaxLen matches NDManager's own bound.
const snippetNameMaxLen = 255

// snippetNameDerivePattern matches every run the broker collapses to a single
// hyphen when deriving a snippet name: anything outside its charset.
var snippetNameDerivePattern = regexp.MustCompile(`[^a-z0-9._-]+`)

// SnippetNameHint is the placeholder each front-end replaces with its own way
// of setting the name — `--name` in the CLI, `snippet_name` over MCP. The
// service cannot name either without being wrong on the other surface.
const SnippetNameHint = "<set-the-name-with>"

// DeriveSnippetName reproduces the broker's derivation exactly: lowercase,
// runs of anything outside [a-z0-9._-] collapsed to one hyphen, leading and
// trailing hyphens trimmed, truncated to the length bound.
//
// It is duplicated from the server on purpose and for one purpose only:
// telling the user *before* a request that the derivation will produce
// nothing. The derived name is never sent — the server derives the name it
// stores, so there is a single source of truth for what actually gets
// created and no way for the two to disagree about it.
func DeriveSnippetName(matchKey string) string {
	derived := snippetNameDerivePattern.ReplaceAllString(strings.ToLower(matchKey), "-")
	derived = strings.Trim(derived, "-")
	if len(derived) > snippetNameMaxLen {
		derived = derived[:snippetNameMaxLen]
	}
	return derived
}

// ValidateSnippetName reports whether name is usable as a snippet name.
//
// The message names the charset rather than echoing a regex: a user who typed
// a space needs to know spaces are fine in the match key and not in the name,
// which is the whole distinction this flag exists to draw.
func ValidateSnippetName(name string) error {
	if name == "" {
		return &Error{Code: CodeInvalidInput, Message: "snippet name cannot be empty"}
	}
	// Counted in runes, not bytes: len() on a multi-byte name would report a
	// number the user cannot reconcile with what they typed. The charset check
	// below rejects non-ASCII anyway, so this only ever fires for a genuinely
	// long ASCII name — but the message has to be honest either way.
	if n := utf8.RuneCountInString(name); n > snippetNameMaxLen {
		return &Error{
			Code:    CodeInvalidInput,
			Message: fmt.Sprintf("snippet name is %d characters; the maximum is %d", n, snippetNameMaxLen),
		}
	}
	if !snippetNamePattern.MatchString(name) {
		return &Error{
			Code: CodeInvalidInput,
			Message: fmt.Sprintf(
				"snippet name %q may only contain letters, digits, dot, underscore and hyphen — the match key may contain spaces, the snippet name may not",
				name),
		}
	}
	return nil
}

// SnippetPullResult carries the task identification returned by the pull
// endpoint. The actual pulled content is delivered asynchronously via the
// task's payload/message; callers that want to wait should poll TaskGet.
type SnippetPullResult struct {
	Task       string `json:"task"`
	Name       string `json:"name"`
	ConfigType string `json:"config_type"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// SnippetPull asks a device to pull a config-snippet payload back to the
// platform.
func (s *Service) SnippetPull(ctx context.Context, org, deviceName string, opts SnippetPullOpts) (*SnippetPullResult, error) {
	if deviceName == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "device name is required"}
	}
	if opts.Name == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "match key is required"}
	}
	if opts.SnippetName != "" {
		if err := ValidateSnippetName(opts.SnippetName); err != nil {
			return nil, err
		}
	} else if opts.AutoCreate || opts.Overwrite {
		// A pull that will create or update a snippet needs a name for it.
		// The server derives one, but only after dispatching a task to the
		// device — so an underivable match key costs a round trip and a
		// device task, and then fails with a message about the object rather
		// than about the name. Worse, if the object does not exist the user
		// only ever sees "not found" and never learns that --name is the way
		// through.
		//
		// A display-only pull creates nothing, so it needs no name and is
		// left alone.
		if DeriveSnippetName(opts.Name) == "" {
			return nil, &Error{
				Code: CodeInvalidInput,
				Message: fmt.Sprintf(
					"Could not derive a snippet name from '%s': nothing survives outside a-z, 0-9, '.', '_', and '-'. Pass %s to set the snippet name explicitly.",
					opts.Name, SnippetNameHint),
			}
		}
	}

	q := url.Values{}
	q.Set("name", opts.Name)
	// Omitted entirely when empty: the server derives a name from the match
	// key, and sending an empty snippet_name would be a different request.
	if opts.SnippetName != "" {
		q.Set("snippet_name", opts.SnippetName)
	}
	if opts.ConfigType != "" {
		q.Set("config_type", opts.ConfigType)
	}
	if opts.AutoCreate {
		q.Set("auto_create", "true")
	}
	if opts.Overwrite {
		q.Set("overwrite", "true")
	}
	endpoint := fmt.Sprintf("/api/v1/organizations/%s/devices/%s/pull?%s", org, deviceName, q.Encode())

	resp, err := s.api.Post(ctx, endpoint, nil)
	if err != nil {
		return nil, wrapAPI("%v", err)
	}
	var result SnippetPullResult
	if err := api.ParseResponse(resp, &result); err != nil {
		return nil, wrapAPI("%v", err)
	}
	return &result, nil
}
