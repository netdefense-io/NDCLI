package models

// Snippet represents a configuration snippet
type Snippet struct {
	Name         string       `json:"name"`
	Type         string       `json:"type,omitempty"` // RULE, ALIAS, USER, GROUP, UNBOUND_HOST_OVERRIDE, UNBOUND_DOMAIN_FORWARD, UNBOUND_HOST_ALIAS, UNBOUND_ACL
	Content      string       `json:"content,omitempty"`
	Priority     int          `json:"priority"`
	Organization string       `json:"organization_name,omitempty"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
}

// SnippetListResponse represents a paginated list of snippets
type SnippetListResponse struct {
	Items      []Snippet `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`
}

