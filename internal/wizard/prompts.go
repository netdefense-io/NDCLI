package wizard

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

var reader = bufio.NewReader(os.Stdin)

// readKey reads a single keypress from the terminal
// Returns the key character, or 0 on error
func readKey() byte {
	fd := int(os.Stdin.Fd())

	// Check if stdin is a terminal
	if !term.IsTerminal(fd) {
		// Fall back to buffered read
		b := make([]byte, 1)
		os.Stdin.Read(b)
		return b[0]
	}

	// Save terminal state
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0
	}
	defer term.Restore(fd, oldState)

	// Read single byte
	b := make([]byte, 1)
	_, err = os.Stdin.Read(b)
	if err != nil {
		return 0
	}

	return b[0]
}

// PromptYesNo prompts for yes/no with a default value
// Reads a single keypress: y, n, or Enter for default
func PromptYesNo(prompt string, defaultYes bool) bool {
	var hint string
	if defaultYes {
		hint = "[Y/n]"
	} else {
		hint = "[y/N]"
	}

	fmt.Printf("%s %s: ", prompt, hint)

	for {
		key := readKey()

		switch key {
		case 'y', 'Y':
			fmt.Println("y")
			return true
		case 'n', 'N':
			fmt.Println("n")
			return false
		case '\r', '\n':
			if defaultYes {
				fmt.Println("y")
			} else {
				fmt.Println("n")
			}
			return defaultYes
		case 3: // Ctrl+C
			fmt.Println()
			os.Exit(0)
		}
		// Ignore other keys, keep waiting
	}
}

// PromptChoice presents a numbered list of options and returns the selected index
// Reads a single keypress: 1-9 for selection, Enter for default
func PromptChoice(prompt string, options []string, defaultIdx int) int {
	if prompt != "" {
		fmt.Println(prompt)
	}
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = color.CyanString("> ")
		}
		fmt.Printf("%s[%d] %s\n", marker, i+1, opt)
	}
	fmt.Println()

	if defaultIdx >= 0 && defaultIdx < len(options) {
		fmt.Printf("Enter choice [1-%d] (Enter = %d): ", len(options), defaultIdx+1)
	} else {
		fmt.Printf("Enter choice [1-%d]: ", len(options))
	}

	for {
		key := readKey()

		// Handle number keys 1-9
		if key >= '1' && key <= '9' {
			choice := int(key - '0')
			if choice >= 1 && choice <= len(options) {
				fmt.Printf("%d\n", choice)
				return choice - 1
			}
		}

		// Handle Enter for default
		if key == '\r' || key == '\n' {
			if defaultIdx >= 0 && defaultIdx < len(options) {
				fmt.Printf("%d\n", defaultIdx+1)
				return defaultIdx
			}
			return -1
		}

		// Handle Ctrl+C
		if key == 3 {
			fmt.Println()
			os.Exit(0)
		}

		// Ignore other keys
	}
}

// PromptText prompts for text input with optional validation
func PromptText(prompt string, required bool) string {
	for {
		fmt.Printf("%s: ", prompt)

		response, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}

		response = strings.TrimSpace(response)
		if response == "" && required {
			color.Yellow("This field is required")
			continue
		}

		return response
	}
}

// PromptTextDefault prompts for text input with a default value
func PromptTextDefault(prompt, defaultValue string) string {
	fmt.Printf("%s [%s]: ", prompt, defaultValue)

	response, err := reader.ReadString('\n')
	if err != nil {
		return defaultValue
	}

	response = strings.TrimSpace(response)
	if response == "" {
		return defaultValue
	}

	return response
}

// ShowStepHeader displays a step header with number
func ShowStepHeader(stepNum int, title string) {
	const totalWidth = 60
	prefix := fmt.Sprintf("━━━ Step %d: %s ", stepNum, title)
	// Calculate remaining width for trailing dashes
	remaining := totalWidth - len(prefix)
	if remaining < 3 {
		remaining = 3
	}
	fmt.Println()
	color.New(color.FgCyan, color.Bold).Printf("%s%s", prefix, strings.Repeat("━", remaining))
	fmt.Println()
	fmt.Println()
}

