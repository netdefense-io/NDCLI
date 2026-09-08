package helpers

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// MaxMessageBytes is the server's per-message body cap (64 KiB, measured in
// bytes rather than characters). Checked client-side so a too-long paste
// fails before the request rather than as a 422.
const MaxMessageBytes = 64 * 1024

// ReadMessage resolves a message body from the two mutually exclusive
// sources every message-taking ticket command accepts: --message TEXT and
// --message-file PATH, where PATH of "-" reads stdin to EOF.
//
// Exactly one source may be given. When required is true, omitting both is
// an error; when it is false (close/reopen, where the message is optional)
// the result is the empty string.
func ReadMessage(message, messageFile string, required bool) (string, error) {
	switch {
	case message != "" && messageFile != "":
		return "", fmt.Errorf("--message and --message-file are mutually exclusive")
	case message != "":
		if err := checkMessageSize(len(message)); err != nil {
			return "", err
		}
		return message, nil
	case messageFile != "":
		var (
			data []byte
			err  error
		)
		if messageFile == "-" {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, MaxMessageBytes+1))
			if err != nil {
				return "", fmt.Errorf("failed to read message from stdin: %w", err)
			}
		} else {
			// Bounded like the stdin branch: a mistyped path pointing at a
			// multi-gigabyte file must not be pulled into memory just to be
			// rejected by the size check below.
			f, openErr := os.Open(messageFile)
			if openErr != nil {
				return "", fmt.Errorf("failed to read message file: %w", openErr)
			}
			data, err = io.ReadAll(io.LimitReader(f, MaxMessageBytes+1))
			f.Close()
			if err != nil {
				return "", fmt.Errorf("failed to read message file: %w", err)
			}
		}
		// Size is checked on the raw read, before the trailing-newline
		// trim: if the byte at the cutoff happens to be \r or \n, trimming
		// first would bring the length back under the cap and submit a
		// silently truncated message instead of rejecting it.
		if err := checkMessageSize(len(data)); err != nil {
			return "", err
		}
		body := strings.TrimRight(string(data), "\r\n")
		if body == "" {
			return "", fmt.Errorf("message is empty")
		}
		return body, nil
	case required:
		return "", fmt.Errorf("a message is required: pass --message TEXT, --message-file PATH, or --message-file - to read stdin")
	default:
		return "", nil
	}
}

// checkMessageSize rejects a body over the cap. It owns the comparison
// rather than trusting callers to pre-filter, so a new call site cannot
// accidentally opt out of the limit. Reads are bounded at the cap plus one
// byte, so "at least" is the honest phrasing for a file or stream whose
// true size was never measured.
func checkMessageSize(n int) error {
	if n <= MaxMessageBytes {
		return nil
	}
	return fmt.Errorf("message is at least %d bytes; the limit is %d bytes (64 KiB)", n, MaxMessageBytes)
}
