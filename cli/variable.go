package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/helpers"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// variableScopeMeta tells the cobra builders how each scope is named on the
// CLI surface. The actual URL/parameter logic lives in service.
type variableScopeMeta struct {
	scope          service.VariableScope
	displayName    string
	entityName     string
	requiresEntity bool
}

var variableScopeMetas = map[string]variableScopeMeta{
	"org":      {scope: service.VarScopeOrg, displayName: "organization", requiresEntity: false},
	"ou":       {scope: service.VarScopeOU, displayName: "OU", requiresEntity: true, entityName: "OU"},
	"template": {scope: service.VarScopeTemplate, displayName: "template", requiresEntity: true, entityName: "template"},
	"device":   {scope: service.VarScopeDevice, displayName: "device", requiresEntity: true, entityName: "device"},
}

var variableCmd = &cobra.Command{
	Use:     "variable",
	Aliases: []string{"var"},
	Short:   "Variable management commands",
	Long:    "Manage configuration variables at different scopes (organization, OU, template, device)",
}

var varOrgCmd = &cobra.Command{Use: "org", Short: "Manage organization-level variables"}
var varOUCmd = &cobra.Command{Use: "ou", Short: "Manage OU-level variables"}
var varTemplateCmd = &cobra.Command{Use: "template", Short: "Manage template-level variables"}
var varDeviceCmd = &cobra.Command{Use: "device", Short: "Manage device-level variables"}

var varOverviewCmd = &cobra.Command{
	Use:   "overview",
	Short: "Show all variables with their definitions across scopes",
	RunE:  runVarOverview,
}

func init() {
	variableCmd.AddCommand(varOrgCmd)
	variableCmd.AddCommand(varOUCmd)
	variableCmd.AddCommand(varTemplateCmd)
	variableCmd.AddCommand(varDeviceCmd)
	variableCmd.AddCommand(varOverviewCmd)

	varOverviewCmd.Flags().String("name", "", "Filter by name pattern (regex)")
	varOverviewCmd.Flags().Int("page", 1, "Page number")
	varOverviewCmd.Flags().Int("per-page", 50, "Items per page")

	for scopeName, meta := range variableScopeMetas {
		scopeCmd := getVariableScopeCmd(scopeName)
		scopeCmd.AddCommand(makeVarListCommand(meta))
		scopeCmd.AddCommand(makeVarDescribeCommand(meta))
		scopeCmd.AddCommand(makeVarCreateCommand(meta))
		scopeCmd.AddCommand(makeVarSetCommand(meta))
		scopeCmd.AddCommand(makeVarDeleteCommand(meta))
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
func makeVarListCommand(meta variableScopeMeta) *cobra.Command {
	use, short, args := varListUseShortArgs(meta)
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE:  makeVarListHandler(meta),
	}
	cmd.Flags().Int("page", 1, "Page number")
	cmd.Flags().Int("per-page", 50, "Items per page")
	cmd.Flags().String("name", "", "Filter by name pattern (regex)")
	if meta.requiresEntity {
		cmd.ValidArgsFunction = getEntityCompleter(string(meta.scope))
	}
	return cmd
}

func varListUseShortArgs(meta variableScopeMeta) (string, string, cobra.PositionalArgs) {
	if meta.requiresEntity {
		return fmt.Sprintf("list [%s]", meta.entityName),
			fmt.Sprintf("List variables for a %s", meta.displayName),
			cobra.ExactArgs(1)
	}
	return "list",
		fmt.Sprintf("List %s-level variables", meta.displayName),
		cobra.NoArgs
}

func makeVarListHandler(meta variableScopeMeta) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()
		entity := ""
		if meta.requiresEntity {
			entity = args[0]
		}
		opts := service.VariableListOpts{}
		opts.NameFilter, _ = cmd.Flags().GetString("name")
		opts.Page, _ = cmd.Flags().GetInt("page")
		opts.PerPage, _ = cmd.Flags().GetInt("per-page")

		result, err := svc.VariableList(context.Background(), meta.scope, org, entity, opts)
		if err != nil {
			return err
		}
		if err := formatter.FormatVariables(result.Variables, result.Total); err != nil {
			return err
		}
		output.PrintPagination(result.Page, result.Total, result.PerPage)
		return nil
	}
}

