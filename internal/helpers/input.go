package helpers

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// ConfirmOrForce is the standard destructive-action gate for commands that
// accept a --force / --yes flag.
//
//   - force == true  → proceed immediately (scripting / CI path).
//   - stdin is a TTY → show the prompt from ConfirmAnswer; proceed only on y/yes.
//   - stdin is NOT a TTY and force is false → return an error asking for
//     --force so the command never hangs waiting on piped input.
func ConfirmOrForce(message string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	if !StdinIsTerminal() {
		return false, fmt.Errorf("stdin is not a terminal — pass --force to confirm non-interactively")
	}
	yes, _ := ConfirmAnswer(message)
	return yes, nil
}

// ConfirmAnswer is AskYesNo with no for its default: [y/N].
func ConfirmAnswer(message string) (yes, answered bool) {
	return AskYesNo(message, false)
}

// AskYesNo asks a yes/no question on stderr, so stdout carries only the
// command's output, and reads the answer from stdin. An empty line is an
// answer, the default: yes when defaultYes ([Y/n]), else no ([y/N]).
// answered is false, and yes with it, when stdin ended before a line was
// typed: an empty pipe or file, or Ctrl-D at a terminal.
func AskYesNo(message string, defaultYes bool) (yes, answered bool) {
	choices := "[y/N]"
	if defaultYes {
		choices = "[Y/n]"
	}
	_, _ = color.New(color.FgYellow).Fprintf(os.Stderr, "%s %s: ", message, choices)
	response, answered := ReadAnswer()
	if !answered {
		return false, false
	}
	switch strings.ToLower(response) {
	case "y", "yes":
		return true, true
	case "":
		return defaultYes, true
	}
	return false, true
}

// maxAnswerBytes bounds what ReadAnswer keeps of a line; the rest of the line
// is read and dropped.
const maxAnswerBytes = 1024

// ReadAnswer reads one line of stdin, trimmed. answered is false when stdin
// ended before anything was read. It reads a byte at a time, so a command
// that asks twice finds its second answer on the next line of a pipe: a
// buffered reader would have taken it with the first.
func ReadAnswer() (answer string, answered bool) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(b)
		if n == 0 && err == nil {
			break
		}
		if n == 1 {
			answered = true
			if b[0] == '\n' {
				break
			}
			if len(line) < maxAnswerBytes {
				line = append(line, b[0])
			}
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(string(line)), answered
}

// StdinIsTerminal reports whether stdin is a terminal.
func StdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ValidateInput validates input length
func ValidateInput(value, name string, minLen, maxLen int) error {
	if len(value) < minLen {
		return fmt.Errorf("%s must be at least %d characters", name, minLen)
	}
	if maxLen > 0 && len(value) > maxLen {
		return fmt.Errorf("%s must be at most %d characters", name, maxLen)
	}
	return nil
}
