package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/output"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// commandErrorJSON is a failed command's error under -f json: the same
// {code, message} object an MCP tool returns as its error.
type commandErrorJSON struct {
	Error struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	} `json:"error"`
}

// printCommandError writes the error a command failed with. Cobra's own
// printing is silenced because it prints only the message, and NDManager's
// error code belongs in every format: "Error: [CODE] message" in the human
// ones, the code field of commandErrorJSON under -f json. Both go to the
// writer given, stderr in practice, so stdout stays empty on failure.
func printCommandError(w io.Writer, err error) {
	if !jsonErrors() {
		fmt.Fprintln(w, "Error:", errorText(err))
		return
	}
	var doc commandErrorJSON
	doc.Error.Code = api.GetErrorCode(err)
	doc.Error.Message = err.Error()
	// A struct of two strings always marshals.
	out, _ := json.MarshalIndent(doc, "", "  ")
	fmt.Fprintln(w, string(out))
}

// refusalWithList is a refusal in the command's own words, header followed by
// the resources that block it, one per line. It wraps err, so the code
// NDManager sent still shows and the command exits non-zero.
func refusalWithList(err error, header, listTitle string, items []string) error {
	var b strings.Builder
	b.WriteString(header)
	if listTitle != "" {
		b.WriteString("\n")
		b.WriteString(listTitle)
	}
	for _, item := range items {
		b.WriteString("\n  • ")
		b.WriteString(item)
	}
	return &service.Error{Code: service.CodeAPIError, Message: b.String(), Err: err}
}

// errorText is err's message with NDManager's error code in front, when the
// error carries one.
func errorText(err error) string {
	return output.WithCode(api.GetErrorCode(err), err.Error())
}

// jsonErrors reports whether errors are printed as JSON: the command's
// formatter is the JSON one or, for a command that failed before its
// formatter was set up, the format it would have run with is JSON.
func jsonErrors() bool {
	if formatter != nil {
		_, ok := formatter.(*output.JSONFormatter)
		return ok
	}
	return output.Format(outputFormat()) == output.FormatJSON
}
