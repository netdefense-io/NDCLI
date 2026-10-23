package cli

import (
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

// explicitBool is a bool flag that must be given its value: --flag=false or
// --flag false. A Bool flag reads a bare --flag as true and leaves the next
// word as an argument, so "--boot-environment false" asked for true and named
// a device "false", and a bare --auto-apply-major turned major upgrades on.
type explicitBool bool

func (b *explicitBool) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return errors.New("want true or false")
	}
	*b = explicitBool(v)
	return nil
}

func (b *explicitBool) String() string { return strconv.FormatBool(bool(*b)) }

func (b *explicitBool) Type() string { return "true|false" }

// addExplicitBoolFlag declares an explicitBool flag on cmd.
func addExplicitBoolFlag(cmd *cobra.Command, name, usage string) {
	cmd.Flags().Var(new(explicitBool), name, usage)
	_ = cmd.RegisterFlagCompletionFunc(name, cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp))
}

// explicitBoolFlag returns the value given to an explicitBool flag, or nil
// when the flag was not given.
func explicitBoolFlag(cmd *cobra.Command, name string) *bool {
	f := cmd.Flags().Lookup(name)
	if f == nil || !f.Changed {
		return nil
	}
	v := bool(*f.Value.(*explicitBool))
	return &v
}
