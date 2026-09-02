package httptransport

import (
	"time"

	tenant "github.com/lihongjie0209/tenant-service/internal/tenant"
)

type TenantBody struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

type MembershipBody struct {
	ID                        string    `json:"id"`
	TenantID                  string    `json:"tenant_id"`
	UserID                    string    `json:"user_id"`
	Status                    string    `json:"status"`
	PrimaryOrganizationUnitID string    `json:"primary_organization_unit_id"`
	JoinedAt                  time.Time `json:"joined_at"`
	Version                   int64     `json:"version"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
	CreatedBy                 string    `json:"created_by"`
	UpdatedBy                 string    `json:"updated_by"`
}

type OrganizationUnitBody struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	ParentID  string    `json:"parent_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

type InvitationBody struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Email            string    `json:"email"`
	Status           string    `json:"status"`
	ExpiresAt        time.Time `json:"expires_at"`
	AcceptedByUserID string    `json:"accepted_by_user_id"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	CreatedBy        string    `json:"created_by"`
	UpdatedBy        string    `json:"updated_by"`
}

type GroupBody struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

type GroupMemberBody struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	GroupID      string    `json:"group_id"`
	MembershipID string    `json:"membership_id"`
	Status       string    `json:"status"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedBy    string    `json:"created_by"`
	UpdatedBy    string    `json:"updated_by"`
}

type QuotaBody struct {
	TenantID  string    `json:"tenant_id"`
	Key       string    `json:"key"`
	Limit     int64     `json:"limit"`
	Used      int64     `json:"used"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

type TenantPageBody struct {
	Tenants  []TenantBody `json:"tenants"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

type MembershipPageBody struct {
	Memberships []MembershipBody `json:"memberships"`
	Total       int64            `json:"total"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
}

type InvitationPageBody struct {
	Invitations []InvitationBody `json:"invitations"`
	Total       int64            `json:"total"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
}

type QuotaPageBody struct {
	Quotas   []QuotaBody `json:"quotas"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

type CreateTenantResponseBody struct {
	Tenant          TenantBody     `json:"tenant"`
	OwnerMembership MembershipBody `json:"owner_membership"`
}

type AcceptInvitationResponseBody struct {
	Invitation InvitationBody `json:"invitation"`
	Membership MembershipBody `json:"membership"`
}

type GroupsResponseBody struct {
	Groups []GroupBody `json:"groups"`
}

type ConsumeQuotaResponseBody struct {
	Quota   QuotaBody `json:"quota"`
	Allowed bool      `json:"allowed"`
}

type AddGroupMemberResponseBody struct {
	Added bool `json:"added"`
}

type RemoveGroupMemberResponseBody struct {
	Removed bool `json:"removed"`
}

func tenantBody(value tenant.Tenant) TenantBody {
	return TenantBody{ID: value.ID, Code: value.Code, Name: value.Name, Status: value.Status, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func membershipBody(value tenant.Membership) MembershipBody {
	return MembershipBody{ID: value.ID, TenantID: value.TenantID, UserID: value.UserID, Status: value.Status, PrimaryOrganizationUnitID: value.PrimaryOrganizationUnitID, JoinedAt: value.JoinedAt, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func organizationUnitBody(value tenant.OrganizationUnit) OrganizationUnitBody {
	return OrganizationUnitBody{ID: value.ID, TenantID: value.TenantID, ParentID: value.ParentID, Code: value.Code, Name: value.Name, Path: value.Path, Status: value.Status, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func invitationBody(value tenant.Invitation) InvitationBody {
	return InvitationBody{ID: value.ID, TenantID: value.TenantID, Email: value.Email, Status: value.Status, ExpiresAt: value.ExpiresAt, AcceptedByUserID: value.AcceptedByUserID, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func groupBody(value tenant.Group) GroupBody {
	return GroupBody{ID: value.ID, TenantID: value.TenantID, Code: value.Code, Name: value.Name, Status: value.Status, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func groupMemberBody(value tenant.GroupMember) GroupMemberBody {
	return GroupMemberBody{ID: value.ID, TenantID: value.TenantID, GroupID: value.GroupID, MembershipID: value.MembershipID, Status: value.Status, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func quotaBody(value tenant.Quota) QuotaBody {
	return QuotaBody{TenantID: value.TenantID, Key: value.Key, Limit: value.Limit, Used: value.Used, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func mapBodies[S, D any](values []S, convert func(S) D) []D {
	result := make([]D, len(values))
	for index := range values {
		result[index] = convert(values[index])
	}
	return result
}

func tenantPageBody(value tenant.Page) TenantPageBody {
	return TenantPageBody{Tenants: mapBodies(value.Tenants, tenantBody), Total: value.Total, Page: value.Page, PageSize: value.PageSize}
}

func membershipPageBody(value tenant.MembershipPage) MembershipPageBody {
	return MembershipPageBody{Memberships: mapBodies(value.Memberships, membershipBody), Total: value.Total, Page: value.Page, PageSize: value.PageSize}
}

func invitationPageBody(value tenant.InvitationPage) InvitationPageBody {
	return InvitationPageBody{Invitations: mapBodies(value.Invitations, invitationBody), Total: value.Total, Page: value.Page, PageSize: value.PageSize}
}

func quotaPageBody(value tenant.QuotaPage) QuotaPageBody {
	return QuotaPageBody{Quotas: mapBodies(value.Quotas, quotaBody), Total: value.Total, Page: value.Page, PageSize: value.PageSize}
}
