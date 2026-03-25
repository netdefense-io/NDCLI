package models

// Variable represents a configuration variable at any scope
type Variable struct {
	Name        string        `json:"name"`
	Value       string        `json:"value"`
	Description string        `json:"description,omitempty"`
	Scope       string        `json:"scope"`      // ORG, OU, TEMPLATE, DEVICE
	ScopeName   *string       `json:"scope_name"` // null for ORG scope
	Secret      bool          `json:"secret"`     // Whether this variable is marked as secret
	CreatedAt   FlexibleTime  `json:"created_at"`
	UpdatedAt   *FlexibleTime `json:"updated_at,omitempty"`
}

// VariableListResponse represents a paginated list of variables
type VariableListResponse struct {
	Items   []Variable `json:"items"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}

// GetItems returns variables from the items field
func (r *VariableListResponse) GetItems() []Variable {
	return r.Items
}

// Variable scope constants
const (
	VariableScopeOrg      = "ORG"
	VariableScopeOU       = "OU"
	VariableScopeTemplate = "TEMPLATE"
	VariableScopeDevice   = "DEVICE"
)

// VariableDefinition represents a single definition of a variable at a specific scope
type VariableDefinition struct {
	Scope       string  `json:"scope"`       // organization, ou, template, device
	ScopeName   *string `json:"scope_name"`  // null for org scope
	Value       string  `json:"value"`
	Description string  `json:"description,omitempty"`
	Secret      bool    `json:"secret"` // Whether this variable is marked as secret
}

// VariableOverview represents a variable with all its definitions across scopes
type VariableOverview struct {
	Name        string               `json:"name"`
	Definitions []VariableDefinition `json:"definitions"`
}

// VariableOverviewResponse represents the paginated overview response
type VariableOverviewResponse struct {
	Items   []VariableOverview `json:"items"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	PerPage int                `json:"per_page"`
	Filters map[string]string  `json:"filters,omitempty"`
}
