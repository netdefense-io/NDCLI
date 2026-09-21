package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/sanitize"
)

// Sanitizer coverage guard.
//
// Server- and agent-supplied strings are scrubbed of terminal control
// sequences exactly once, at the decode boundary (api.DecodeJSON →
// sanitize.Struct). That single pass is only as good as its reach: a model
// field the reflective walk cannot follow silently delivers raw escape
// sequences to whichever formatter prints it. Device.Facts was such a field —
// a map[string]any whose values all report Kind() == Interface — and the gap
// was found by review, not by a test.
//
// The two tests below close that door for every future field. They derive
// their subject matter from the Formatter interface itself, so a new
// formatter method (and therefore a new model on the render path) is covered
// the moment it is declared, with no list to keep in sync:
//
//   - TestSanitizeStruct_ReachesEveryFormatterModelShape builds a value of
//     every type in every Formatter method signature with a control-sequence
//     marker in every string leaf — struct fields, pointers, slice elements,
//     map keys, map values, and values stored in an interface — runs
//     sanitize.Struct over it, and asserts nothing survives.
//   - TestFormatters_EmitNoControlSequences renders those same values through
//     all four formatters and asserts no control byte reaches the writer,
//     which additionally catches a formatter that derives printable output
//     from something the sanitizer does not rewrite.
//
// CR is deliberately absent from the marker set: sanitize.String preserves
// \r (alongside \t and \n) so multi-line server messages keep rendering, and
// these tests guard the sanitizer's stated contract rather than redefining
// it.

const guardToken = "NDGUARD"

// poisonString wraps a recognizable token in every control-sequence shape a
// hostile control plane would reach for: an SGR colour change, an OSC
// title-bar write, and the 8-bit CSI/OSC introducers that tmux and VTE-based
// terminals honour without an ESC prefix.
func poisonString(seed string) string {
	return "\x1b[31m" + seed + guardToken + "\x1b]0;pwn\x07\u009b2J\u009d0;x\x7f"
}

// hasControl reports whether s carries any rune sanitize.String must strip.
// It mirrors the package's own rule, including the \t \n \r exemptions.
func hasControl(s string) bool {
	for _, r := range s {
		switch r {
		case '\t', '\n', '\r':
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// escapedControlForms are the JSON encodings of the marker's control runes.
// The JSON formatter escapes rather than emits them, and `ndcli -f json | jq`
// turns them straight back into live escape sequences on the terminal, so an
// unsanitized string is just as dangerous there as in the table output.
//
// Only ESC and BEL are listed: encoding/json escapes runes below 0x20 and
// emits DEL and the C1 range as literal bytes, which the raw-control check on
// the rendered buffer already catches. A third entry here would be dead.
var escapedControlForms = []string{`\u001b`, `\u0007`}

var (
	timeType       = reflect.TypeOf(time.Time{})
	rawMessageType = reflect.TypeOf(json.RawMessage{})
)

// poisonValue builds a value of type t with poisonString in every string
// leaf it can reach, including map keys and values stored inside an
// interface. Types whose contents are not free-form server data —
// time.Time, json.RawMessage — get a valid stand-in instead, so formatters
// that parse them still behave.
//
// Recursion stops on the type graph, not on a nesting depth. A flat depth
// cap silently stopped generating before it reached the deeper shapes — a
// map field behind a slice of structs is already five levels down — which
// made a real gap read as a pass. seen counts how many times each composite
// type appears on the current path, so a self-referential model terminates
// while an ordinary deep one is still filled to its leaves.
func poisonValue(t reflect.Type, seen map[reflect.Type]int) reflect.Value {
	if seen[t] > 0 {
		return reflect.Zero(t)
	}
	seen[t]++
	defer func() { seen[t]-- }()

	switch t {
	case timeType:
		return reflect.ValueOf(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
	case rawMessageType:
		return reflect.ValueOf(json.RawMessage(`"` + guardToken + `"`))
	}

	switch t.Kind() {
	case reflect.String:
		v := reflect.New(t).Elem()
		v.SetString(poisonString(t.Name()))
		return v
	case reflect.Bool:
		return reflect.ValueOf(true).Convert(t)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(int64(1)).Convert(t)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(uint64(1)).Convert(t)
	case reflect.Float32, reflect.Float64:
		return reflect.ValueOf(float64(1)).Convert(t)
	case reflect.Ptr:
		p := reflect.New(t.Elem())
		p.Elem().Set(poisonValue(t.Elem(), seen))
		return p
	case reflect.Slice:
		s := reflect.MakeSlice(t, 1, 1)
		s.Index(0).Set(poisonValue(t.Elem(), seen))
		return s
	case reflect.Array:
		a := reflect.New(t).Elem()
		for i := 0; i < a.Len(); i++ {
			a.Index(i).Set(poisonValue(t.Elem(), seen))
		}
		return a
	case reflect.Map:
		m := reflect.MakeMap(t)
		m.SetMapIndex(poisonValue(t.Key(), seen), poisonValue(t.Elem(), seen))
		return m
	case reflect.Struct:
		v := reflect.New(t).Elem()
		for i := 0; i < t.NumField(); i++ {
			if !v.Field(i).CanSet() {
				continue
			}
			v.Field(i).Set(poisonValue(t.Field(i).Type, seen))
		}
		return v
	case reflect.Interface:
		if t.NumMethod() > 0 {
			return reflect.Zero(t)
		}
		// The cargo shape a JSON decode produces for an unmodeled key:
		// strings, nested objects, and arrays, all behind interface values.
		return reflect.ValueOf(any(map[string]any{
			poisonString("key"): poisonString("value"),
			"nested": map[string]any{
				poisonString("innerkey"): poisonString("innervalue"),
			},
			"list": []any{poisonString("item")},
		}))
	}
	return reflect.Zero(t)
}

// findControl walks v read-only and records the path of every string —
// field, slice element, map key, map value, or interface content — that
// still carries a control rune.
func findControl(v reflect.Value, path string, found *[]string) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		if hasControl(v.String()) {
			*found = append(*found, fmt.Sprintf("%s = %q", path, v.String()))
		}
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return
		}
		findControl(v.Elem(), path, found)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			findControl(v.Index(i), fmt.Sprintf("%s[%d]", path, i), found)
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		for _, key := range v.MapKeys() {
			findControl(key, path+"<key>", found)
			findControl(v.MapIndex(key), fmt.Sprintf("%s[%v]", path, key), found)
		}
	case reflect.Struct:
		if v.Type() == timeType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			// An unexported field cannot be read safely here, and
			// sanitize.Struct documents it as an intentional no-op.
			if v.Type().Field(i).PkgPath != "" {
				continue
			}
			findControl(v.Field(i), path+"."+v.Type().Field(i).Name, found)
		}
	}
}

