package models

// Template represents a configuration template
type Template struct {
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Position     string       `json:"position"` // PREPEND or APPEND
	SnippetCount int          `json:"snippet_count"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at"`
	CreatedBy    string       `json:"created_by,omitempty"`
	// Snippets is populated by the describe endpoint
	Snippets []Snippet `json:"snippets,omitempty"`
}

// TemplateListResponse represents a paginated list of templates
type TemplateListResponse struct {
	Items      []Template `json:"items"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PerPage    int        `json:"per_page"`
	TotalPages int        `json:"total_pages"`
}

// TemplateDetail represents a template with its snippets
type TemplateDetail struct {
	Template
	Snippets []Snippet `json:"snippets,omitempty"`
}
