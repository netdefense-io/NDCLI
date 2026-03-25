package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// VariableConflict represents a variable conflict from sync operations
type VariableConflict struct {
	Variable string `json:"variable"`
	Message  string `json:"message"`
}

// APIError represents an error response from the API
type APIError struct {
	StatusCode         int
	Code               string             // Machine-readable error code (e.g., "NOT_FOUND", "AUTH_REQUIRED")
	Message            string             // Human-readable error message
	Detail             string             // Additional context about the error
	BlockingResources  []string           // For 409 conflicts - list of blocking resource names
	BlockingCount      int                // For 409 conflicts - count of blocking resources
	Conflicts          []VariableConflict // For VARIABLE_CONFLICT errors
	UndefinedVariables []string           // For UNDEFINED_VARIABLES errors
}

func (e *APIError) Error() string {
	// Handle VARIABLE_CONFLICT errors with detailed conflict info
	if e.Code == "VARIABLE_CONFLICT" && len(e.Conflicts) > 0 {
		var lines []string
		lines = append(lines, e.Message)
		for _, c := range e.Conflicts {
			if c.Message != "" {
				lines = append(lines, fmt.Sprintf("  • %s", c.Message))
			} else {
				lines = append(lines, fmt.Sprintf("  • %s", c.Variable))
			}
		}
		return strings.Join(lines, "\n")
	}

	// Handle UNDEFINED_VARIABLES errors with list of undefined vars
	if e.Code == "UNDEFINED_VARIABLES" && len(e.UndefinedVariables) > 0 {
		var lines []string
		lines = append(lines, e.Message)
		for _, v := range e.UndefinedVariables {
			lines = append(lines, fmt.Sprintf("  • ${%s}", v))
		}
		return strings.Join(lines, "\n")
	}

	// Handle plan limit errors with hint
	if (e.Code == "PLAN_LIMIT_EXCEEDED" || e.Code == "FEATURE_NOT_AVAILABLE") && e.Detail != "" {
		return fmt.Sprintf("%s\n  Hint: %s", e.Message, e.Detail)
	}

	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("API error: %d", e.StatusCode)
}

// ParseError creates an APIError from an HTTP response
func ParseError(resp *http.Response) *APIError {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
	}

	// Read body once
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		apiErr.Message = statusMessage(resp.StatusCode)
		return apiErr
	}

	// Try to parse as generic JSON first
	var rawResp map[string]interface{}
	if err := json.Unmarshal(body, &rawResp); err != nil {
		apiErr.Message = statusMessage(resp.StatusCode)
		return apiErr
	}

	// Extract machine-readable error code (per API spec)
	if code, ok := rawResp["code"].(string); ok {
		apiErr.Code = code
	}

	// Check for "detail" field - could be string or array
	if detail, ok := rawResp["detail"]; ok {
		switch v := detail.(type) {
		case string:
			// Simple string detail - store in Detail field
			apiErr.Detail = v
		case []interface{}:
			// Pydantic validation errors array - format as message
			apiErr.Message = formatValidationErrors(v)
		}
	}

	// Extract primary error message (per API spec: "error" is the standard field)
	if errMsg, ok := rawResp["error"].(string); ok && errMsg != "" {
		apiErr.Message = errMsg
	} else if msg, ok := rawResp["message"].(string); ok && msg != "" {
		// Fallback to "message" field
		apiErr.Message = msg
	}

	// If no message but we have detail, use detail as message
	if apiErr.Message == "" && apiErr.Detail != "" {
		apiErr.Message = apiErr.Detail
	}

	// Capture blocking_resources (API spec) or blocking_devices (legacy) for conflict errors
	if resources, ok := rawResp["blocking_resources"].([]interface{}); ok {
		for _, r := range resources {
			if name, ok := r.(string); ok {
				apiErr.BlockingResources = append(apiErr.BlockingResources, name)
			}
		}
	} else if devices, ok := rawResp["blocking_devices"].([]interface{}); ok {
		for _, d := range devices {
			if name, ok := d.(string); ok {
				apiErr.BlockingResources = append(apiErr.BlockingResources, name)
			}
		}
	}

	// Capture count for conflict errors
	if count, ok := rawResp["count"].(float64); ok {
		apiErr.BlockingCount = int(count)
	}

	// Capture variable conflicts for VARIABLE_CONFLICT errors
	if conflicts, ok := rawResp["conflicts"].([]interface{}); ok {
		for _, c := range conflicts {
			if conflictMap, ok := c.(map[string]interface{}); ok {
				vc := VariableConflict{}
				if variable, ok := conflictMap["variable"].(string); ok {
					vc.Variable = variable
				}
				if message, ok := conflictMap["message"].(string); ok {
					vc.Message = message
				}
				if vc.Variable != "" || vc.Message != "" {
					apiErr.Conflicts = append(apiErr.Conflicts, vc)
				}
			}
		}
	}

	// Capture undefined variables for UNDEFINED_VARIABLES errors
	if undefinedVars, ok := rawResp["undefined_variables"].([]interface{}); ok {
		for _, v := range undefinedVars {
			if varName, ok := v.(string); ok {
				apiErr.UndefinedVariables = append(apiErr.UndefinedVariables, varName)
			}
		}
	}

	// If no message from JSON, use status-based message
	if apiErr.Message == "" {
		apiErr.Message = statusMessage(resp.StatusCode)
	}

	return apiErr
}

