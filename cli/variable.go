package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/output"
)

// variableScope defines scope-specific configuration for variable commands
type variableScope struct {
	name           string
	displayName    string
	requiresEntity bool
	entityName     string
	buildURL       func(org, entity string) string
}

var variableScopes = map[string]variableScope{
	"org": {
		name:           "org",
		displayName:    "organization",
		requiresEntity: false,
		buildURL: func(org, _ string) string {
			return fmt.Sprintf("/api/v1/organizations/%s/variables", url.PathEscape(org))
		},
	},
	"ou": {
		name:           "ou",
		displayName:    "OU",
		requiresEntity: true,
		entityName:     "OU",
		buildURL: func(org, ou string) string {
			return fmt.Sprintf("/api/v1/organizations/%s/ous/%s/variables", url.PathEscape(org), url.PathEscape(ou))
		},
	},
	"template": {
		name:           "template",
		displayName:    "template",
		requiresEntity: true,
		entityName:     "template",
		buildURL: func(org, template string) string {
			return fmt.Sprintf("/api/v1/organizations/%s/templates/%s/variables", url.PathEscape(org), url.PathEscape(template))
		},
	},
	"device": {
		name:           "device",
		displayName:    "device",
		requiresEntity: true,
		entityName:     "device",
		buildURL: func(org, device string) string {
			return fmt.Sprintf("/api/v1/organizations/%s/devices/%s/variables", url.PathEscape(org), url.PathEscape(device))
		},
	},
}

// Root variable command
var variableCmd = &cobra.Command{
	Use:     "variable",
	Aliases: []string{"var"},
	Short:   "Variable management commands",
	Long:    "Manage configuration variables at different scopes (organization, OU, template, device)",
}

// Scope subcommands
var varOrgCmd = &cobra.Command{
	Use:   "org",
	Short: "Manage organization-level variables",
}

var varOUCmd = &cobra.Command{
	Use:   "ou",
	Short: "Manage OU-level variables",
}

var varTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage template-level variables",
}

var varDeviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage device-level variables",
}

var varOverviewCmd = &cobra.Command{
	Use:   "overview",
	Short: "Show all variables with their definitions across scopes",
	RunE:  runVarOverview,
}

func init() {
	// Add scope subcommands to variable
	variableCmd.AddCommand(varOrgCmd)
	variableCmd.AddCommand(varOUCmd)
	variableCmd.AddCommand(varTemplateCmd)
	variableCmd.AddCommand(varDeviceCmd)
	variableCmd.AddCommand(varOverviewCmd)

	// Overview command flags
	varOverviewCmd.Flags().String("name", "", "Filter by name pattern (regex)")
	varOverviewCmd.Flags().Int("page", 1, "Page number")
	varOverviewCmd.Flags().Int("per-page", 50, "Items per page")

	// Build and add CRUD commands for each scope
	for scopeName, scope := range variableScopes {
		scopeCmd := getVariableScopeCmd(scopeName)
		scopeCmd.AddCommand(makeVarListCommand(scope))
		scopeCmd.AddCommand(makeVarDescribeCommand(scope))
		scopeCmd.AddCommand(makeVarCreateCommand(scope))
		scopeCmd.AddCommand(makeVarSetCommand(scope))
		scopeCmd.AddCommand(makeVarDeleteCommand(scope))
	}
}

func getVariableScopeCmd(name string) *cobra.Command {
	switch name {
	case "org":
		return varOrgCmd
	case "ou":
		return varOUCmd
	case "template":
		return varTemplateCmd
	case "device":
		return varDeviceCmd
	}
	return nil
}

// makeVarListCommand creates a list command for a scope
func makeVarListCommand(scope variableScope) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs

	if scope.requiresEntity {
		use = fmt.Sprintf("list [%s]", scope.entityName)
		short = fmt.Sprintf("List variables for a %s", scope.displayName)
		args = cobra.ExactArgs(1)
	} else {
		use = "list"
		short = fmt.Sprintf("List %s-level variables", scope.displayName)
		args = cobra.NoArgs
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarListHandler(scope),
	}

	cmd.Flags().Int("page", 1, "Page number")
	cmd.Flags().Int("per-page", 50, "Items per page")
	cmd.Flags().String("name", "", "Filter by name pattern (regex)")

	// Set up completions
	if scope.requiresEntity {
		cmd.ValidArgsFunction = getEntityCompleter(scope.name)
	}

	return cmd
}

func makeVarListHandler(scope variableScope) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		entity := ""
		if scope.requiresEntity {
			entity = args[0]
		}

		page, _ := cmd.Flags().GetInt("page")
		perPage, _ := cmd.Flags().GetInt("per-page")
		nameFilter, _ := cmd.Flags().GetString("name")

		params := map[string]string{
			"page":     strconv.Itoa(page),
			"per_page": strconv.Itoa(perPage),
		}
		if nameFilter != "" {
			params["name_filter"] = nameFilter
		}

		ctx := context.Background()
		resp, err := apiClient.Get(ctx, scope.buildURL(org, entity), params)
		if err != nil {
			return err
		}

		var result models.VariableListResponse
		if err := api.ParseResponse(resp, &result); err != nil {
			return err
		}

		if err := formatter.FormatVariables(result.Items, result.Total); err != nil {
			return err
		}

		output.PrintPagination(page, result.Total, perPage)
		return nil
	}
}