// ShowSuccess displays a success message
func ShowSuccess(msg string) {
	color.Green("✓ %s", msg)
}

// ShowInfo displays an info message
func ShowInfo(msg string) {
	color.Cyan("ℹ %s", msg)
}

// ShowWarning displays a warning message
func ShowWarning(msg string) {
	color.Yellow("⚠ %s", msg)
}

// ShowError displays an error message
func ShowError(msg string) {
	color.Red("✗ %s", msg)
}

// ShowBanner displays the setup wizard banner
func ShowBanner() {
	banner := `
▙ ▌   ▐  ▛▀▖   ▗▀▖             ▞▀▖▌  ▜▘
▌▌▌▞▀▖▜▀ ▌ ▌▞▀▖▐  ▞▀▖▛▀▖▞▀▘▞▀▖ ▌  ▌  ▐
▌▝▌▛▀ ▐ ▖▌ ▌▛▀ ▜▀ ▛▀ ▌ ▌▝▀▖▛▀  ▌ ▖▌  ▐
▘ ▘▝▀▘ ▀ ▀▀ ▝▀▘▐  ▝▀▘▘ ▘▀▀ ▝▀▘ ▝▀ ▀▀▘▀▘
`
	color.Cyan(banner)
	color.New(color.FgCyan, color.Bold).Println("                      SETUP GUIDE")
	fmt.Println()
	fmt.Println("This wizard will help you configure NDCLI.")
	fmt.Println("Press Ctrl+C to exit at any time.")
	fmt.Println()
}

// ShowFinalInstructions displays the final instructions after setup
func ShowFinalInstructions(registrationToken string, defaultOrg string) {
	fmt.Println()
	color.New(color.FgGreen, color.Bold).Println("╔══════════════════════════════════════════════════════════════╗")
	color.New(color.FgGreen, color.Bold).Println("║                     Setup Complete!                          ║")
	color.New(color.FgGreen, color.Bold).Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	color.New(color.Bold).Println("To add a firewall to NetDefense:")
	fmt.Println()
	fmt.Println("  1. Connect to OPNsense terminal (console or SSH) and run:")
	color.Cyan("     curl -sSL https://repo.netdefense.io/install.sh | sh")
	fmt.Println()
	fmt.Println("  2. In OPNsense, go to:")
	color.Cyan("     Services > NetDefense > Settings")
	fmt.Println()

	if registrationToken != "" && defaultOrg != "" {
		fmt.Printf("  3. Enter Registration Token (to link device to '%s'):\n", defaultOrg)
		color.New(color.FgYellow, color.Bold).Printf("     %s\n", registrationToken)
		fmt.Println()
		fmt.Println("  4. Enable the service and save.")
	} else if registrationToken != "" {
		fmt.Println("  3. Enter Registration Token:")
		color.New(color.FgYellow, color.Bold).Printf("     %s\n", registrationToken)
		fmt.Println()
		fmt.Println("  4. Enable the service and save.")
	} else if defaultOrg != "" {
		fmt.Println("  3. Get your registration token with:")
		color.Cyan("     ndcli org describe %s", defaultOrg)
		fmt.Println()
		fmt.Println("  4. Enter the token, enable the service and save.")
	} else {
		fmt.Println("  3. Create an organization first:")
		color.Cyan("     ndcli org create <name>")
		fmt.Println()
		fmt.Println("  4. Then get your registration token and configure the agent.")
	}

	fmt.Println()
	color.New(color.Bold).Println("Next steps:")
	fmt.Println("  ndcli device list              # View registered devices")
	fmt.Println("  ndcli device approve <name>    # Approve pending devices")
	fmt.Println("  ndcli help                     # See all commands")
	fmt.Println()
}