// formatValidationErrors formats Pydantic validation errors for display
func formatValidationErrors(errors []interface{}) string {
	var messages []string

	for _, e := range errors {
		errMap, ok := e.(map[string]interface{})
		if !ok {
			continue
		}

		msg, _ := errMap["msg"].(string)
		loc, _ := errMap["loc"].([]interface{})

		// Build field path from location
		var fieldParts []string
		for _, l := range loc {
			if s, ok := l.(string); ok {
				fieldParts = append(fieldParts, s)
			}
		}
		field := strings.Join(fieldParts, ".")

		if field != "" && msg != "" {
			messages = append(messages, fmt.Sprintf("  %s: %s", field, msg))
		} else if msg != "" {
			messages = append(messages, fmt.Sprintf("  %s", msg))
		}
	}

	if len(messages) == 0 {
		return "Validation error"
	}

	return "Validation error:\n" + strings.Join(messages, "\n")
}

// statusMessage returns a user-friendly message for common HTTP status codes
func statusMessage(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "Authentication failed. Please run 'ndcli auth login' to authenticate."
	case http.StatusForbidden:
		return "Access denied. You don't have permission to perform this action."
	case http.StatusNotFound:
		return "Resource not found."
	case http.StatusConflict:
		return "Conflict. The resource may already exist or is in an invalid state."
	case http.StatusUnprocessableEntity:
		return "Validation error. Please check your input."
	case http.StatusTooManyRequests:
		return "Too many requests. Please try again later."
	case http.StatusInternalServerError:
		return "Server error. Please try again later."
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "Service temporarily unavailable. Please try again later."
	default:
		return fmt.Sprintf("Request failed with status %d", code)
	}
}

// IsAuthError returns true if the error is an authentication error
func IsAuthError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusUnauthorized
	}
	return false
}

// IsNotFoundError returns true if the error is a not found error
func IsNotFoundError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflictError returns true if the error is a 409 conflict error
func IsConflictError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusConflict
	}
	return false
}

// GetErrorCode returns the machine-readable error code from an API error
func GetErrorCode(err error) string {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.Code
	}
	return ""
}

// IsPlanLimitError returns true if the error is a plan limit or feature not available error
func IsPlanLimitError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusForbidden &&
			(apiErr.Code == "PLAN_LIMIT_EXCEEDED" || apiErr.Code == "FEATURE_NOT_AVAILABLE")
	}
	return false
}

// IsFeatureNotAvailableError returns true if the error is a feature not available error
func IsFeatureNotAvailableError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusForbidden && apiErr.Code == "FEATURE_NOT_AVAILABLE"
	}
	return false
}

// IsRegistrationRestrictedError returns true if the error is a closed-beta registration restriction
func IsRegistrationRestrictedError(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == http.StatusForbidden && apiErr.Code == "REGISTRATION_RESTRICTED"
	}
	return false
}