// makeVarDescribeCommand creates a describe command for a scope
func makeVarDescribeCommand(scope variableScope) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs

	if scope.requiresEntity {
		use = fmt.Sprintf("describe [%s] [variable]", scope.entityName)
		short = fmt.Sprintf("Show details of a %s variable", scope.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "describe [variable]"
		short = fmt.Sprintf("Show details of an %s-level variable", scope.displayName)
		args = cobra.ExactArgs(1)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarDescribeHandler(scope),
	}

	// Set up completions
	if scope.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(scope.name)
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}

	return cmd
}

func makeVarDescribeHandler(scope variableScope) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName string
		if scope.requiresEntity {
			entity = args[0]
			varName = args[1]
		} else {
			varName = args[0]
		}

		ctx := context.Background()
		urlPath := scope.buildURL(org, entity) + "/" + url.PathEscape(varName)
		resp, err := apiClient.Get(ctx, urlPath, nil)
		if err != nil {
			return err
		}

		var variable models.Variable
		if err := api.ParseResponse(resp, &variable); err != nil {
			return err
		}

		return formatter.FormatVariable(&variable)
	}
}

// makeVarCreateCommand creates a create command for a scope
func makeVarCreateCommand(scope variableScope) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs

	if scope.requiresEntity {
		use = fmt.Sprintf("create [%s] [name] [value]", scope.entityName)
		short = fmt.Sprintf("Create a %s variable", scope.displayName)
		args = cobra.ExactArgs(3)
	} else {
		use = "create [name] [value]"
		short = fmt.Sprintf("Create an %s-level variable", scope.displayName)
		args = cobra.ExactArgs(2)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarCreateHandler(scope),
	}

	cmd.Flags().String("description", "", "Variable description")

	// Add --secret flag only for org scope (secret is only valid at org level)
	if scope.name == "org" {
		cmd.Flags().Bool("secret", false, "Mark variable as secret (value will be redacted in API responses)")
	}

	// Set up completions for entity only (variable name is new)
	if scope.requiresEntity {
		cmd.ValidArgsFunction = getEntityCompleter(scope.name)
	}

	return cmd
}

func makeVarCreateHandler(scope variableScope) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName, value string
		if scope.requiresEntity {
			entity = args[0]
			varName = args[1]
			value = args[2]
		} else {
			varName = args[0]
			value = args[1]
		}

		description, _ := cmd.Flags().GetString("description")

		payload := map[string]interface{}{
			"name":  varName,
			"value": value,
		}
		if description != "" {
			payload["description"] = description
		}

		// Add secret flag for org scope only
		if scope.name == "org" {
			secret, _ := cmd.Flags().GetBool("secret")
			if secret {
				payload["secret"] = true
			}
		}

		ctx := context.Background()
		resp, err := apiClient.Post(ctx, scope.buildURL(org, entity), payload)
		if err != nil {
			return err
		}

		var variable models.Variable
		if err := api.ParseResponse(resp, &variable); err != nil {
			// Enhance INVALID_PARAM error for missing org-scope parent
			if apiErr, ok := err.(*api.APIError); ok && apiErr.Code == "INVALID_PARAM" {
				if strings.Contains(apiErr.Message, "must exist before creating overrides") {
					return fmt.Errorf("%s\n\nTip: Create the org-scope variable first:\n  ndcli variable org create %s <default-value>", apiErr.Message, varName)
				}
			}
			return err
		}

		if scope.requiresEntity {
			color.Green("Variable created: %s (at %s '%s')", varName, scope.displayName, entity)
		} else {
			secretIndicator := ""
			if variable.Secret {
				secretIndicator = " [SECRET]"
			}
			color.Green("Variable created: %s (at %s level)%s", varName, scope.displayName, secretIndicator)
		}
		return nil
	}
}

// makeVarSetCommand creates a set (update) command for a scope
func makeVarSetCommand(scope variableScope) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs

	if scope.requiresEntity {
		use = fmt.Sprintf("set [%s] [variable]", scope.entityName)
		short = fmt.Sprintf("Update a %s variable", scope.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "set [variable]"
		short = fmt.Sprintf("Update an %s-level variable", scope.displayName)
		args = cobra.ExactArgs(1)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarSetHandler(scope),
	}

	cmd.Flags().String("value", "", "New value for the variable")
	cmd.Flags().String("description", "", "New description for the variable")

	// Set up completions
	if scope.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(scope.name)
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}

	return cmd
}

