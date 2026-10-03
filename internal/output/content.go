package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// writeContent prints the content block that the human formats show under a
// described snippet or software policy: the stored JSON indented, or the
// content as stored when it does not parse. The JSON formatter carries content
// as a field instead.
func writeContent(w io.Writer, content string) {
	shown := content
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(strings.TrimSpace(content)), "", "  "); err == nil {
		shown = pretty.String()
	}
	fmt.Fprintln(w)
	ColorLabel.Fprintln(w, "Content:")
	fmt.Fprintln(w, shown)
}
