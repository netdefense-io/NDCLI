package helpers

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

// Confirm prompts the user for a yes/no confirmation
func Confirm(message string) bool {
	reader := bufio.NewReader(os.Stdin)

	color.New(color.FgYellow).Printf("%s [y/N]: ", message)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// Prompt asks the user for input
func Prompt(message string) string {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("%s: ", message)

	response, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}

	return strings.TrimSpace(response)
}

// PromptDefault asks the user for input with a default value
func PromptDefault(message, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("%s [%s]: ", message, defaultValue)

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
