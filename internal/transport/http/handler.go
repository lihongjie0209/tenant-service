package httptransport

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/buildinfo"
	"github.com/lihongjie0209/tenant-service/internal/health"
	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	tenant "github.com/lihongjie0209/tenant-service/internal/tenant"
)

type Handler struct {
	logger *slog.Logger
	health *health.Service

	tenants  *tenant.Service
	issuer   identityclient.Issuer
	identity identityclient.Directory
}

func NewHandler(healthService *health.Service, tenantService *tenant.Service, issuer *identityclient.Client, logger *slog.Logger) *Handler {
	return &Handler{health: healthService, tenants: tenantService, issuer: issuer, identity: issuer, logger: logger}
}

type MeResponseBody struct {
	Subject string `json:"subject"`
}

type CreateTenantRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	OwnerUserID string `json:"owner_user_id" binding:"required"`
}
type SelectTenantRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
}
type SelectTenantResponseBody struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	TenantID     string    `json:"tenant_id"`
	MembershipID string    `json:"membership_id"`
}
type GetTenantRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
}
type UpdateTenantRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Status   string `json:"status" binding:"required"`
	Version  int64  `json:"version" binding:"required"`
	Reason   string `json:"reason"`
}
type AddMembershipRequest struct {
	TenantID                  string `json:"tenant_id" binding:"required"`
	UserID                    string `json:"user_id" binding:"required"`
	PrimaryOrganizationUnitID string `json:"primary_organization_unit_id"`
}
type UpdateMembershipRequest struct {
	MembershipID              string `json:"membership_id" binding:"required"`
	Status                    string `json:"status" binding:"required"`
	PrimaryOrganizationUnitID string `json:"primary_organization_unit_id"`
	Version                   int64  `json:"version" binding:"required"`
	Reason                    string `json:"reason"`
}
type ListMembershipsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type BatchGetMembershipsRequest struct {
	TenantID      string   `json:"tenant_id" binding:"required"`
	MembershipIDs []string `json:"membership_ids" binding:"required"`
}
type SearchMembershipDirectoryRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Keyword  string `json:"keyword"`
	Limit    int    `json:"limit"`
}
type ListUserTenantsRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type ListTenantsRequest struct {
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type CreateOrganizationUnitRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	ParentID string `json:"parent_id"`
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
}
type GetOrganizationUnitRequest struct {
	OrganizationUnitID string `json:"organization_unit_id" binding:"required"`
}
type UpdateOrganizationUnitRequest struct {
	OrganizationUnitID string `json:"organization_unit_id" binding:"required"`
	ParentID           string `json:"parent_id"`
	Name               string `json:"name" binding:"required"`
	Status             string `json:"status" binding:"required"`
	Version            int64  `json:"version" binding:"required"`
}
type ListOrganizationUnitsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
}
type OrganizationUnitsResponseBody struct {
	OrganizationUnits []OrganizationUnitBody `json:"organization_units"`
}
type CreateInvitationRequest struct {
	TenantID         string `json:"tenant_id" binding:"required"`
	Email            string `json:"email" binding:"required"`
	ExpiresInSeconds int64  `json:"expires_in_seconds" binding:"required,gt=0"`
}
type CreateInvitationResponseBody struct {
	Invitation InvitationBody `json:"invitation"`
	Token      string         `json:"token"`
}
type AcceptInvitationRequest struct {
	Token string `json:"token" binding:"required"`
}
type RevokeInvitationRequest struct {
	InvitationID string `json:"invitation_id" binding:"required"`
	Version      int64  `json:"version" binding:"required,gt=0"`
}
type ListInvitationsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type CreateGroupRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
}
type UpdateGroupRequest struct {
	GroupID string `json:"group_id" binding:"required"`
	Name    string `json:"name" binding:"required"`
	Status  string `json:"status" binding:"required"`
	Version int64  `json:"version" binding:"required,gt=0"`
}
type GroupMemberRequest struct {
	GroupID      string `json:"group_id" binding:"required"`
	MembershipID string `json:"membership_id" binding:"required"`
}
type RemoveGroupMemberRequest struct {
	GroupID      string `json:"group_id" binding:"required"`
	MembershipID string `json:"membership_id" binding:"required"`
	Version      int64  `json:"version" binding:"required,gt=0"`
}
type ListGroupMembersRequest struct {
	GroupID string `json:"group_id" binding:"required"`
}
type GroupMembersResponseBody struct {
	GroupMembers []GroupMemberBody `json:"group_members"`
}
type ListGroupsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
}
type SearchGroupsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type GetQuotaRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Key      string `json:"key" binding:"required"`
}
type ListQuotasRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Keyword  string `json:"keyword"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
type SetQuotaRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Key      string `json:"key" binding:"required"`
	Limit    int64  `json:"limit" binding:"gte=0"`
	Version  int64  `json:"version" binding:"gte=0"`
}
type ConsumeQuotaRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Key      string `json:"key" binding:"required"`
	Amount   int64  `json:"amount" binding:"required,gt=0"`
}

// Login godoc
// @Summary Issue a JWT access token
// @Tags authentication
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Client credentials"
// @Success 200 {object} Response{body=LoginResponseBody}
// @Failure 400 {object} Response "Code 10001: invalid request"
// @Failure 401 {object} Response "Code 20001: invalid credentials"
// @Failure 429 {object} Response "Code 10029: rate limited"

// Live godoc
// @Summary Check process liveness
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=health.Status}
// @Router /live [post]
func (h *Handler) Live(c *gin.Context) { OK(c, h.health.Live()) }

// Ready godoc
// @Summary Check database and Redis readiness
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=health.Status}
// @Failure 503 {object} Response{body=health.Status} "Code 50003: dependency unavailable"
// @Router /ready [post]
func (h *Handler) Ready(c *gin.Context) {
	status, ready := h.health.Ready(c.Request.Context())
	if !ready {
		c.JSON(503, Response{Code: apperror.CodeDependencyUnavailable, Message: "service is not ready", Body: status, RequestID: requestID(c)})
		return
	}
	OK(c, status)
}

// Me godoc
// @Summary Return the authenticated subject
// @Tags authentication
// @Produce json
// @Security Bearer
// @Success 200 {object} Response{body=MeResponseBody}
// @Failure 401 {object} Response "Code 20001: unauthorized"
// @Router /api/v1/me [post]
func (h *Handler) Me(c *gin.Context) {
	subject, _ := c.Get("subject")
	OK(c, gin.H{"subject": subject})
}

// Version godoc
// @Summary Return build and runtime version information
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=buildinfo.Info}
// @Router /api/v1/version [post]
func (h *Handler) Version(c *gin.Context) { OK(c, buildinfo.Current()) }

// CreateTenant godoc
// @Summary Create a tenant owned by the authenticated user
// @Tags tenants
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body CreateTenantRequest true "Tenant"
// @Success 200 {object} Response{body=CreateTenantResponseBody}
// @Router /api/v1/tenants/create [post]
func (h *Handler) CreateTenant(c *gin.Context) {
	h.createTenant(c)
}

// CreateManagedTenant godoc
// @Summary Create a tenant for a target owner as a platform administrator
// @Tags tenants
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body CreateTenantRequest true "Tenant and target owner"
// @Success 200 {object} Response{body=CreateTenantResponseBody}
// @Router /api/v1/tenants/manage/create [post]
func (h *Handler) CreateManagedTenant(c *gin.Context) {
	h.createTenant(c)
}

func (h *Handler) createTenant(c *gin.Context) {
	var request CreateTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	created, owner, err := h.tenants.Create(c.Request.Context(), request.Code, request.Name, request.OwnerUserID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, CreateTenantResponseBody{Tenant: tenantBody(created), OwnerMembership: membershipBody(owner)})
}

// SelectTenant godoc
// @Summary Validate the current user's membership and exchange a tenant-scoped access token
// @Tags tenants
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body SelectTenantRequest true "Tenant selection"
// @Success 200 {object} Response{body=SelectTenantResponseBody}
// @Failure 403 {object} Response "Code 20003: inactive or missing membership"
// @Failure 503 {object} Response "Code 50003: identity service unavailable"
// @Router /api/v1/tenants/select [post]
func (h *Handler) SelectTenant(c *gin.Context) {
	var request SelectTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	identity, err := principal.Require(c.Request.Context())
	if err != nil || identity.Type != principal.TypeUser || identity.ID == "" || identity.SessionID == "" {
		Fail(c, h.logger, apperror.Unauthorized("an interactive user session is required"))
		return
	}
	_, membership, valid := h.tenants.ValidateMembership(c.Request.Context(), identity.ID, request.TenantID)
	if !valid || membership.ID == "" {
		Fail(c, h.logger, apperror.Forbidden("active tenant membership is required"))
		return
	}
	token, expiresAt, err := h.issuer.IssueTenantToken(c.Request.Context(), identity.ID, request.TenantID, membership.ID, identity.SessionID)
	if err != nil {
		Fail(c, h.logger, apperror.Unavailable("identity service is unavailable", errors.Join(identityclient.ErrUnavailable, err)))
		return
	}
	OK(c, SelectTenantResponseBody{AccessToken: token, TokenType: "Bearer", ExpiresAt: expiresAt, TenantID: request.TenantID, MembershipID: membership.ID})
}

// GetTenant godoc
// @Summary Get a tenant
// @Tags tenants
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetTenantRequest true "Tenant"
// @Success 200 {object} Response{body=TenantBody}
// @Router /api/v1/tenants/get [post]
func (h *Handler) GetTenant(c *gin.Context) {
	var request GetTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.Get(c.Request.Context(), request.TenantID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, tenantBody(value))
}

// UpdateTenant godoc
// @Summary Update a tenant using optimistic locking
// @Tags tenants
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateTenantRequest true "Tenant and current version"
// @Success 200 {object} Response{body=TenantBody}
// @Router /api/v1/tenants/update [post]
func (h *Handler) UpdateTenant(c *gin.Context) {
	var request UpdateTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.Update(c.Request.Context(), request.TenantID, request.Name, request.Status, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, tenantBody(value))
}

// AddMembership godoc
// @Summary Add a tenant membership
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body AddMembershipRequest true "Membership"
// @Success 200 {object} Response{body=MembershipBody}
// @Router /api/v1/memberships/add [post]
func (h *Handler) AddMembership(c *gin.Context) {
	var request AddMembershipRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.AddMembership(c.Request.Context(), request.TenantID, request.UserID, request.PrimaryOrganizationUnitID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, membershipBody(value))
}

// UpdateMembership godoc
// @Summary Update a tenant membership using optimistic locking
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateMembershipRequest true "Membership and current version"
// @Success 200 {object} Response{body=MembershipBody}
// @Router /api/v1/memberships/update [post]
func (h *Handler) UpdateMembership(c *gin.Context) {
	var request UpdateMembershipRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.UpdateMembership(c.Request.Context(), request.MembershipID, request.Status, request.PrimaryOrganizationUnitID, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, membershipBody(value))
}

// ListMemberships godoc
// @Summary List tenant memberships
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListMembershipsRequest true "Tenant, filters and pagination"
// @Success 200 {object} Response{body=MembershipPageBody}
// @Router /api/v1/memberships/list [post]
func (h *Handler) ListMemberships(c *gin.Context) {
	var request ListMembershipsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListMemberships(c.Request.Context(), request.TenantID, request.UserID, request.Status, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, membershipPageBody(value))
}

// BatchGetMemberships godoc
// @Summary Get a bounded set of tenant memberships by ID
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body BatchGetMembershipsRequest true "Tenant and membership IDs (maximum 100)"
// @Success 200 {object} Response{body=MembershipBatchBody}
// @Router /api/v1/memberships/batch-get [post]
func (h *Handler) BatchGetMemberships(c *gin.Context) {
	var request BatchGetMembershipsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	items, err := h.tenants.BatchGetMemberships(c.Request.Context(), request.TenantID, request.MembershipIDs)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, membershipBatchBody(items))
}

// SearchMembershipDirectory godoc
// @Summary Search active tenant members by user identity
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body SearchMembershipDirectoryRequest true "Tenant, identity keyword and result limit"
// @Success 200 {object} Response{body=MembershipDirectoryBody}
// @Router /api/v1/memberships/directory/search [post]
func (h *Handler) SearchMembershipDirectory(c *gin.Context) {
	var request SearchMembershipDirectoryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		Fail(c, h.logger, apperror.Invalid("limit must not exceed 50", nil))
		return
	}
	if h.identity == nil {
		Fail(c, h.logger, apperror.Unavailable("identity service is unavailable", identityclient.ErrUnavailable))
		return
	}
	const candidatePageSize = 100
	const maxCandidatePages = 5
	items := make([]MembershipDirectoryItemBody, 0, limit)
	for page := 1; page <= maxCandidatePages && len(items) < limit; page++ {
		users, err := h.identity.ListUsers(c.Request.Context(), request.Keyword, page, candidatePageSize)
		if err != nil {
			Fail(c, h.logger, apperror.Unavailable("identity service is unavailable", err))
			return
		}
		userIDs := make([]string, 0, len(users.Users))
		for _, user := range users.Users {
			userIDs = append(userIDs, user.ID)
		}
		memberships, err := h.tenants.FindMembershipsByUserIDs(c.Request.Context(), request.TenantID, userIDs, "active")
		if err != nil {
			Fail(c, h.logger, err)
			return
		}
		membershipByUserID := make(map[string]tenant.Membership, len(memberships))
		for _, membership := range memberships {
			membershipByUserID[membership.UserID] = membership
		}
		for _, user := range users.Users {
			membership, ok := membershipByUserID[user.ID]
			if !ok {
				continue
			}
			items = append(items, membershipDirectoryItemBody(membership, user))
			if len(items) == limit {
				break
			}
		}
		if len(users.Users) < candidatePageSize || uint64(page*candidatePageSize) >= users.Total {
			break
		}
	}
	OK(c, MembershipDirectoryBody{Items: items})
}

// ListUserTenants godoc
// @Summary List tenant memberships for a user
// @Tags memberships
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListUserTenantsRequest true "User and pagination"
// @Success 200 {object} Response{body=TenantPageBody}
// @Router /api/v1/tenants/list-by-user [post]
func (h *Handler) ListUserTenants(c *gin.Context) {
	var request ListUserTenantsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListUserTenants(c.Request.Context(), request.UserID, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, tenantPageBody(value))
}

// ListTenants godoc
// @Summary List tenants
// @Tags tenants
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body ListTenantsRequest true "Filters and pagination"
// @Success 200 {object} Response{body=TenantPageBody}
// @Router /api/v1/tenants/list [post]
func (h *Handler) ListTenants(c *gin.Context) {
	var request ListTenantsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListTenants(c.Request.Context(), request.Keyword, request.Status, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, tenantPageBody(value))
}

// CreateOrganizationUnit godoc
// @Summary Create an organization unit
// @Tags organization-units
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateOrganizationUnitRequest true "Organization unit"
// @Success 200 {object} Response{body=OrganizationUnitBody}
// @Router /api/v1/organization-units/create [post]
func (h *Handler) CreateOrganizationUnit(c *gin.Context) {
	var request CreateOrganizationUnitRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.CreateOrganizationUnit(c.Request.Context(), request.TenantID, request.ParentID, request.Code, request.Name)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, organizationUnitBody(value))
}

// GetOrganizationUnit godoc
// @Summary Get an organization unit
// @Tags organization-units
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetOrganizationUnitRequest true "Organization unit ID"
// @Success 200 {object} Response{body=OrganizationUnitBody}
// @Router /api/v1/organization-units/get [post]
func (h *Handler) GetOrganizationUnit(c *gin.Context) {
	var request GetOrganizationUnitRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.GetOrganizationUnit(c.Request.Context(), request.OrganizationUnitID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, organizationUnitBody(value))
}

// UpdateOrganizationUnit godoc
// @Summary Update or move an organization unit using optimistic locking
// @Tags organization-units
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateOrganizationUnitRequest true "Organization unit and current version"
// @Success 200 {object} Response{body=OrganizationUnitBody}
// @Router /api/v1/organization-units/update [post]
func (h *Handler) UpdateOrganizationUnit(c *gin.Context) {
	var request UpdateOrganizationUnitRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.UpdateOrganizationUnit(c.Request.Context(), request.OrganizationUnitID, request.ParentID, request.Name, request.Status, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, organizationUnitBody(value))
}

// ListOrganizationUnits godoc
// @Summary List a tenant's organization units
// @Tags organization-units
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListOrganizationUnitsRequest true "Tenant"
// @Success 200 {object} Response{body=OrganizationUnitsResponseBody}
// @Router /api/v1/organization-units/list [post]
func (h *Handler) ListOrganizationUnits(c *gin.Context) {
	var request ListOrganizationUnitsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListOrganizationUnits(c.Request.Context(), request.TenantID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, OrganizationUnitsResponseBody{OrganizationUnits: mapBodies(value, organizationUnitBody)})
}

// CreateInvitation godoc
// @Summary Create a tenant invitation
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateInvitationRequest true "Invitation"
// @Success 200 {object} Response{body=CreateInvitationResponseBody}
// @Router /api/v1/invitations/create [post]
func (h *Handler) CreateInvitation(c *gin.Context) {
	var request CreateInvitationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, token, err := h.tenants.CreateInvitation(c.Request.Context(), request.TenantID, request.Email, time.Duration(request.ExpiresInSeconds)*time.Second)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, CreateInvitationResponseBody{Invitation: invitationBody(value), Token: token})
}

// AcceptInvitation godoc
// @Summary Accept an invitation as the authenticated subject
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body AcceptInvitationRequest true "Invitation token"
// @Success 200 {object} Response{body=AcceptInvitationResponseBody}
// @Router /api/v1/invitations/accept [post]
func (h *Handler) AcceptInvitation(c *gin.Context) {
	var request AcceptInvitationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	actor, err := principal.Require(c.Request.Context())
	if err != nil {
		Fail(c, h.logger, apperror.Unauthorized("authenticated actor is required"))
		return
	}
	value, membership, err := h.tenants.AcceptInvitation(c.Request.Context(), request.Token, actor.ID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, AcceptInvitationResponseBody{Invitation: invitationBody(value), Membership: membershipBody(membership)})
}

// RevokeInvitation godoc
// @Summary Revoke a pending invitation using optimistic locking
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body RevokeInvitationRequest true "Invitation and current version"
// @Success 200 {object} Response{body=InvitationBody}
// @Router /api/v1/invitations/revoke [post]
func (h *Handler) RevokeInvitation(c *gin.Context) {
	var request RevokeInvitationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.RevokeInvitation(c.Request.Context(), request.InvitationID, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, invitationBody(value))
}

// ListInvitations godoc
// @Summary List tenant invitations
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListInvitationsRequest true "Tenant and pagination"
// @Success 200 {object} Response{body=InvitationPageBody}
// @Router /api/v1/invitations/list [post]
func (h *Handler) ListInvitations(c *gin.Context) {
	var request ListInvitationsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListInvitations(c.Request.Context(), request.TenantID, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, invitationPageBody(value))
}

// CreateGroup godoc
// @Summary Create a tenant member group
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateGroupRequest true "Group"
// @Success 200 {object} Response{body=GroupBody}
// @Router /api/v1/groups/create [post]
func (h *Handler) CreateGroup(c *gin.Context) {
	var request CreateGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.CreateGroup(c.Request.Context(), request.TenantID, request.Code, request.Name)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, groupBody(value))
}

// UpdateGroup godoc
// @Summary Update a member group using optimistic locking
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateGroupRequest true "Group and current version"
// @Success 200 {object} Response{body=GroupBody}
// @Router /api/v1/groups/update [post]
func (h *Handler) UpdateGroup(c *gin.Context) {
	var request UpdateGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.UpdateGroup(c.Request.Context(), request.GroupID, request.Name, request.Status, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, groupBody(value))
}

// AddGroupMember godoc
// @Summary Add a membership to a group
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GroupMemberRequest true "Group membership"
// @Success 200 {object} Response{body=AddGroupMemberResponseBody}
// @Router /api/v1/groups/member-add [post]
func (h *Handler) AddGroupMember(c *gin.Context) {
	var request GroupMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	if err := h.tenants.AddGroupMember(c.Request.Context(), request.GroupID, request.MembershipID); err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, AddGroupMemberResponseBody{Added: true})
}

// RemoveGroupMember godoc
// @Summary Remove a membership from a group using optimistic locking
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body RemoveGroupMemberRequest true "Group membership and current version"
// @Success 200 {object} Response{body=RemoveGroupMemberResponseBody}
// @Router /api/v1/groups/member-remove [post]
func (h *Handler) RemoveGroupMember(c *gin.Context) {
	var request RemoveGroupMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	if err := h.tenants.RemoveGroupMember(c.Request.Context(), request.GroupID, request.MembershipID, request.Version); err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, RemoveGroupMemberResponseBody{Removed: true})
}

// ListGroupMembers godoc
// @Summary List assignments for a member group
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListGroupMembersRequest true "Member group"
// @Success 200 {object} Response{body=GroupMembersResponseBody}
// @Router /api/v1/groups/members/list [post]
func (h *Handler) ListGroupMembers(c *gin.Context) {
	var request ListGroupMembersRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	values, err := h.tenants.ListGroupMembers(c.Request.Context(), request.GroupID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, GroupMembersResponseBody{GroupMembers: mapBodies(values, groupMemberBody)})
}

// ListGroups godoc
// @Summary List tenant member groups
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListGroupsRequest true "Tenant"
// @Success 200 {object} Response{body=GroupsResponseBody}
// @Router /api/v1/groups/list [post]
func (h *Handler) ListGroups(c *gin.Context) {
	var request ListGroupsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListGroups(c.Request.Context(), request.TenantID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, GroupsResponseBody{Groups: mapBodies(value, groupBody)})
}

// SearchGroups godoc
// @Summary Search tenant member groups with pagination
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body SearchGroupsRequest true "Tenant, filters and pagination"
// @Success 200 {object} Response{body=GroupPageBody}
// @Router /api/v1/groups/search [post]
func (h *Handler) SearchGroups(c *gin.Context) {
	var request SearchGroupsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.SearchGroups(c.Request.Context(), request.TenantID, request.Keyword, request.Status, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, groupPageBody(value))
}

// GetQuota godoc
// @Summary Get a tenant quota
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetQuotaRequest true "Tenant quota key"
// @Success 200 {object} Response{body=QuotaBody}
// @Router /api/v1/quotas/get [post]
func (h *Handler) GetQuota(c *gin.Context) {
	var request GetQuotaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.GetQuota(c.Request.Context(), request.TenantID, request.Key)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, quotaBody(value))
}

// ListQuotas godoc
// @Summary List tenant quotas
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListQuotasRequest true "Tenant, keyword and pagination"
// @Success 200 {object} Response{body=QuotaPageBody}
// @Router /api/v1/quotas/list [post]
func (h *Handler) ListQuotas(c *gin.Context) {
	var request ListQuotasRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.ListQuotas(c.Request.Context(), request.TenantID, request.Keyword, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, quotaPageBody(value))
}

// SetQuota godoc
// @Summary Create or update a tenant quota using optimistic locking
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body SetQuotaRequest true "Quota and current version; version 0 creates"
// @Success 200 {object} Response{body=QuotaBody}
// @Router /api/v1/quotas/set [post]
func (h *Handler) SetQuota(c *gin.Context) {
	var request SetQuotaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.tenants.SetQuota(c.Request.Context(), request.TenantID, request.Key, request.Limit, request.Version)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, quotaBody(value))
}

// ConsumeQuota godoc
// @Summary Atomically consume tenant quota
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ConsumeQuotaRequest true "Quota consumption"
// @Success 200 {object} Response{body=ConsumeQuotaResponseBody}
// @Router /api/v1/quotas/consume [post]
func (h *Handler) ConsumeQuota(c *gin.Context) {
	var request ConsumeQuotaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, allowed, err := h.tenants.ConsumeQuota(c.Request.Context(), request.TenantID, request.Key, request.Amount)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, ConsumeQuotaResponseBody{Quota: quotaBody(value), Allowed: allowed})
}