// formatterArgTypes returns the input types of one Formatter method.
func formatterArgTypes(m reflect.Method) []reflect.Type {
	types := make([]reflect.Type, 0, m.Type.NumIn())
	for i := 0; i < m.Type.NumIn(); i++ {
		types = append(types, m.Type.In(i))
	}
	return types
}

// TestSanitizeStruct_ReachesEveryFormatterModelShape is the shape guard: for
// every type that appears in a Formatter method signature, a value with a
// control-sequence marker in every reachable string leaf must come out of
// sanitize.Struct with none of them left. A new field, nested map, slice, or
// any-typed cargo value that the sanitizer's walk cannot follow fails here.
func TestSanitizeStruct_ReachesEveryFormatterModelShape(t *testing.T) {
	iface := reflect.TypeOf((*Formatter)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		for _, argType := range formatterArgTypes(m) {
			name := fmt.Sprintf("%s/%s", m.Name, argType)
			t.Run(name, func(t *testing.T) {
				holder := reflect.New(argType)
				holder.Elem().Set(poisonValue(argType, map[reflect.Type]int{}))

				sanitize.Struct(holder)

				var found []string
				findControl(holder.Elem(), argType.String(), &found)
				for _, f := range found {
					t.Errorf("control sequence survived sanitize.Struct at %s", f)
				}
			})
		}
	}
}

// formatterFactories builds each formatter over a caller-supplied writer.
// These are the concrete types GetFormatter hands the commands.
var formatterFactories = map[string]func(w io.Writer) Formatter{
	"table":    func(w io.Writer) Formatter { return &TableFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
	"simple":   func(w io.Writer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
	"detailed": func(w io.Writer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: w}} },
	"json": func(w io.Writer) Formatter {
		return &JSONFormatter{BaseFormatter: BaseFormatter{Writer: w}, Indent: true}
	},
}

