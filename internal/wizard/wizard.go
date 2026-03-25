package wizard

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/netdefense-io/NDCLI/internal/api"
	"github.com/netdefense-io/NDCLI/internal/auth"
	"github.com/netdefense-io/NDCLI/internal/config"
	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/storage"
)

// Wizard orchestrates the setup flow
type Wizard struct {
	authManager       *auth.Manager
	apiClient         *api.Client
	authenticated     bool
	defaultOrg        string
	registrationToken string
}

// New creates a new setup wizard instance
func New() *Wizard {
	return &Wizard{}
}

// Run executes all wizard steps in sequence
func (w *Wizard) Run(ctx context.Context) error {
	// Cleanup auth manager when wizard completes
	defer func() {
		if w.authManager != nil {
			w.authManager.Close()
		}
	}()

	// Step 1: Banner
	ShowBanner()

	// Step 2: Auth Setup
	ShowStepHeader(1, "Authentication")
	w.runAuthStep(ctx)

	// Step 3: Organization Setup (requires auth)
	if w.authenticated {
		ShowStepHeader(2, "Organization")
		w.runOrgStep(ctx)
	} else {
		fmt.Println()
		ShowWarning("Skipping organization setup (authentication required)")
	}

	// Step 3: Terminal Setup
	ShowStepHeader(3, "Terminal Preferences")
	w.runTerminalStep()

	// Step 4: Shell Completion
	ShowStepHeader(4, "Shell Completion")
	w.showCompletionInfo()

	// Step 5: Final Instructions
	ShowFinalInstructions(w.registrationToken, w.defaultOrg)

	return nil
}

// runAuthStep handles authentication setup
func (w *Wizard) runAuthStep(ctx context.Context) {
	configPath := config.GetDefaultConfigPath()

	// Check if config exists
	if !config.ConfigExists() {
		ShowInfo("Creating config file at:")
		fmt.Printf("  %s\n", configPath)
		if _, err := config.EnsureConfigDir(); err != nil {
			ShowError(fmt.Sprintf("Failed to create config directory: %v", err))
			return
		}
		if err := config.CreateDefaultConfig(); err != nil {
			ShowError(fmt.Sprintf("Failed to create config file: %v", err))
			return
		}
		fmt.Println()
		ShowSuccess("Config file created")
		fmt.Println()
	} else {
		ShowInfo(fmt.Sprintf("Config file: %s", configPath))
		fmt.Println()
	}

	// Load config if not already loaded
	if err := config.Load(""); err != nil {
		ShowError(fmt.Sprintf("Failed to load config: %v", err))
		return
	}

	// Check storage preference
	cfg := config.Get()
	if cfg.Auth.Storage == "" {
		w.promptStorageChoice()
	}

	// Initialize auth manager with fresh instance (after storage config is set)
	// Don't use GetManager() singleton as it may have stale config
	w.authManager = auth.NewManager()

	// Check if already authenticated
	if w.authManager.IsAuthenticated() {
		userInfo, err := w.authManager.GetUserInfo()
		if err == nil && userInfo != nil {
			ShowSuccess(fmt.Sprintf("Already authenticated as: %s", userInfo.Email))
			fmt.Println()

			if !PromptYesNo("Change account?", false) {
				w.authenticated = true
				w.initAPIClient()
				return
			}
		}
	}

	// Prompt for login
	if !PromptYesNo("Create or authenticate your NetDefense account?", true) {
		ShowWarning("Authentication skipped")
		return
	}

	// Perform login
	fmt.Println()
	_, err := w.authManager.Login(ctx, "", true)
	if err != nil {
		ShowError(fmt.Sprintf("Login failed: %v", err))
		return
	}

	// Show success
	userInfo, _ := w.authManager.GetUserInfo()
	if userInfo != nil {
		fmt.Println()
		ShowSuccess("Successfully authenticated!")
		fmt.Printf("  Name: %s\n", userInfo.Name)
		fmt.Printf("  Email: %s\n", userInfo.Email)
	}

	w.authenticated = true
	w.initAPIClient()

	// Record login
	w.recordLogin(ctx, userInfo)
}

