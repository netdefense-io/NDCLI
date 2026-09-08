package models

// Support-ticket models. Field names mirror the NDManager response shapes
// exactly (see openapi.json, tag "support-tickets" / "support"): the
// org-side and support-side payloads are the same structs, the support side
// simply populates `organization` on tickets and `email` on SUPPORT authors.
//
// Everything textual here — subject, body, filename, author/participant
// name, organization name — is plain text by contract. It is sanitized once
// at decode time (api.DecodeJSON → sanitize.Struct); renderers print it as
// text and never as markup.

// Ticket enum values, mirrored from NDDataModels. Kept as slices because
// every consumer (flag validation, MCP schema enums) wants the list.
var (
	TicketStatuses         = []string{"OPEN", "PENDING", "CLOSED"}
	TicketPriorities       = []string{"LOW", "NORMAL", "HIGH", "URGENT"}
	TicketCategories       = []string{"BUG", "QUESTION", "FEATURE_REQUEST", "BILLING", "OTHER"}
	TicketInteractionKinds = []string{"RESPONSE", "INTERNAL_NOTE"}
	// TicketSortFields is the server's sort_by field set; the direction
	// (asc|desc) is validated client-side.
	TicketSortFields = []string{"last_activity_at", "created_at", "updated_at", "priority", "status", "subject"}
)

// Interaction kinds as constants for call sites that post one.
const (
	TicketKindResponse     = "RESPONSE"
	TicketKindInternalNote = "INTERNAL_NOTE"
)

