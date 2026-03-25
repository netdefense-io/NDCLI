package models

// Organization represents a NetDefense organization
type Organization struct {
	ID          FlexibleID   `json:"id"`
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name,omitempty"`
	Description string       `json:"description,omitempty"`
	Status      string       `json:"status,omitempty"`
	Plan        *string      `json:"plan,omitempty"`
	CreatedAt   FlexibleTime `json:"created_at"`
	UpdatedAt   FlexibleTime `json:"updated_at"`

	// Fields from list endpoint
	Role      string `json:"role,omitempty"`
	DefaultOU string `json:"default_ou,omitempty"`

	// Fields from describe endpoint
	UserRole             string         `json:"user_role,omitempty"`
	DefaultOUName        string         `json:"default_ou_name,omitempty"`
	Owners               []string       `json:"owners,omitempty"`
	Token                string         `json:"token,omitempty"`
	DeviceCount          int            `json:"device_count,omitempty"`
	MemberCount          int            `json:"member_count,omitempty"`
	MemberCountsByRole   map[string]int `json:"member_counts_by_role,omitempty"`
	MemberCountsByStatus map[string]int `json:"member_counts_by_status,omitempty"`
}

// GetPlan returns the plan name, defaulting to "free" when not set
func (o *Organization) GetPlan() string {
	if o.Plan != nil && *o.Plan != "" {
		return *o.Plan
	}
	return "free"
}

// OrgPlan represents plan info in a quota response
type OrgPlan struct {
	Name                string `json:"name"`
	DisplayName         string `json:"display_name"`
	PricePerDeviceCents int    `json:"price_per_device_cents"`
}

// OrgQuota represents the quota response for GET /api/v1/organizations/{org}/quota
type OrgQuota struct {
	Organization       string   `json:"organization"`
	Plan               *OrgPlan `json:"plan"`
	Devices            Quota    `json:"devices"`
	Users              Quota    `json:"users"`
	VpnNetworks        Quota    `json:"vpn_networks"`
	Snippets           Quota    `json:"snippets"`
	BackupEnabled      bool     `json:"backup_enabled"`
	RemoteAdminEnabled bool     `json:"remoteadmin_enabled"`
}

// GetRole returns the role from either field (list uses 'role', describe uses 'user_role')
func (o *Organization) GetRole() string {
	if o.UserRole != "" {
		return o.UserRole
	}
	return o.Role
}

// GetDefaultOU returns the default OU from either field (list uses 'default_ou', describe uses 'default_ou_name')
func (o *Organization) GetDefaultOU() string {
	if o.DefaultOU != "" {
		return o.DefaultOU
	}
	return o.DefaultOUName
}

// OrganizationListResponse represents a paginated list of organizations
// API may return items as "items" or "organizations" key
type OrganizationListResponse struct {
	Items         []Organization `json:"items"`
	Organizations []Organization `json:"organizations"`
	Total         int            `json:"total"`
	Count         int            `json:"count"` // API uses 'count' instead of 'total'
	Page          int            `json:"page"`
	PerPage       int            `json:"per_page"`
	TotalPages    int            `json:"total_pages"`
}

// GetItems returns the organizations from whichever key was populated
func (r *OrganizationListResponse) GetItems() []Organization {
	if len(r.Items) > 0 {
		return r.Items
	}
	return r.Organizations
}

// GetTotal returns the total count from whichever field was populated
func (r *OrganizationListResponse) GetTotal() int {
	if r.Total > 0 {
		return r.Total
	}
	return r.Count
}

// OUDevice represents a device in an OU (from describe endpoint)
type OUDevice struct {
	Name string `json:"name"`
}

// OUTemplate represents a template in an OU (from describe endpoint)
type OUTemplate struct {
	Name         string `json:"name"`
	SnippetCount int    `json:"snippet_count"`
}

// OrganizationalUnit represents an OU within an organization
type OrganizationalUnit struct {
	Name          string       `json:"name"`
	DisplayName   string       `json:"display_name,omitempty"`
	Description   string       `json:"description,omitempty"`
	Organization  string       `json:"organization"`
	Status        string       `json:"status,omitempty"`
	ParentOU      string       `json:"parent_ou,omitempty"`
	DeviceCount   int          `json:"device_count"`
	TemplateCount int          `json:"template_count"`
	CreatedAt     FlexibleTime `json:"created_at"`
	UpdatedAt     FlexibleTime `json:"updated_at"`
	// Fields from describe endpoint
	Devices   []OUDevice   `json:"devices,omitempty"`
	Templates []OUTemplate `json:"templates,omitempty"`
}

// GetDeviceCount returns the device count from either field
func (ou *OrganizationalUnit) GetDeviceCount() int {
	if ou.DeviceCount > 0 {
		return ou.DeviceCount
	}
	return len(ou.Devices)
}

// OUListResponse represents a paginated list of OUs
type OUListResponse struct {
	OUs        []OrganizationalUnit `json:"ous"`
	Count      int                  `json:"count"`
	Total      int                  `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

// Account represents a user account in an organization
type Account struct {
	ID           string       `json:"id,omitempty"`
	Email        string       `json:"email"`
	Name         string       `json:"name,omitempty"`
	Role         string       `json:"role"`
	Status       string       `json:"status"`
	Organization string       `json:"organization,omitempty"`
	CreatedAt    FlexibleTime `json:"created_at"`
	UpdatedAt    FlexibleTime `json:"updated_at,omitempty"`
	LastLogin    FlexibleTime `json:"last_login,omitempty"`
}

// GetDisplayName returns the display name (uses Name field from API)
func (a *Account) GetDisplayName() string {
	return a.Name
}

// AccountListResponse represents the API response for listing accounts
type AccountListResponse struct {
	Accounts   []Account  `json:"accounts"`
	Pagination Pagination `json:"pagination"`
	Quota      *Quota     `json:"quota,omitempty"`
}

// Pagination represents the pagination info from API
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
	Pages    int `json:"pages"`
}

// AccountRole constants
const (
	RoleSuperuser = "SU"
	RoleReadWrite = "RW"
	RoleReadOnly  = "RO"
)

// Invitation represents an organization invitation
type Invitation struct {
	ID           string       `json:"id"`
	Email        string       `json:"email"`
	Organization string       `json:"organization"`
	Role         string       `json:"role"`
	Status       string       `json:"status"`
	InvitedBy    string       `json:"invited_by"`
	ExpiresAt    FlexibleTime `json:"expires_at"`
	CreatedAt    FlexibleTime `json:"created_at"`
}

// InvitationStatus constants
const (
	InvitationStatusPending  = "PENDING"
	InvitationStatusAccepted = "ACCEPTED"
	InvitationStatusDeclined = "DECLINED"
	InvitationStatusExpired  = "EXPIRED"
	InvitationStatusInvited  = "INVITED"
)

// Invite represents an invite in the invites list response (different from Invitation)
type Invite struct {
	Organization string       `json:"organization"`
	Role         string       `json:"role"`
	Status       string       `json:"status"`
	InvitedBy    string       `json:"invited_by"`
	Email        string       `json:"email,omitempty"` // Present in sent invites
	CreatedAt    FlexibleTime `json:"created_at"`
}

// InvitesResponse represents the response from GET /api/v1/invites
type InvitesResponse struct {
	Received []Invite `json:"received"`
	Sent     []Invite `json:"sent"`
}