// promptStorageChoice asks user where to store auth tokens
func (w *Wizard) promptStorageChoice() {
	fmt.Println("Where would you like to store authentication credentials?")
	fmt.Println()

	options := []string{
		"System Keyring (recommended - more secure)",
		"Config file (less secure, but portable)",
	}

	// Check keyring availability
	keyringAvailable := storage.IsKeyringAvailable()
	if !keyringAvailable {
		ShowWarning("System keyring is not available on this system")
		options[0] = "System Keyring (not available)"
	}

	defaultIdx := 0
	if !keyringAvailable {
		defaultIdx = 1
	}

	choice := PromptChoice("Select storage method:", options, defaultIdx)
	fmt.Println()

	var storageType string
	if choice == 0 && keyringAvailable {
		storageType = "keyring"
		ShowSuccess("Using system keyring for credentials")
	} else {
		storageType = "file"
		authPath := config.GetAuthFilePath()
		ShowInfo("Credentials will be stored in:")
		fmt.Printf("  %s\n", authPath)
	}

	config.UpdateValue("auth.storage", storageType)
	fmt.Println()
}

// initAPIClient initializes the API client
func (w *Wizard) initAPIClient() {
	w.apiClient = api.NewClientFromConfig(w.authManager)
}

// recordLogin records login with API and shows pending invites
func (w *Wizard) recordLogin(ctx context.Context, userInfo *models.UserInfo) {
	if w.apiClient == nil {
		return
	}

	payload := map[string]interface{}{}
	if userInfo != nil && userInfo.Name != "" {
		payload["name"] = userInfo.Name
	}

	resp, err := w.apiClient.Post(ctx, "/api/v1/auth/me", payload)
	if err != nil {
		return
	}

	var result models.AuthMeUpdateResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return
	}

	// Store pending invites for org step
	if len(result.PendingInvites) > 0 {
		fmt.Println()
		color.Yellow("You have %d pending organization invite(s):", len(result.PendingInvites))
		for _, inv := range result.PendingInvites {
			fmt.Printf("  - %s [%s] from %s\n", inv.Organization, inv.Role, inv.InvitedBy)
		}
	}
}

// runOrgStep handles organization setup
func (w *Wizard) runOrgStep(ctx context.Context) {
	// Get user's organizations
	resp, err := w.apiClient.Get(ctx, "/api/v1/auth/me", nil)
	if err != nil {
		ShowError(fmt.Sprintf("Failed to get user info: %v", err))
		return
	}

	var authMe models.AuthMe
	if err := api.ParseResponse(resp, &authMe); err != nil {
		ShowError(fmt.Sprintf("Failed to parse user info: %v", err))
		return
	}

	// Handle pending invites
	w.handlePendingInvites(ctx, &authMe)

	// Check current organizations
	orgs := authMe.Organizations
	cfg := config.Get()

	if len(orgs) == 0 {
		// No organizations - offer to create one
		fmt.Println("You don't have any organizations yet.")
		fmt.Println()

		if PromptYesNo("Create your first organization?", true) {
			w.createOrganization(ctx)
		} else {
			ShowWarning("Organization setup skipped")
			fmt.Println("You can create an organization later with: ndcli org create <name>")
		}
		return
	}

	// Has organizations - check default
	if cfg.Organization.Name != "" {
		// Verify the org still exists in user's list
		found := false
		for _, org := range orgs {
			if org.Name == cfg.Organization.Name {
				found = true
				break
			}
		}
		if found {
			ShowSuccess(fmt.Sprintf("Default organization: %s", cfg.Organization.Name))
			w.defaultOrg = cfg.Organization.Name
			w.fetchRegistrationToken(ctx)
			fmt.Println()

			if !PromptYesNo("Change default organization?", false) {
				return
			}
			fmt.Println()
		} else {
			ShowWarning(fmt.Sprintf("Default organization '%s' no longer accessible", cfg.Organization.Name))
		}
	}

	// Prompt to set default org
	w.promptSetDefaultOrg(orgs)
	if w.defaultOrg != "" {
		w.fetchRegistrationToken(ctx)
	}
}

