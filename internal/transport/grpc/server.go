package grpctransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	platformidempotency "github.com/lihongjie0209/microservice-platform-go/idempotency"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	dictionaryv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/dictionary/v1"
	tenantv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/tenant/v1"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/auth"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/environment"
	apphealth "github.com/lihongjie0209/tenant-service/internal/health"
	"github.com/lihongjie0209/tenant-service/internal/idempotency"
	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	"github.com/lihongjie0209/tenant-service/internal/membershipdirectory"
	"github.com/lihongjie0209/tenant-service/internal/observability"
	"github.com/lihongjie0209/tenant-service/internal/requestid"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	server  *grpc.Server
	address string
	logger  *slog.Logger
}

func NewServer(lc fx.Lifecycle, cfg config.Config, authService *auth.Service, authorizer platformauthz.Authorizer, healthService *apphealth.Service, tenantService *tenantdomain.Service, identityDirectory *identityclient.Client, dictionaryProvider *tenantdomain.DictionaryProvider, idempotencyManager *idempotency.Manager, metrics *observability.Metrics, logger *slog.Logger) (*Server, error) {
	options := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.GRPC.MaxReceiveBytes),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(environmentInterceptor(cfg.Runtime.ActiveProfile), requestIDInterceptor, idempotencyInterceptor, recoveryInterceptor(logger), authInterceptor(authService, cfg.Auth), platformauthz.UnaryServerInterceptor(authorizer, tenantGRPCRequirement(cfg.Authorization.Enabled)), platformidempotency.UnaryServerInterceptor(idempotencyManager, cfg.Idempotency.GRPCMethods, logger), metricsInterceptor(metrics, logger)),
		grpc.ChainStreamInterceptor(environmentStreamInterceptor(cfg.Runtime.ActiveProfile), requestIDStreamInterceptor, idempotencyStreamInterceptor, recoveryStreamInterceptor(logger), authStreamInterceptor(authService, cfg.Auth), metricsStreamInterceptor(metrics, logger)),
	}
	if cfg.GRPC.TLS.Enabled {
		creds, err := serverCredentials(cfg.GRPC.TLS)
		if err != nil {
			return nil, err
		}
		options = append(options, grpc.Creds(creds))
	}
	grpcServer := grpc.NewServer(options...)
	tenantv1.RegisterTenantServiceServer(grpcServer, &tenantServer{service: tenantService, identity: identityDirectory})
	dictionaryv1.RegisterDictionaryProviderServiceServer(grpcServer, &dictionaryProviderServer{provider: dictionaryProvider})
	grpc_health_v1.RegisterHealthServer(grpcServer, &healthServer{health: healthService})
	if cfg.GRPC.ReflectionEnabled {
		reflection.Register(grpcServer)
	}
	server := &Server{server: grpcServer, address: cfg.GRPC.Address, logger: logger}
	lc.Append(fx.Hook{OnStart: server.start(cfg.GRPC.Enabled), OnStop: server.stop})
	return server, nil
}

