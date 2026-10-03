package output

// WithCode puts NDManager's error code in front of message, in the brackets
// auth and trust issue lines already use. An empty code leaves message as it
// is.
func WithCode(code, message string) string {
	if code == "" {
		return message
	}
	return "[" + code + "] " + message
}