// handlePendingInvites processes pending organization invites
func (w *Wizard) handlePendingInvites(ctx context.Context, authMe *models.AuthMe) {
	// Get pending invites
	resp, err := w.apiClient.Post(ctx, "/api/v1/auth/me", nil)
	if err != nil {
		return
	}

	var result models.AuthMeUpdateResponse
	if err := api.ParseResponse(resp, &result); err != nil {
		return
	}

	if len(result.PendingInvites) == 0 {
		return
	}

	fmt.Println("You have pending organization invitations:")
	fmt.Println()

	for _, inv := range result.PendingInvites {
		fmt.Printf("  Organization: %s\n", inv.Organization)
		fmt.Printf("  Role: %s\n", inv.Role)
		fmt.Printf("  Invited by: %s\n", inv.InvitedBy)
		fmt.Println()

		if PromptYesNo(fmt.Sprintf("Accept invitation to '%s'?", inv.Organization), true) {
			_, err := w.apiClient.Put(ctx, fmt.Sprintf("/api/v1/organizations/%s/invites/accept", inv.Organization), nil)
			if err != nil {
				ShowError(fmt.Sprintf("Failed to accept invitation: %v", err))
			} else {
				ShowSuccess(fmt.Sprintf("Joined organization: %s", inv.Organization))
			}
		}
		fmt.Println()
	}
}

// createOrganization creates a new organization
func (w *Wizard) createOrganization(ctx context.Context) {
	fmt.Println()
	name := PromptText("Organization name", true)
	if name == "" {
		ShowWarning("Organization creation cancelled")
		return
	}

	payload := map[string]string{"name": name}
	resp, err := w.apiClient.Post(ctx, "/api/v1/organizations", payload)
	if err != nil {
		ShowError(fmt.Sprintf("Failed to create organization: %v", err))
		return
	}

	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		ShowError(fmt.Sprintf("Failed to parse response: %v", err))
		return
	}

	fmt.Println()
	ShowSuccess(fmt.Sprintf("Organization created: %s", name))
	w.registrationToken = org.Token
	w.defaultOrg = name

	// Set as default
	config.UpdateValue("organization.name", name)
	ShowInfo(fmt.Sprintf("Set as default organization"))
}

// promptSetDefaultOrg prompts user to select a default organization
func (w *Wizard) promptSetDefaultOrg(orgs []models.AuthMeOrganization) {
	if len(orgs) == 0 {
		return
	}

	fmt.Println("Select your default organization:")
	fmt.Println()

	options := make([]string, len(orgs)+1)
	for i, org := range orgs {
		options[i] = fmt.Sprintf("%s [%s]", org.Name, org.Role)
	}
	options[len(orgs)] = "Skip - don't set a default"

	choice := PromptChoice("", options, 0)
	fmt.Println()
	if choice < 0 || choice >= len(orgs) {
		ShowInfo("No default organization set")
		fmt.Println("  You can set one later with: ndcli config set org <name>")
		return
	}

	selectedOrg := orgs[choice].Name
	config.UpdateValue("organization.name", selectedOrg)
	w.defaultOrg = selectedOrg
	ShowSuccess(fmt.Sprintf("Default organization set to: %s", selectedOrg))
}

// fetchRegistrationToken retrieves the registration token for the default org
func (w *Wizard) fetchRegistrationToken(ctx context.Context) {
	if w.defaultOrg == "" {
		return
	}

	resp, err := w.apiClient.Get(ctx, fmt.Sprintf("/api/v1/organizations/%s", w.defaultOrg), nil)
	if err != nil {
		return
	}

	var org models.Organization
	if err := api.ParseResponse(resp, &org); err != nil {
		return
	}

	w.registrationToken = org.Token
}