func makeVarSetHandler(scope variableScope) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName string
		if scope.requiresEntity {
			entity = args[0]
			varName = args[1]
		} else {
			varName = args[0]
		}

		value, valueSet := cmd.Flags().GetString("value")
		description, descSet := cmd.Flags().GetString("description")

		// Check if at least one flag is provided
		if !cmd.Flags().Changed("value") && !cmd.Flags().Changed("description") {
			return fmt.Errorf("at least one of --value or --description must be provided")
		}

		payload := make(map[string]interface{})
		if cmd.Flags().Changed("value") {
			payload["value"] = value
		}
		if cmd.Flags().Changed("description") {
			payload["description"] = description
		}

		// Avoid unused variable warnings
		_ = valueSet
		_ = descSet

		ctx := context.Background()
		urlPath := scope.buildURL(org, entity) + "/" + url.PathEscape(varName)
		resp, err := apiClient.Patch(ctx, urlPath, payload)
		if err != nil {
			return err
		}

		var variable models.Variable
		if err := api.ParseResponse(resp, &variable); err != nil {
			return err
		}

		color.Green("Variable updated: %s", varName)
		return nil
	}
}

// makeVarDeleteCommand creates a delete command for a scope
func makeVarDeleteCommand(scope variableScope) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs

	if scope.requiresEntity {
		use = fmt.Sprintf("delete [%s] [variable]", scope.entityName)
		short = fmt.Sprintf("Delete a %s variable", scope.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "delete [variable]"
		short = fmt.Sprintf("Delete an %s-level variable", scope.displayName)
		args = cobra.ExactArgs(1)
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarDeleteHandler(scope),
	}

	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")

	// Set up completions
	if scope.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(scope.name)
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}

	return cmd
}

func makeVarDeleteHandler(scope variableScope) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName string
		if scope.requiresEntity {
			entity = args[0]
			varName = args[1]
		} else {
			varName = args[0]
		}

		skipConfirm, _ := cmd.Flags().GetBool("yes")

		// For org-scope deletes, check for overrides that will be cascade-deleted
		overrideCount := 0
		if scope.name == "org" {
			overrideCount = countVariableOverrides(org, varName)
			if overrideCount > 0 {
				color.Yellow("WARNING: This will also delete %d override(s) at other scopes (OU, template, device)", overrideCount)
			}
		}

		if !skipConfirm {
			msg := fmt.Sprintf("Delete variable '%s'", varName)
			if scope.requiresEntity {
				msg += fmt.Sprintf(" from %s '%s'", scope.entityName, entity)
			}
			msg += "?"

			if !helpers.Confirm(msg) {
				fmt.Println("Cancelled")
				return nil
			}
		}

		ctx := context.Background()
		urlPath := scope.buildURL(org, entity) + "/" + url.PathEscape(varName)
		resp, err := apiClient.Delete(ctx, urlPath)
		if err != nil {
			return err
		}

		if err := api.ParseResponse(resp, nil); err != nil {
			return err
		}

		color.Green("Variable deleted: %s", varName)
		return nil
	}
}

// Helper functions for completions

func getEntityCompleter(scopeName string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	switch scopeName {
	case "ou":
		return completeOUs
	case "template":
		return completeTemplates
	case "device":
		return completeDevices
	}
	return nil
}

func getEntityThenVariableCompleter(scopeName string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	switch scopeName {
	case "ou":
		return completeOUThenVariable
	case "template":
		return completeTemplateThenVariable
	case "device":
		return completeDeviceThenVariable
	}
	return nil
}

// countVariableOverrides counts non-org-scope definitions for a variable
// Returns the count of overrides (OU, template, device scopes)
func countVariableOverrides(org, varName string) int {
	ctx := context.Background()
	urlPath := fmt.Sprintf("/api/v1/organizations/%s/variables", url.PathEscape(org))
	params := map[string]string{
		"scope": "all",
		"name":  fmt.Sprintf("^%s$", varName), // Exact match regex
	}

	resp, err := apiClient.Get(ctx, urlPath, params)
	if err != nil {
		return 0 // Can't count, proceed without warning
	}

	var result models.VariableOverviewResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return 0
	}

	// Find the variable and count non-org definitions
	for _, item := range result.Items {
		if item.Name == varName {
			count := 0
			for _, def := range item.Definitions {
				if def.Scope != "organization" {
					count++
				}
			}
			return count
		}
	}
	return 0
}

// runVarOverview handles the variable overview command
func runVarOverview(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	page, _ := cmd.Flags().GetInt("page")
	perPage, _ := cmd.Flags().GetInt("per-page")
	nameFilter, _ := cmd.Flags().GetString("name")

	params := map[string]string{
		"scope":    "all", // Request overview across all scopes
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	}
	if nameFilter != "" {
		params["name"] = nameFilter
	}

	ctx := context.Background()
	urlPath := fmt.Sprintf("/api/v1/organizations/%s/variables", url.PathEscape(org))
	resp, err := apiClient.Get(ctx, urlPath, params)
	if err != nil {
		return err
	}

	var result models.VariableOverviewResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return err
	}

	if err := formatter.FormatVariableOverview(result.Items, result.Total); err != nil {
		return err
	}

	output.PrintPagination(page, result.Total, perPage)
	return nil
}
