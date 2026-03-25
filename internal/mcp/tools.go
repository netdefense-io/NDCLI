package mcp

import (
	"encoding/json"
	"time"
)

const (
	apiTimeout = 30 * time.Second
)

// Tool input types

// DeviceListInput is the input for ndcli.device.list
type DeviceListInput struct {
	Organization    string `json:"organization,omitempty"`
	Status          string `json:"status,omitempty"`
	OU              string `json:"ou,omitempty"`
	Name            string `json:"name,omitempty"`
	SortBy          string `json:"sort_by,omitempty"`
	Page            int    `json:"page,omitempty"`
	PerPage         int    `json:"per_page,omitempty"`
	HeartbeatAfter  string `json:"heartbeat_after,omitempty"`
	HeartbeatBefore string `json:"heartbeat_before,omitempty"`
}

// DeviceInput is the input for single device operations
type DeviceInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// DeviceRenameInput is the input for ndcli.device.rename
type DeviceRenameInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
	NewName      string `json:"new_name"`
	Confirm      bool   `json:"confirm,omitempty"`
}

// OrgInput is the input for organization operations
type OrgInput struct {
	Organization string `json:"organization,omitempty"`
}

// OrgListInput is the input for ndcli.org.list
type OrgListInput struct {
	Page    int `json:"page,omitempty"`
	PerPage int `json:"per_page,omitempty"`
}

// OUListInput is the input for ndcli.ou.list
type OUListInput struct {
	Organization string `json:"organization,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

// OUInput is the input for single OU operations
type OUInput struct {
	Organization string `json:"organization,omitempty"`
	OU           string `json:"ou"`
}

// SyncStatusInput is the input for ndcli.sync.status
type SyncStatusInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device,omitempty"`
}

// SyncApplyInput is the input for ndcli.sync.apply
type SyncApplyInput struct {
	Organization string `json:"organization,omitempty"`
	Device       string `json:"device"`
}

// TaskListInput is the input for ndcli.task.list
type TaskListInput struct {
	Organization string `json:"organization,omitempty"`
	Status       string `json:"status,omitempty"`
	Page         int    `json:"page,omitempty"`
	PerPage      int    `json:"per_page,omitempty"`
}

// parseInput parses the tool arguments into the given struct
func parseInput[T any](args json.RawMessage) (*T, error) {
	var input T
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, &ToolError{
				Code:    "INVALID_INPUT",
				Message: "Failed to parse input: " + err.Error(),
			}
		}
	}
	return &input, nil
}

// marshalJSON marshals a value to indented JSON
func marshalJSON(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Tool schema helpers

// stringProperty creates a string property for tool schemas
func stringProperty(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
	}
}

// stringEnumProperty creates a string enum property for tool schemas
func stringEnumProperty(description string, values []string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}

// intProperty creates an integer property for tool schemas
func intProperty(description string, defaultVal int) map[string]interface{} {
	return map[string]interface{}{
		"type":        "integer",
		"description": description,
		"default":     defaultVal,
	}
}

// boolProperty creates a boolean property for tool schemas
func boolProperty(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": description,
	}
}

// organizationProperty creates the standard organization property
func organizationProperty() map[string]interface{} {
	return stringProperty("Organization name (uses default from config if not specified)")
}

// confirmProperty creates the standard confirm property for destructive operations
func confirmProperty() map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": "Set to true to execute the destructive operation. Without this, returns a preview.",
		"default":     false,
	}
}
