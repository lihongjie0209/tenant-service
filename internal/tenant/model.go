package tenant

import "time"

type Tenant struct {
	ID        string    `db:"id" json:"id"`
	Code      string    `db:"code" json:"code"`
	Name      string    `db:"name" json:"name"`
	Status    string    `db:"status" json:"status"`
	Version   int64     `db:"version" json:"version"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}

type Membership struct {
	ID                        string    `db:"id" json:"id"`
	TenantID                  string    `db:"tenant_id" json:"tenant_id"`
	UserID                    string    `db:"user_id" json:"user_id"`
	Status                    string    `db:"status" json:"status"`
	PrimaryOrganizationUnitID string    `db:"primary_organization_unit_id" json:"primary_organization_unit_id"`
	JoinedAt                  time.Time `db:"joined_at" json:"joined_at"`
	Version                   int64     `db:"version" json:"version"`
	CreatedAt                 time.Time `db:"created_at" json:"created_at"`
	UpdatedAt                 time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy                 string    `db:"created_by" json:"created_by"`
	UpdatedBy                 string    `db:"updated_by" json:"updated_by"`
}

type OrganizationUnit struct {
	ID        string    `db:"id" json:"id"`
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	ParentID  string    `db:"parent_id" json:"parent_id"`
	Code      string    `db:"code" json:"code"`
	Name      string    `db:"name" json:"name"`
	Path      string    `db:"path" json:"path"`
	Status    string    `db:"status" json:"status"`
	Version   int64     `db:"version" json:"version"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}

type Invitation struct {
	ID               string    `db:"id" json:"id"`
	TenantID         string    `db:"tenant_id" json:"tenant_id"`
	Email            string    `db:"email" json:"email"`
	TokenHash        string    `db:"token_hash" json:"-"`
	Status           string    `db:"status" json:"status"`
	ExpiresAt        time.Time `db:"expires_at" json:"expires_at"`
	AcceptedByUserID string    `db:"accepted_by_user_id" json:"accepted_by_user_id"`
	Version          int64     `db:"version" json:"version"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy        string    `db:"created_by" json:"created_by"`
	UpdatedBy        string    `db:"updated_by" json:"updated_by"`
}

type Group struct {
	ID        string    `db:"id" json:"id"`
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	Code      string    `db:"code" json:"code"`
	Name      string    `db:"name" json:"name"`
	Status    string    `db:"status" json:"status"`
	Version   int64     `db:"version" json:"version"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}

type GroupMember struct {
	ID           string    `db:"id" json:"id"`
	TenantID     string    `db:"tenant_id" json:"tenant_id"`
	GroupID      string    `db:"group_id" json:"group_id"`
	MembershipID string    `db:"membership_id" json:"membership_id"`
	Status       string    `db:"status" json:"status"`
	Version      int64     `db:"version" json:"version"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy    string    `db:"created_by" json:"created_by"`
	UpdatedBy    string    `db:"updated_by" json:"updated_by"`
}

type Quota struct {
	TenantID  string    `db:"tenant_id" json:"tenant_id"`
	Key       string    `db:"quota_key" json:"key"`
	Limit     int64     `db:"limit_value" json:"limit"`
	Used      int64     `db:"used_value" json:"used"`
	Version   int64     `db:"version" json:"version"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy string    `db:"created_by" json:"created_by"`
	UpdatedBy string    `db:"updated_by" json:"updated_by"`
}

type InvitationPage struct {
	Invitations []Invitation `json:"invitations"`
	Total       int64        `json:"total"`
	Page        int          `json:"page"`
	PageSize    int          `json:"page_size"`
}

type Page struct {
	Tenants  []Tenant `json:"tenants"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

type MembershipPage struct {
	Memberships []Membership `json:"memberships"`
	Total       int64        `json:"total"`
	Page        int          `json:"page"`
	PageSize    int          `json:"page_size"`
}

type OutboxEvent struct {
	ID          string
	Subject     string
	Envelope    []byte
	AvailableAt time.Time
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
}