func tenantGRPCRequirement(enabled bool) platformauthz.GRPCResolver {
	return func(method string) (platformauthz.Requirement, bool) {
		if !enabled {
			return platformauthz.Requirement{}, false
		}
		requirements := map[string]platformauthz.Requirement{
			tenantv1.TenantService_GetMembership_FullMethodName: {Resource: "tenant.membership", Action: "read", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_GetGroup_FullMethodName:      {Resource: "tenant.group", Action: "read", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_GetTenant_FullMethodName:     {Resource: "tenant.profile", Action: "read", Scope: platformauthz.ScopePlatform}, tenantv1.TenantService_ListTenants_FullMethodName: {Resource: "tenant.profile", Action: "list", Scope: platformauthz.ScopePlatform}, tenantv1.TenantService_UpdateTenant_FullMethodName: {Resource: "tenant.profile", Action: "update", Scope: platformauthz.ScopePlatform},
			tenantv1.TenantService_AddMembership_FullMethodName: {Resource: "tenant.membership", Action: "create", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_UpdateMembership_FullMethodName: {Resource: "tenant.membership", Action: "update", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListMemberships_FullMethodName: {Resource: "tenant.membership", Action: "list", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_SearchMembershipDirectory_FullMethodName: {Resource: "tenant.membership", Action: "list", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_CreateOrganizationUnit_FullMethodName:    {Resource: "tenant.organization-unit", Action: "create", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_GetOrganizationUnit_FullMethodName: {Resource: "tenant.organization-unit", Action: "read", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_UpdateOrganizationUnit_FullMethodName: {Resource: "tenant.organization-unit", Action: "update", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListOrganizationUnits_FullMethodName: {Resource: "tenant.organization-unit", Action: "list", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_CreateInvitation_FullMethodName: {Resource: "tenant.invitation", Action: "create", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_GetInvitation_FullMethodName: {Resource: "tenant.invitation", Action: "read", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_RevokeInvitation_FullMethodName: {Resource: "tenant.invitation", Action: "revoke", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListInvitations_FullMethodName: {Resource: "tenant.invitation", Action: "list", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_CreateGroup_FullMethodName: {Resource: "tenant.group", Action: "create", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_UpdateGroup_FullMethodName: {Resource: "tenant.group", Action: "update", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_AddGroupMember_FullMethodName: {Resource: "tenant.group", Action: "add-member", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_GetGroupMember_FullMethodName: {Resource: "tenant.group", Action: "read-member", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_RemoveGroupMember_FullMethodName: {Resource: "tenant.group", Action: "remove-member", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListGroupMembers_FullMethodName: {Resource: "tenant.group", Action: "list-members", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListGroups_FullMethodName: {Resource: "tenant.group", Action: "list", Scope: platformauthz.ScopePrincipal},
			tenantv1.TenantService_GetQuota_FullMethodName: {Resource: "tenant.quota", Action: "read", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ListQuotas_FullMethodName: {Resource: "tenant.quota", Action: "list", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_SetQuota_FullMethodName: {Resource: "tenant.quota", Action: "update", Scope: platformauthz.ScopePrincipal}, tenantv1.TenantService_ConsumeQuota_FullMethodName: {Resource: "tenant.quota", Action: "consume", Scope: platformauthz.ScopePrincipal},
		}
		requirement, ok := requirements[method]
		return requirement, ok
	}
}

type tenantServer struct {
	tenantv1.UnimplementedTenantServiceServer
	service  *tenantdomain.Service
	identity identityclient.Directory
}

func (s *tenantServer) CreateTenant(ctx context.Context, request *tenantv1.CreateTenantRequest) (*tenantv1.CreateTenantResponse, error) {
	created, owner, err := s.service.Create(ctx, request.GetCode(), request.GetName(), request.GetOwnerUserId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.CreateTenantResponse{Tenant: toProtoTenant(created), OwnerMembership: toProtoMembership(owner)}, nil
}
func (s *tenantServer) GetTenant(ctx context.Context, request *tenantv1.GetTenantRequest) (*tenantv1.GetTenantResponse, error) {
	value, err := s.service.Get(ctx, request.GetTenantId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.GetTenantResponse{Tenant: toProtoTenant(value)}, nil
}
func (s *tenantServer) ListTenants(ctx context.Context, request *tenantv1.ListTenantsRequest) (*tenantv1.ListTenantsResponse, error) {
	pageNumber, pageSize := 1, 20
	if request.GetPage() != nil {
		pageNumber, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	statusFilter := tenantStatusString(request.GetStatus())
	if request.GetStatus() != tenantv1.TenantStatus_TENANT_STATUS_UNSPECIFIED && statusFilter == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant status")
	}
	page, err := s.service.ListTenants(ctx, request.GetKeyword(), statusFilter, pageNumber, pageSize)
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Tenant, 0, len(page.Tenants))
	for _, value := range page.Tenants {
		items = append(items, toProtoTenant(value))
	}
	return &tenantv1.ListTenantsResponse{
		Tenants: items,
		Page:    &commonv1.PageResult{Total: uint64(page.Total), Page: uint32(page.Page), PageSize: uint32(page.PageSize)},
	}, nil
}
func (s *tenantServer) UpdateTenant(ctx context.Context, request *tenantv1.UpdateTenantRequest) (*tenantv1.UpdateTenantResponse, error) {
	value, err := s.service.Update(ctx, request.GetTenantId(), request.GetName(), tenantStatusString(request.GetStatus()), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.UpdateTenantResponse{Tenant: toProtoTenant(value)}, nil
}
func (s *tenantServer) AddMembership(ctx context.Context, request *tenantv1.AddMembershipRequest) (*tenantv1.AddMembershipResponse, error) {
	value, err := s.service.AddMembership(ctx, request.GetTenantId(), request.GetUserId(), request.GetPrimaryOrganizationUnitId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.AddMembershipResponse{Membership: toProtoMembership(value)}, nil
}
func (s *tenantServer) UpdateMembership(ctx context.Context, request *tenantv1.UpdateMembershipRequest) (*tenantv1.UpdateMembershipResponse, error) {
	value, err := s.service.UpdateMembership(ctx, request.GetMembershipId(), membershipStatusString(request.GetStatus()), request.GetPrimaryOrganizationUnitId(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.UpdateMembershipResponse{Membership: toProtoMembership(value)}, nil
}
func (s *tenantServer) ListMemberships(ctx context.Context, request *tenantv1.ListMembershipsRequest) (*tenantv1.ListMembershipsResponse, error) {
	pageNumber, pageSize := 0, 0
	if request.GetPage() != nil {
		pageNumber, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	page, err := s.service.ListMemberships(ctx, request.GetTenantId(), request.GetUserId(), membershipStatusString(request.GetStatus()), pageNumber, pageSize)
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Membership, 0, len(page.Memberships))
	for _, value := range page.Memberships {
		items = append(items, toProtoMembership(value))
	}
	return &tenantv1.ListMembershipsResponse{
		Memberships: items,
		Page:        &commonv1.PageResult{Total: uint64(page.Total), Page: uint32(page.Page), PageSize: uint32(page.PageSize)},
	}, nil
}
func (s *tenantServer) SearchMembershipDirectory(ctx context.Context, request *tenantv1.SearchMembershipDirectoryRequest) (*tenantv1.SearchMembershipDirectoryResponse, error) {
	limit := int(request.GetLimit())
	if limit == 0 {
		limit = 20
	}
	entries, err := membershipdirectory.Search(ctx, s.service, s.identity, request.GetTenantId(), request.GetKeyword(), limit)
	if err != nil {
		if errors.Is(err, membershipdirectory.ErrUnavailable) {
			return nil, status.Error(codes.Unavailable, "identity directory is unavailable")
		}
		return nil, grpcError(err)
	}
	result := make([]*tenantv1.MembershipDirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, &tenantv1.MembershipDirectoryEntry{
			Membership: toProtoMembership(entry.Membership), Username: entry.User.Username, DisplayName: entry.User.DisplayName,
		})
	}
	return &tenantv1.SearchMembershipDirectoryResponse{Entries: result}, nil
}
func (s *tenantServer) CreateOrganizationUnit(ctx context.Context, request *tenantv1.CreateOrganizationUnitRequest) (*tenantv1.CreateOrganizationUnitResponse, error) {
	value, err := s.service.CreateOrganizationUnit(ctx, request.GetTenantId(), request.GetParentId(), request.GetCode(), request.GetName())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.CreateOrganizationUnitResponse{OrganizationUnit: toProtoOrganizationUnit(value)}, nil
}
func (s *tenantServer) GetOrganizationUnit(ctx context.Context, request *tenantv1.GetOrganizationUnitRequest) (*tenantv1.GetOrganizationUnitResponse, error) {
	value, err := s.service.GetOrganizationUnit(ctx, request.GetOrganizationUnitId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.GetOrganizationUnitResponse{OrganizationUnit: toProtoOrganizationUnit(value)}, nil
}
func (s *tenantServer) UpdateOrganizationUnit(ctx context.Context, request *tenantv1.UpdateOrganizationUnitRequest) (*tenantv1.UpdateOrganizationUnitResponse, error) {
	value, err := s.service.UpdateOrganizationUnit(ctx, request.GetOrganizationUnitId(), request.GetParentId(), request.GetName(), request.GetStatus(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.UpdateOrganizationUnitResponse{OrganizationUnit: toProtoOrganizationUnit(value)}, nil
}
func (s *tenantServer) ListOrganizationUnits(ctx context.Context, request *tenantv1.ListOrganizationUnitsRequest) (*tenantv1.ListOrganizationUnitsResponse, error) {
	values, err := s.service.ListOrganizationUnits(ctx, request.GetTenantId())
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.OrganizationUnit, 0, len(values))
	for _, value := range values {
		items = append(items, toProtoOrganizationUnit(value))
	}
	return &tenantv1.ListOrganizationUnitsResponse{OrganizationUnits: items}, nil
}
func (s *tenantServer) CreateInvitation(ctx context.Context, request *tenantv1.CreateInvitationRequest) (*tenantv1.CreateInvitationResponse, error) {
	value, token, err := s.service.CreateInvitation(ctx, request.GetTenantId(), request.GetEmail(), time.Duration(request.GetExpiresInSeconds())*time.Second)
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.CreateInvitationResponse{Invitation: toProtoInvitation(value), Token: token}, nil
}
func (s *tenantServer) AcceptInvitation(ctx context.Context, request *tenantv1.AcceptInvitationRequest) (*tenantv1.AcceptInvitationResponse, error) {
	value, membership, err := s.service.AcceptInvitation(ctx, request.GetToken(), request.GetUserId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.AcceptInvitationResponse{Invitation: toProtoInvitation(value), Membership: toProtoMembership(membership)}, nil
}
func (s *tenantServer) RevokeInvitation(ctx context.Context, request *tenantv1.RevokeInvitationRequest) (*tenantv1.RevokeInvitationResponse, error) {
	value, err := s.service.RevokeInvitation(ctx, request.GetInvitationId(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.RevokeInvitationResponse{Invitation: toProtoInvitation(value)}, nil
}
func (s *tenantServer) GetInvitation(ctx context.Context, request *tenantv1.GetInvitationRequest) (*tenantv1.GetInvitationResponse, error) {
	value, err := s.service.GetInvitation(ctx, request.GetInvitationId())
	if err != nil {
		return nil, err
	}
	return &tenantv1.GetInvitationResponse{Invitation: toProtoInvitation(value)}, nil
}
func (s *tenantServer) ListInvitations(ctx context.Context, request *tenantv1.ListInvitationsRequest) (*tenantv1.ListInvitationsResponse, error) {
	pageNumber, pageSize := 0, 0
	if request.GetPage() != nil {
		pageNumber, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	page, err := s.service.ListInvitations(ctx, request.GetTenantId(), pageNumber, pageSize)
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Invitation, 0, len(page.Invitations))
	for _, value := range page.Invitations {
		items = append(items, toProtoInvitation(value))
	}
	return &tenantv1.ListInvitationsResponse{Invitations: items, Page: &commonv1.PageResult{Total: uint64(page.Total), Page: uint32(page.Page), PageSize: uint32(page.PageSize)}}, nil
}
func (s *tenantServer) CreateGroup(ctx context.Context, request *tenantv1.CreateGroupRequest) (*tenantv1.CreateGroupResponse, error) {
	value, err := s.service.CreateGroup(ctx, request.GetTenantId(), request.GetCode(), request.GetName())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.CreateGroupResponse{Group: toProtoGroup(value)}, nil
}
func (s *tenantServer) UpdateGroup(ctx context.Context, request *tenantv1.UpdateGroupRequest) (*tenantv1.UpdateGroupResponse, error) {
	value, err := s.service.UpdateGroup(ctx, request.GetGroupId(), request.GetName(), request.GetStatus(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.UpdateGroupResponse{Group: toProtoGroup(value)}, nil
}
func (s *tenantServer) GetGroup(ctx context.Context, request *tenantv1.GetGroupRequest) (*tenantv1.GetGroupResponse, error) {
	value, err := s.service.GetGroup(ctx, request.GetGroupId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.GetGroupResponse{Group: toProtoGroup(value)}, nil
}
func (s *tenantServer) AddGroupMember(ctx context.Context, request *tenantv1.AddGroupMemberRequest) (*tenantv1.AddGroupMemberResponse, error) {
	if err := s.service.AddGroupMember(ctx, request.GetGroupId(), request.GetMembershipId()); err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.AddGroupMemberResponse{Added: true}, nil
}
func (s *tenantServer) RemoveGroupMember(ctx context.Context, request *tenantv1.RemoveGroupMemberRequest) (*tenantv1.RemoveGroupMemberResponse, error) {
	if err := s.service.RemoveGroupMember(ctx, request.GetGroupId(), request.GetMembershipId(), request.GetExpectedVersion()); err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.RemoveGroupMemberResponse{Removed: true}, nil
}
func (s *tenantServer) GetGroupMember(ctx context.Context, request *tenantv1.GetGroupMemberRequest) (*tenantv1.GetGroupMemberResponse, error) {
	value, err := s.service.GetGroupMember(ctx, request.GetGroupId(), request.GetMembershipId())
	if err != nil {
		return nil, err
	}
	return &tenantv1.GetGroupMemberResponse{GroupMember: toProtoGroupMember(value)}, nil
}
func (s *tenantServer) ListGroupMembers(ctx context.Context, request *tenantv1.ListGroupMembersRequest) (*tenantv1.ListGroupMembersResponse, error) {
	values, err := s.service.ListGroupMembers(ctx, request.GetGroupId())
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.GroupMember, 0, len(values))
	for _, value := range values {
		items = append(items, toProtoGroupMember(value))
	}
	return &tenantv1.ListGroupMembersResponse{GroupMembers: items}, nil
}
func (s *tenantServer) ListGroups(ctx context.Context, request *tenantv1.ListGroupsRequest) (*tenantv1.ListGroupsResponse, error) {
	values, err := s.service.ListGroups(ctx, request.GetTenantId())
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Group, 0, len(values))
	for _, value := range values {
		items = append(items, toProtoGroup(value))
	}
	return &tenantv1.ListGroupsResponse{Groups: items}, nil
}
func (s *tenantServer) GetQuota(ctx context.Context, request *tenantv1.GetQuotaRequest) (*tenantv1.GetQuotaResponse, error) {
	value, err := s.service.GetQuota(ctx, request.GetTenantId(), request.GetKey())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.GetQuotaResponse{Quota: toProtoQuota(value)}, nil
}
func (s *tenantServer) ListQuotas(ctx context.Context, request *tenantv1.ListQuotasRequest) (*tenantv1.ListQuotasResponse, error) {
	pageNumber, pageSize := 0, 0
	if request.GetPage() != nil {
		pageNumber, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	page, err := s.service.ListQuotas(ctx, request.GetTenantId(), request.GetKeyword(), pageNumber, pageSize)
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Quota, 0, len(page.Quotas))
	for _, value := range page.Quotas {
		items = append(items, toProtoQuota(value))
	}
	return &tenantv1.ListQuotasResponse{
		Quotas: items,
		Page:   &commonv1.PageResult{Total: uint64(page.Total), Page: uint32(page.Page), PageSize: uint32(page.PageSize)},
	}, nil
}
func (s *tenantServer) SetQuota(ctx context.Context, request *tenantv1.SetQuotaRequest) (*tenantv1.SetQuotaResponse, error) {
	value, err := s.service.SetQuota(ctx, request.GetTenantId(), request.GetKey(), request.GetLimit(), request.GetExpectedVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.SetQuotaResponse{Quota: toProtoQuota(value)}, nil
}
func (s *tenantServer) ConsumeQuota(ctx context.Context, request *tenantv1.ConsumeQuotaRequest) (*tenantv1.ConsumeQuotaResponse, error) {
	value, allowed, err := s.service.ConsumeQuota(ctx, request.GetTenantId(), request.GetKey(), request.GetAmount())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.ConsumeQuotaResponse{Quota: toProtoQuota(value), Allowed: allowed}, nil
}
func (s *tenantServer) ValidateMembership(ctx context.Context, request *tenantv1.ValidateMembershipRequest) (*tenantv1.ValidateMembershipResponse, error) {
	tenantValue, membership, valid := s.service.ValidateMembership(ctx, request.GetUserId(), request.GetTenantId())
	response := &tenantv1.ValidateMembershipResponse{Valid: valid}
	if valid {
		response.Tenant = toProtoTenant(tenantValue)
		response.Membership = toProtoMembership(membership)
	}
	return response, nil
}
func (s *tenantServer) GetMembership(ctx context.Context, request *tenantv1.GetMembershipRequest) (*tenantv1.GetMembershipResponse, error) {
	value, err := s.service.GetMembership(ctx, request.GetMembershipId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.GetMembershipResponse{Membership: toProtoMembership(value)}, nil
}
func (s *tenantServer) ListUserTenants(ctx context.Context, request *tenantv1.ListUserTenantsRequest) (*tenantv1.ListUserTenantsResponse, error) {
	pageNumber, pageSize := 0, 0
	if request.GetPage() != nil {
		pageNumber, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	page, err := s.service.ListUserTenants(ctx, request.GetUserId(), pageNumber, pageSize)
	if err != nil {
		return nil, grpcError(err)
	}
	items := make([]*tenantv1.Tenant, 0, len(page.Tenants))
	for _, item := range page.Tenants {
		items = append(items, toProtoTenant(item))
	}
	return &tenantv1.ListUserTenantsResponse{Tenants: items, Page: &commonv1.PageResult{Total: uint64(page.Total), Page: uint32(page.Page), PageSize: uint32(page.PageSize)}}, nil
}
func (s *tenantServer) ResolveOrganizationScope(ctx context.Context, request *tenantv1.ResolveOrganizationScopeRequest) (*tenantv1.ResolveOrganizationScopeResponse, error) {
	ids, err := s.service.ResolveOrganizationScope(ctx, request.GetMembershipId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &tenantv1.ResolveOrganizationScopeResponse{OrganizationUnitIds: ids}, nil
}

func toProtoTenant(value tenantdomain.Tenant) *tenantv1.Tenant {
	return &tenantv1.Tenant{Id: value.ID, Code: value.Code, Name: value.Name, Status: tenantStatusProto(value.Status), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoMembership(value tenantdomain.Membership) *tenantv1.Membership {
	return &tenantv1.Membership{Id: value.ID, TenantId: value.TenantID, UserId: value.UserID, Status: membershipStatusProto(value.Status), PrimaryOrganizationUnitId: value.PrimaryOrganizationUnitID, JoinedAt: timestamppb.New(value.JoinedAt), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoOrganizationUnit(value tenantdomain.OrganizationUnit) *tenantv1.OrganizationUnit {
	return &tenantv1.OrganizationUnit{Id: value.ID, TenantId: value.TenantID, ParentId: value.ParentID, Code: value.Code, Name: value.Name, Path: value.Path, Status: value.Status, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoInvitation(value tenantdomain.Invitation) *tenantv1.Invitation {
	return &tenantv1.Invitation{Id: value.ID, TenantId: value.TenantID, Email: value.Email, Status: value.Status, ExpiresAt: timestamppb.New(value.ExpiresAt), AcceptedByUserId: value.AcceptedByUserID, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoGroup(value tenantdomain.Group) *tenantv1.Group {
	return &tenantv1.Group{Id: value.ID, TenantId: value.TenantID, Code: value.Code, Name: value.Name, Status: value.Status, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoQuota(value tenantdomain.Quota) *tenantv1.Quota {
	return &tenantv1.Quota{TenantId: value.TenantID, Key: value.Key, Limit: value.Limit, Used: value.Used, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func toProtoGroupMember(value tenantdomain.GroupMember) *tenantv1.GroupMember {
	return &tenantv1.GroupMember{Id: value.ID, TenantId: value.TenantID, GroupId: value.GroupID, MembershipId: value.MembershipID, Status: value.Status, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func tenantStatusString(value tenantv1.TenantStatus) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "TENANT_STATUS_"))
}
func membershipStatusString(value tenantv1.MembershipStatus) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "MEMBERSHIP_STATUS_"))
}
func tenantStatusProto(value string) tenantv1.TenantStatus {
	return tenantv1.TenantStatus(tenantv1.TenantStatus_value["TENANT_STATUS_"+strings.ToUpper(value)])
}
func membershipStatusProto(value string) tenantv1.MembershipStatus {
	return tenantv1.MembershipStatus(tenantv1.MembershipStatus_value["MEMBERSHIP_STATUS_"+strings.ToUpper(value)])
}

func (s *Server) start(enabled bool) func(context.Context) error {
	return func(context.Context) error {
		if !enabled {
			s.logger.Warn("grpc server is disabled")
			return nil
		}
		listener, err := net.Listen("tcp", s.address)
		if err != nil {
			return fmt.Errorf("listen grpc: %w", err)
		}
		go func() {
			if err := s.server.Serve(listener); err != nil {
				s.logger.Error("grpc server stopped unexpectedly", "error", err)
			}
		}()
		s.logger.Info("grpc server started", "address", s.address)
		return nil
	}
}
func (s *Server) stop(ctx context.Context) error {
	stopped := make(chan struct{})
	go func() { s.server.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		s.server.Stop()
		return ctx.Err()
	}
}

type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	health *apphealth.Service
}

func grpcError(err error) error {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal server error")
	}
	code := codes.Internal
	switch appErr.Code {
	case apperror.CodeInvalidArgument:
		code = codes.InvalidArgument
	case apperror.CodeNotFound:
		code = codes.NotFound
	case apperror.CodeConflict:
		code = codes.Aborted
	case apperror.CodeDependencyUnavailable:
		code = codes.Unavailable
	}
	return status.Error(code, appErr.Message)
}

func (s *healthServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	_, ready := s.health.Ready(ctx)
	serving := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if ready {
		serving = grpc_health_v1.HealthCheckResponse_SERVING
	}
	return &grpc_health_v1.HealthCheckResponse{Status: serving}, nil
}
func (s *healthServer) List(context.Context, *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{Statuses: map[string]*grpc_health_v1.HealthCheckResponse{"": {Status: grpc_health_v1.HealthCheckResponse_SERVING}}}, nil
}

func requestIDInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id := ""
	if values := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(values) > 0 && requestid.Valid(values[0]) {
		id = values[0]
	}
	if id == "" {
		id = requestid.Generate()
	}
	header := metadata.Pairs("x-request-id", id)
	_ = grpc.SetHeader(ctx, header)
	return handler(requestid.WithContext(ctx, id), req)
}
func idempotencyInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	values := metadata.ValueFromIncomingContext(ctx, "idempotency-key")
	if len(values) == 0 {
		return handler(ctx, req)
	}
	if !idempotency.Valid(values[0]) {
		return nil, status.Error(codes.InvalidArgument, "invalid idempotency-key")
	}
	return handler(idempotency.WithContext(ctx, values[0]), req)
}
func environmentInterceptor(profile string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(environment.WithContext(ctx, profile), req)
	}
}
func authInterceptor(service *auth.Service, cfg config.Auth) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		authCtx, err := authenticateGRPC(ctx, info.FullMethod, service, cfg)
		if err != nil {
			return nil, err
		}
		return handler(authCtx, req)
	}
}

func authenticateGRPC(ctx context.Context, method string, service *auth.Service, cfg config.Auth) (context.Context, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if cfg.PSK.Enabled && auth.MatchesAny(method, cfg.PSK.GRPCMethods) {
		if len(values) == 0 || !auth.VerifyPSK(values[0], cfg.PSK.Key) {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid PSK")
		}
		return principal.WithContext(ctx, principal.Principal{ID: "psk", Type: principal.TypeServiceAccount}), nil
	}
	if auth.MatchesAny(method, cfg.SkipGRPCMethods) {
		return ctx, nil
	}
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	scheme, raw, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	caller, err := service.Verify(ctx, raw)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}
	return principal.WithContext(ctx, caller), nil
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

func environmentStreamInterceptor(profile string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: environment.WithContext(stream.Context(), profile)})
	}
}

func requestIDStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := stream.Context()
	id := ""
	if values := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(values) > 0 && requestid.Valid(values[0]) {
		id = values[0]
	}
	if id == "" {
		id = requestid.Generate()
	}
	if err := stream.SetHeader(metadata.Pairs("x-request-id", id)); err != nil {
		return status.Error(codes.Internal, "set request metadata")
	}
	return handler(srv, &contextServerStream{ServerStream: stream, ctx: requestid.WithContext(ctx, id)})
}

func idempotencyStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	values := metadata.ValueFromIncomingContext(stream.Context(), "idempotency-key")
	if len(values) == 0 {
		return handler(srv, stream)
	}
	if !idempotency.Valid(values[0]) {
		return status.Error(codes.InvalidArgument, "invalid idempotency-key")
	}
	return handler(srv, &contextServerStream{ServerStream: stream, ctx: idempotency.WithContext(stream.Context(), values[0])})
}

func authStreamInterceptor(service *auth.Service, cfg config.Auth) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authenticateGRPC(stream.Context(), info.FullMethod, service, cfg)
		if err != nil {
			return err
		}
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: ctx})
	}
}

func recoveryStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(stream.Context(), "grpc stream panic recovered", "method", info.FullMethod, "panic", recovered)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, stream)
	}
}

func metricsStreamInterceptor(metrics *observability.Metrics, logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		started := time.Now()
		err := handler(srv, stream)
		code := status.Code(err)
		if metrics.Enabled() {
			metrics.GRPCRequests.WithLabelValues(info.FullMethod, code.String()).Inc()
			metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		}
		requestID, _ := requestid.FromContext(stream.Context())
		logger.InfoContext(stream.Context(), "grpc stream", "request_id", requestID, "method", info.FullMethod, "code", code.String(), "duration", time.Since(started))
		return err
	}
}

func recoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(ctx, "grpc panic recovered", "method", info.FullMethod, "panic", recovered)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
func metricsInterceptor(metrics *observability.Metrics, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		response, err := handler(ctx, req)
		code := status.Code(err)
		if metrics.Enabled() {
			metrics.GRPCRequests.WithLabelValues(info.FullMethod, code.String()).Inc()
			metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		}
		span := trace.SpanFromContext(ctx).SpanContext()
		requestID, _ := requestid.FromContext(ctx)
		logger.InfoContext(ctx, "grpc request", "request_id", requestID, "trace_id", span.TraceID().String(), "span_id", span.SpanID().String(), "method", info.FullMethod, "code", code.String(), "duration", time.Since(started))
		return response, err
	}
}

func serverCredentials(cfg config.GRPCTLS) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load grpc certificate: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	if cfg.ClientCAFile != "" {
		pem, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read grpc client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse grpc client CA")
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(tlsConfig), nil
}

var Module = fx.Module("grpc", fx.Provide(NewServer), fx.Invoke(func(*Server) {}))
