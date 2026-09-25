package output

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written there. Several formatters — the table renderer above all —
// print with fmt.Printf rather than through BaseFormatter.Writer, so a
// buffer on the formatter would come back empty. These tests do not run in
// parallel, so the swap is safe.
//
// The pipe is drained concurrently: a render larger than the pipe buffer
// would otherwise block fn forever, and the write-then-read-after ordering
// that invites it has no upper bound on output size.
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

// TestCaptureStdout_SurvivesOutputLargerThanThePipeBuffer is the regression
// guard for the pattern this helper replaced. A pipe buffer is 64 KiB on
// Linux and smaller on some platforms, so an undrained capture blocks the
// writer partway through and the test hangs until the go test deadline
// rather than failing. Anything that reintroduces read-after-close here
// stops the suite on this test instead of on whichever formatter happens to
// grow a long render first.
func TestCaptureStdout_SurvivesOutputLargerThanThePipeBuffer(t *testing.T) {
	const size = 1 << 20
	line := strings.Repeat("x", 63) + "\n"
	want := strings.Repeat(line, size/len(line))

	got := captureStdout(t, func() {
		if _, err := io.WriteString(os.Stdout, want); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	if got != want {
		t.Errorf("captured %d bytes of %d", len(got), len(want))
	}
}