// TestFormatters_EmitNoControlSequences is the render guard: every Formatter
// method, on every formatter, fed a sanitized payload that was poisoned in
// every reachable string leaf, must write no control sequence to the
// terminal. It covers what the shape guard cannot — a formatter printing
// something derived from data sanitize.Struct does not rewrite, such as a
// map key.
func TestFormatters_EmitNoControlSequences(t *testing.T) {
	// Keep the check deterministic: the formatters' own ANSI colouring is
	// off under `go test` (stdout is not a TTY) and must stay off, so any
	// control byte in the output came from the payload.
	restore := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = restore })

	iface := reflect.TypeOf((*Formatter)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		argTypes := formatterArgTypes(m)
		for fname, factory := range formatterFactories {
			t.Run(m.Name+"/"+fname, func(t *testing.T) {
				args := make([]reflect.Value, 0, len(argTypes))
				for _, argType := range argTypes {
					holder := reflect.New(argType)
					holder.Elem().Set(poisonValue(argType, map[reflect.Type]int{}))
					sanitize.Struct(holder)
					args = append(args, holder.Elem())
				}

				var buf bytes.Buffer
				stdout := captureStdout(t, func() {
					callFormatter(t, factory(&buf), m.Name, args)
				})

				// The table formatter prints with fmt.Printf rather than
				// through BaseFormatter.Writer, so its output arrives on
				// stdout. Checking only the buffer would have left a
				// quarter of the render surface unexamined.
				out := buf.String() + stdout
				if hasControl(out) {
					t.Errorf("%s rendered a control sequence: %q", m.Name, firstControlContext(out))
				}
				for _, esc := range escapedControlForms {
					if strings.Contains(out, esc) {
						t.Errorf("%s rendered the escaped control sequence %s, which a JSON consumer decodes back into a live one", m.Name, esc)
					}
				}
			})
		}
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written there. These tests do not run in parallel, so the swap is safe.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	// Restored with defer rather than after fn: a t.Fatal inside fn would
	// runtime.Goexit past a bare restore, leaving every later test in this
	// binary writing into a closed pipe. Closing w twice on the normal path
	// is harmless.
	defer func() {
		os.Stdout = orig
		w.Close()
		r.Close()
	}()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	w.Close()
	return <-done
}

// callFormatter invokes one formatter method reflectively. A panic is a
// finding in its own right — server data must never crash a render — so it
// is reported rather than allowed to abort the run.
func callFormatter(t *testing.T, f Formatter, method string, args []reflect.Value) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked on poisoned input: %v", method, r)
		}
	}()
	out := reflect.ValueOf(f).MethodByName(method).Call(args)
	for _, rv := range out {
		if err, ok := rv.Interface().(error); ok && err != nil {
			t.Errorf("%s returned an error on poisoned input: %v", method, err)
		}
	}
}

// firstControlContext returns a short window around the first offending
// rune, so a failure names the line that leaked rather than the whole render.
func firstControlContext(s string) string {
	for i, r := range s {
		switch r {
		case '\t', '\n', '\r':
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			start := i - 40
			if start < 0 {
				start = 0
			}
			end := i + 40
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		}
	}
	return ""
}

// TestDeviceFactsWireDecode_IsSanitized pins the specific gap this guard was
// written for: the agent-reported facts map arrives as unmodeled cargo, and
// every string in it — nested objects and arrays included — must be scrubbed
// by the real decode path before any formatter sees it.
func TestDeviceFactsWireDecode_IsSanitized(t *testing.T) {
	const (
		esc = "\\u001b" // the JSON encoding of ESC, as NDManager would send it
		bel = "\\u0007"
		csi = "\\u009b" // 8-bit CSI introducer
	)
	wire := `{
	  "name": "e2e-a",
	  "facts": {
	    "hostname": "fw` + esc + `[2Jhost",
	    "os": {"platform": "FreeBSD` + esc + `]0;pwn` + bel + `", "version": "15.0"},
	    "interfaces": [{"role": "wan", "if": "vtnet0", "descr": "WAN` + csi + `2J"}],
	    "newfact` + esc + `[31m": "value"
	  }
	}`

	var device models.Device
	if err := json.Unmarshal([]byte(wire), &device); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sanitize.Struct(reflect.ValueOf(&device))

	var found []string
	findControl(reflect.ValueOf(&device).Elem(), "Device", &found)
	for _, f := range found {
		t.Errorf("control sequence survived the decode path at %s", f)
	}

	var buf bytes.Buffer
	f := &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatDevice(&device); err != nil {
		t.Fatalf("FormatDevice: %v", err)
	}
	if hasControl(buf.String()) {
		t.Errorf("device facts rendered a control sequence: %q", firstControlContext(buf.String()))
	}
}