// makeVarDescribeCommand creates a describe command for a scope
func makeVarDescribeCommand(meta variableScopeMeta) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs
	if meta.requiresEntity {
		use = fmt.Sprintf("describe [%s] [variable]", meta.entityName)
		short = fmt.Sprintf("Show details of a %s variable", meta.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "describe [variable]"
		short = fmt.Sprintf("Show details of an %s-level variable", meta.displayName)
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: makeVarDescribeHandler(meta)}
	if meta.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(string(meta.scope))
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}
	return cmd
}

func makeVarDescribeHandler(meta variableScopeMeta) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()
		var entity, varName string
		if meta.requiresEntity {
			entity, varName = args[0], args[1]
		} else {
			varName = args[0]
		}
		variable, err := svc.VariableGet(context.Background(), meta.scope, org, entity, varName)
		if err != nil {
			return err
		}
		return formatter.FormatVariable(variable)
	}
}

// makeVarCreateCommand creates a create command for a scope
func makeVarCreateCommand(meta variableScopeMeta) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs
	if meta.requiresEntity {
		use = fmt.Sprintf("create [%s] [name] [value]", meta.entityName)
		short = fmt.Sprintf("Create a %s variable", meta.displayName)
		args = cobra.ExactArgs(3)
	} else {
		use = "create [name] [value]"
		short = fmt.Sprintf("Create an %s-level variable", meta.displayName)
		args = cobra.ExactArgs(2)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: makeVarCreateHandler(meta)}
	cmd.Flags().String("description", "", "Variable description")
	if meta.scope == service.VarScopeOrg {
		cmd.Flags().Bool("secret", false, "Mark variable as secret (value will be redacted in API responses)")
	}
	if meta.requiresEntity {
		cmd.ValidArgsFunction = getEntityCompleter(string(meta.scope))
	}
	return cmd
}

func makeVarCreateHandler(meta variableScopeMeta) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName, value string
		if meta.requiresEntity {
			entity, varName, value = args[0], args[1], args[2]
		} else {
			varName, value = args[0], args[1]
		}
		description, _ := cmd.Flags().GetString("description")
		secret := false
		if meta.scope == service.VarScopeOrg {
			secret, _ = cmd.Flags().GetBool("secret")
		}

		variable, err := svc.VariableCreate(context.Background(), meta.scope, org, entity, service.VariableCreateOpts{
			Name:        varName,
			Value:       value,
			Description: description,
			Secret:      secret,
		})
		if err != nil {
			// Hint when an override fails because the org-scope parent doesn't exist.
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.Code == "INVALID_PARAM" && strings.Contains(apiErr.Message, "must exist before creating overrides") {
				return fmt.Errorf("%s\n\nTip: Create the org-scope variable first:\n  ndcli variable org create %s <default-value>", apiErr.Message, varName)
			}
			return err
		}

		if meta.requiresEntity {
			color.Green("Variable created: %s (at %s '%s')", varName, meta.displayName, entity)
		} else {
			secretIndicator := ""
			if variable.Secret {
				secretIndicator = " [SECRET]"
			}
			color.Green("Variable created: %s (at %s level)%s", varName, meta.displayName, secretIndicator)
		}
		return nil
	}
}

// makeVarSetCommand creates a set (update) command for a scope
func makeVarSetCommand(meta variableScopeMeta) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs
	if meta.requiresEntity {
		use = fmt.Sprintf("set [%s] [variable]", meta.entityName)
		short = fmt.Sprintf("Update a %s variable", meta.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "set [variable]"
		short = fmt.Sprintf("Update an %s-level variable", meta.displayName)
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: makeVarSetHandler(meta)}
	cmd.Flags().String("value", "", "New value for the variable")
	cmd.Flags().String("description", "", "New description for the variable")
	if meta.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(string(meta.scope))
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}
	return cmd
}