// runTerminalStep handles terminal preferences setup
func (w *Wizard) runTerminalStep() {
	cfg := config.Get()

	// Output format
	if cfg.Output.Format == "" || cfg.Output.Format == config.DefaultOutputFormat {
		fmt.Println("Choose your preferred output format:")
		options := []string{
			"table    - Traditional table layout",
			"simple   - Compact bullet points",
			"detailed - Rich formatted output with box drawing",
			"json     - Machine-readable JSON",
		}
		formats := []string{"table", "simple", "detailed", "json"}

		choice := PromptChoice("", options, 2) // default to detailed
		fmt.Println()
		if choice >= 0 && choice < len(formats) {
			config.UpdateValue("output.format", formats[choice])
			ShowSuccess(fmt.Sprintf("Output format set to: %s", formats[choice]))
		}
	} else {
		ShowInfo(fmt.Sprintf("Output format: %s", cfg.Output.Format))
		fmt.Println()
		if PromptYesNo("Change output format?", false) {
			fmt.Println()
			options := []string{
				"table    - Traditional table layout",
				"simple   - Compact bullet points",
				"detailed - Rich formatted output with box drawing",
				"json     - Machine-readable JSON",
			}
			formats := []string{"table", "simple", "detailed", "json"}

			choice := PromptChoice("Choose format:", options, 2)
			fmt.Println()
			if choice >= 0 && choice < len(formats) {
				config.UpdateValue("output.format", formats[choice])
				ShowSuccess(fmt.Sprintf("Output format changed to: %s", formats[choice]))
			}
		}
	}

}

// showCompletionInfo displays shell completion information
func (w *Wizard) showCompletionInfo() {
	shell := os.Getenv("SHELL")
	shellName := "your shell"
	if strings.Contains(shell, "zsh") {
		shellName = "zsh"
	} else if strings.Contains(shell, "bash") {
		shellName = "bash"
	} else if strings.Contains(shell, "fish") {
		shellName = "fish"
	}

	fmt.Printf("Detected shell: %s\n", shellName)
	fmt.Println()
	fmt.Println("NDCLI supports tab completion for commands, flags, and dynamic")
	fmt.Println("values like organization and device names.")
	fmt.Println()

	if strings.Contains(shell, "zsh") {
		ShowInfo("To enable zsh completion, run once:")
		fmt.Println()
		// Check if we're on macOS (Darwin) or Linux
		if strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin") || isMacOS() {
			fmt.Println("  # macOS (with Homebrew):")
			fmt.Println("  ndcli completion zsh > $(brew --prefix)/share/zsh/site-functions/_ndcli")
		} else {
			fmt.Println("  # Linux:")
			fmt.Println("  ndcli completion zsh > \"${fpath[1]}/_ndcli\"")
		}
		fmt.Println()
		fmt.Println("  Then restart your shell or run: source ~/.zshrc")
	} else if strings.Contains(shell, "bash") {
		ShowInfo("To enable bash completion, add to ~/.bashrc:")
		fmt.Println()
		fmt.Println("  source <(ndcli completion bash)")
		fmt.Println()
		fmt.Println("  Then restart your shell or run: source ~/.bashrc")
	} else if strings.Contains(shell, "fish") {
		ShowInfo("To enable fish completion, run once:")
		fmt.Println()
		fmt.Println("  ndcli completion fish > ~/.config/fish/completions/ndcli.fish")
	} else {
		fmt.Println("  Run 'ndcli completion --help' for setup instructions.")
	}
	fmt.Println()

	// Pause to let user read completion instructions
	fmt.Print("Press Enter to continue...")
	reader.ReadString('\n')
}

// isMacOS checks if running on macOS
func isMacOS() bool {
	// Check uname if OSTYPE isn't set
	return strings.Contains(strings.ToLower(os.Getenv("HOME")), "/users/")
}
