package httptransport

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/buildinfo"
	"github.com/lihongjie0209/tenant-service/internal/health"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"
)

type Handler struct {
	logger *slog.Logger
	health *health.Service

	tenants *tenantdomain.Service
}

func NewHandler(healthService *health.Service, tenantService *tenantdomain.Service, logger *slog.Logger) *Handler {
	return &Handler{health: healthService, tenants: tenantService, logger: logger}
}

type MeResponseBody struct {
	Subject string `json:"subject"`
}

type CreateTenantRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	OwnerUserID string `json:"owner_user_id" binding:"required"`
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
type CreateInvitationRequest struct {
	TenantID         string `json:"tenant_id" binding:"required"`
	Email            string `json:"email" binding:"required"`
	ExpiresInSeconds int64  `json:"expires_in_seconds" binding:"required,gt=0"`
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
type ListGroupsRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
}
type GetQuotaRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Key      string `json:"key" binding:"required"`
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

// CreateUser godoc
// @Summary Create a user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateUserRequest true "User"
// @Success 200 {object} Response{body=user.User}
// @Failure 400 {object} Response "Code 10001: invalid request"
// @Failure 409 {object} Response "Code 30009: email already exists"

// GetUser godoc
// @Summary Get a user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetUserRequest true "User ID"
// @Success 200 {object} Response{body=user.User}
// @Failure 404 {object} Response "Code 10004: user not found"

// ListUsers godoc
// @Summary List users
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListUsersRequest true "Pagination"
// @Success 200 {object} Response{body=user.Page}

// UpdateUser godoc
// @Summary Update a user using optimistic locking
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateUserRequest true "User and current version"
// @Success 200 {object} Response{body=user.User}
// @Failure 409 {object} Response "Code 30009: version conflict"

// DeleteUser godoc
// @Summary Delete a user using optimistic locking
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body DeleteUserRequest true "User ID and current version"
// @Success 200 {object} Response

func (h *Handler) CreateTenant(c *gin.Context) {
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
	OK(c, gin.H{"tenant": created, "owner_membership": owner})
}
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, value)
}

// ListTenants godoc
// @Summary List tenants
// @Tags tenants
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body ListTenantsRequest true "Filters and pagination"
// @Success 200 {object} Response
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, value)
}
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
	OK(c, gin.H{"organization_units": value})
}

// CreateInvitation godoc
// @Summary Create a tenant invitation
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateInvitationRequest true "Invitation"
// @Success 200 {object} Response
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
	OK(c, gin.H{"invitation": value, "token": token})
}

// AcceptInvitation godoc
// @Summary Accept an invitation as the authenticated subject
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body AcceptInvitationRequest true "Invitation token"
// @Success 200 {object} Response
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
	OK(c, gin.H{"invitation": value, "membership": membership})
}

// RevokeInvitation godoc
// @Summary Revoke a pending invitation using optimistic locking
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body RevokeInvitationRequest true "Invitation and current version"
// @Success 200 {object} Response{body=tenant.Invitation}
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
	OK(c, value)
}

// ListInvitations godoc
// @Summary List tenant invitations
// @Tags invitations
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListInvitationsRequest true "Tenant and pagination"
// @Success 200 {object} Response{body=tenant.InvitationPage}
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
	OK(c, value)
}

// CreateGroup godoc
// @Summary Create a tenant member group
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateGroupRequest true "Group"
// @Success 200 {object} Response{body=tenant.Group}
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
	OK(c, value)
}

// UpdateGroup godoc
// @Summary Update a member group using optimistic locking
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateGroupRequest true "Group and current version"
// @Success 200 {object} Response{body=tenant.Group}
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
	OK(c, value)
}

// AddGroupMember godoc
// @Summary Add a membership to a group
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GroupMemberRequest true "Group membership"
// @Success 200 {object} Response
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
	OK(c, gin.H{"added": true})
}

// RemoveGroupMember godoc
// @Summary Remove a membership from a group using optimistic locking
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body RemoveGroupMemberRequest true "Group membership and current version"
// @Success 200 {object} Response
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
	OK(c, gin.H{"removed": true})
}

// ListGroups godoc
// @Summary List tenant member groups
// @Tags groups
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListGroupsRequest true "Tenant"
// @Success 200 {object} Response
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
	OK(c, gin.H{"groups": value})
}

// GetQuota godoc
// @Summary Get a tenant quota
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetQuotaRequest true "Tenant quota key"
// @Success 200 {object} Response{body=tenant.Quota}
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
	OK(c, value)
}

// SetQuota godoc
// @Summary Create or update a tenant quota using optimistic locking
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body SetQuotaRequest true "Quota and current version; version 0 creates"
// @Success 200 {object} Response{body=tenant.Quota}
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
	OK(c, value)
}

// ConsumeQuota godoc
// @Summary Atomically consume tenant quota
// @Tags quotas
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ConsumeQuotaRequest true "Quota consumption"
// @Success 200 {object} Response
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
	OK(c, gin.H{"quota": value, "allowed": allowed})
}