func makeVarSetHandler(meta variableScopeMeta) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName string
		if meta.requiresEntity {
			entity, varName = args[0], args[1]
		} else {
			varName = args[0]
		}

		opts := service.VariableSetOpts{}
		if cmd.Flags().Changed("value") {
			v, _ := cmd.Flags().GetString("value")
			opts.Value = &v
		}
		if cmd.Flags().Changed("description") {
			v, _ := cmd.Flags().GetString("description")
			opts.Description = &v
		}
		if opts.Value == nil && opts.Description == nil {
			return fmt.Errorf("at least one of --value or --description must be provided")
		}
		if _, err := svc.VariableSet(context.Background(), meta.scope, org, entity, varName, opts); err != nil {
			return err
		}
		color.Green("Variable updated: %s", varName)
		return nil
	}
}

// makeVarDeleteCommand creates a delete command for a scope
func makeVarDeleteCommand(meta variableScopeMeta) *cobra.Command {
	var use, short string
	var args cobra.PositionalArgs
	if meta.requiresEntity {
		use = fmt.Sprintf("delete [%s] [variable]", meta.entityName)
		short = fmt.Sprintf("Delete a %s variable", meta.displayName)
		args = cobra.ExactArgs(2)
	} else {
		use = "delete [variable]"
		short = fmt.Sprintf("Delete an %s-level variable", meta.displayName)
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: args, RunE: makeVarDeleteHandler(meta)}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	if meta.requiresEntity {
		cmd.ValidArgsFunction = getEntityThenVariableCompleter(string(meta.scope))
	} else {
		cmd.ValidArgsFunction = completeOrgVariables
	}
	return cmd
}

func makeVarDeleteHandler(meta variableScopeMeta) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		requireAuth()
		org := requireOrganization()

		var entity, varName string
		if meta.requiresEntity {
			entity, varName = args[0], args[1]
		} else {
			varName = args[0]
		}

		ctx := context.Background()
		skipConfirm, _ := cmd.Flags().GetBool("yes")

		// Org-scope deletes cascade to other-scope overrides — warn first.
		if meta.scope == service.VarScopeOrg {
			if overrideCount := svc.VariableCountNonOrgDefinitions(ctx, org, varName); overrideCount > 0 {
				color.Yellow("WARNING: This will also delete %d override(s) at other scopes (OU, template, device)", overrideCount)
			}
		}

		if !skipConfirm {
			msg := fmt.Sprintf("Delete variable '%s'", varName)
			if meta.requiresEntity {
				msg += fmt.Sprintf(" from %s '%s'", meta.entityName, entity)
			}
			msg += "?"
			if !helpers.Confirm(msg) {
				fmt.Println("Cancelled")
				return nil
			}
		}

		if err := svc.VariableDelete(ctx, meta.scope, org, entity, varName); err != nil {
			return err
		}
		color.Green("Variable deleted: %s", varName)
		return nil
	}
}

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

func runVarOverview(cmd *cobra.Command, args []string) error {
	requireAuth()
	org := requireOrganization()

	opts := service.VariableOverviewOpts{}
	opts.Page, _ = cmd.Flags().GetInt("page")
	opts.PerPage, _ = cmd.Flags().GetInt("per-page")
	opts.NameFilter, _ = cmd.Flags().GetString("name")

	result, err := svc.VariableOverview(context.Background(), org, opts)
	if err != nil {
		return err
	}
	if err := formatter.FormatVariableOverview(result.Variables, result.Total); err != nil {
		return err
	}
	output.PrintPagination(result.Page, result.Total, result.PerPage)
	return nil
}