// TicketActor is a referenced org account, or null when the account was
// hard-deleted (hence the pointer at every use site).
type TicketActor struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// TicketAuthor is the author of an interaction: an org user (with email) or
// NetDefense Support. `email` is only ever populated for a SUPPORT author on
// the support surface — never on the org side.
type TicketAuthor struct {
	Kind  string `json:"kind"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// DisplayName renders an author for a thread header without leaking a
// support responder's address on the org side.
func (a *TicketAuthor) DisplayName() string {
	if a == nil {
		return "(deleted account)"
	}
	switch {
	case a.Name != "":
		return a.Name
	case a.Email != "":
		return a.Email
	case a.Kind == "SUPPORT":
		return "NetDefense Support"
	default:
		return "(unknown)"
	}
}

// TicketDeviceRef is a device related to a ticket.
type TicketDeviceRef struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// TicketParticipant is a participant row on a ticket.
type TicketParticipant struct {
	Email     string       `json:"email"`
	Name      string       `json:"name,omitempty"`
	IsCreator bool         `json:"is_creator"`
	Enabled   bool         `json:"enabled"`
	AddedAt   FlexibleTime `json:"added_at"`
}

// TicketOrganizationRef is the owning organization of a ticket. Name only,
// never an id; present on support-side shapes.
type TicketOrganizationRef struct {
	Name string `json:"name,omitempty"`
}

// Ticket carries both the list-summary and the detail shape. The
// detail-only fields (participants, devices, interaction_count) are absent
// from a list row and decode to their zero values.
type Ticket struct {
	Code             string                 `json:"code"`
	Subject          string                 `json:"subject"`
	Status           string                 `json:"status"`
	Priority         string                 `json:"priority"`
	Category         string                 `json:"category"`
	CreatedBy        *TicketActor           `json:"created_by,omitempty"`
	ParticipantCount int                    `json:"participant_count"`
	DeviceCount      int                    `json:"device_count"`
	LastActivityAt   FlexibleTime           `json:"last_activity_at"`
	CreatedAt        FlexibleTime           `json:"created_at"`
	UpdatedAt        *FlexibleTime          `json:"updated_at,omitempty"`
	ClosedAt         *FlexibleTime          `json:"closed_at,omitempty"`
	WebURL           string                 `json:"web_url,omitempty"`
	Participants     []TicketParticipant    `json:"participants,omitempty"`
	Devices          []TicketDeviceRef      `json:"devices,omitempty"`
	InteractionCount int                    `json:"interaction_count"`
	Organization     *TicketOrganizationRef `json:"organization,omitempty"`
}

// OrgName returns the owning organization name, or "" on the org-side
// shapes that omit it.
func (t *Ticket) OrgName() string {
	if t == nil || t.Organization == nil {
		return ""
	}
	return t.Organization.Name
}

// TicketListResponse is the five-field paginated envelope returned by both
// list endpoints.
type TicketListResponse struct {
	Items   []Ticket               `json:"items"`
	Total   int                    `json:"total"`
	Page    int                    `json:"page"`
	PerPage int                    `json:"per_page"`
	Pages   int                    `json:"pages"`
	Filters map[string]interface{} `json:"filters,omitempty"`
}

// GetItems returns the tickets on this page.
func (r *TicketListResponse) GetItems() []Ticket {
	if r == nil {
		return nil
	}
	return r.Items
}

// TicketAttachment is an uploaded attachment, bound or still unbound.
// DownloadPath is a *relative* API path (NDManager has no public-API-base
// setting); the client prepends the API base it is already configured with.
type TicketAttachment struct {
	UUID         string        `json:"uuid"`
	Filename     string        `json:"filename"`
	ContentType  string        `json:"content_type"`
	SizeBytes    int64         `json:"size_bytes"`
	SHA256       string        `json:"sha256"`
	CreatedAt    FlexibleTime  `json:"created_at"`
	ExpiresAt    *FlexibleTime `json:"expires_at,omitempty"`
	TicketCode   string        `json:"ticket_code,omitempty"`
	DownloadPath string        `json:"download_path,omitempty"`
}

// TicketInteraction is one message on a ticket. Interactions are immutable
// once written. TicketStatus is set only on the create-interaction
// response, where it reveals an auto-reopen — clients render what came back
// rather than assuming their POST left the status alone.
type TicketInteraction struct {
	UUID         string             `json:"uuid"`
	Kind         string             `json:"kind"`
	Author       *TicketAuthor      `json:"author,omitempty"`
	Body         string             `json:"body"`
	CreatedAt    FlexibleTime       `json:"created_at"`
	Attachments  []TicketAttachment `json:"attachments,omitempty"`
	TicketStatus string             `json:"ticket_status,omitempty"`
}

// IsInternalNote reports whether this interaction is a side-private note.
func (i *TicketInteraction) IsInternalNote() bool {
	return i != nil && i.Kind == TicketKindInternalNote
}

// TicketInteractionListResponse is the paginated thread envelope.
type TicketInteractionListResponse struct {
	Items   []TicketInteraction `json:"items"`
	Total   int                 `json:"total"`
	Page    int                 `json:"page"`
	PerPage int                 `json:"per_page"`
	Pages   int                 `json:"pages"`
}

// GetItems returns the interactions on this page.
func (r *TicketInteractionListResponse) GetItems() []TicketInteraction {
	if r == nil {
		return nil
	}
	return r.Items
}

// TicketParticipantListResponse is the response of the participant
// add/remove endpoints: the full list after the change.
type TicketParticipantListResponse struct {
	Participants []TicketParticipant `json:"participants"`
	Count        int                 `json:"count"`
}

// GetItems returns the participants after the change.
func (r *TicketParticipantListResponse) GetItems() []TicketParticipant {
	if r == nil {
		return nil
	}
	return r.Participants
}

// TicketAttachmentDeleteResponse is the body of a delete-while-unbound call.
type TicketAttachmentDeleteResponse struct {
	Message string `json:"message"`
}

// SupportResponderProfile is the GET /support/me shape: who the caller is on
// the support side. A client shows the console only when this returns 200,
// and hides write controls when CanWrite is false.
type SupportResponderProfile struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Status      string `json:"status"`
	Principal   string `json:"principal"`
	CanWrite    bool   `json:"can_write"`
}

// TicketDownloadResult describes a completed attachment download. It is a
// local artefact, not a wire shape: Path is where the bytes landed and
// SHA256 is the digest actually computed over them, already verified
// against the attachment metadata from the thread listing.
type TicketDownloadResult struct {
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}
