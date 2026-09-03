package grpctransport

import (
	"testing"
	"time"

	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	tenantv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/tenant/v1"
	"github.com/lihongjie0209/tenant-service/internal/auth"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTenantGRPCRequirementCoverageAndInternalExclusions(t *testing.T) {
	t.Parallel()
	resolve := tenantGRPCRequirement(true)
	protected := []string{
		tenantv1.TenantService_GetTenant_FullMethodName, tenantv1.TenantService_ListTenants_FullMethodName, tenantv1.TenantService_UpdateTenant_FullMethodName,
		tenantv1.TenantService_AddMembership_FullMethodName, tenantv1.TenantService_GetMembership_FullMethodName, tenantv1.TenantService_UpdateMembership_FullMethodName, tenantv1.TenantService_ListMemberships_FullMethodName,
		tenantv1.TenantService_CreateOrganizationUnit_FullMethodName, tenantv1.TenantService_GetOrganizationUnit_FullMethodName, tenantv1.TenantService_UpdateOrganizationUnit_FullMethodName, tenantv1.TenantService_ListOrganizationUnits_FullMethodName,
		tenantv1.TenantService_CreateInvitation_FullMethodName, tenantv1.TenantService_GetInvitation_FullMethodName, tenantv1.TenantService_RevokeInvitation_FullMethodName, tenantv1.TenantService_ListInvitations_FullMethodName,
		tenantv1.TenantService_CreateGroup_FullMethodName, tenantv1.TenantService_GetGroup_FullMethodName, tenantv1.TenantService_UpdateGroup_FullMethodName, tenantv1.TenantService_AddGroupMember_FullMethodName, tenantv1.TenantService_GetGroupMember_FullMethodName, tenantv1.TenantService_RemoveGroupMember_FullMethodName, tenantv1.TenantService_ListGroupMembers_FullMethodName, tenantv1.TenantService_ListGroups_FullMethodName,
		tenantv1.TenantService_GetQuota_FullMethodName, tenantv1.TenantService_ListQuotas_FullMethodName, tenantv1.TenantService_SetQuota_FullMethodName, tenantv1.TenantService_ConsumeQuota_FullMethodName,
	}
	for _, method := range protected {
		if requirement, ok := resolve(method); !ok || requirement.Resource == "" || requirement.Action == "" {
			t.Fatalf("method %q requirement = %+v, %v", method, requirement, ok)
		}
	}
	excluded := []string{tenantv1.TenantService_CreateTenant_FullMethodName, tenantv1.TenantService_AcceptInvitation_FullMethodName, tenantv1.TenantService_ValidateMembership_FullMethodName, tenantv1.TenantService_ListUserTenants_FullMethodName, tenantv1.TenantService_ResolveOrganizationScope_FullMethodName}
	for _, method := range excluded {
		if _, ok := resolve(method); ok {
			t.Fatalf("self-service/internal fact method %q must not recurse through tenant authorization", method)
		}
	}
	if _, ok := tenantGRPCRequirement(false)(protected[0]); ok {
		t.Fatal("disabled authorization must not call the decision service")
	}
}

func TestTenantGRPCRequirementSeparatesPlatformDirectoryFromTenantResources(t *testing.T) {
	t.Parallel()
	resolve := tenantGRPCRequirement(true)
	for _, method := range []string{tenantv1.TenantService_GetTenant_FullMethodName, tenantv1.TenantService_ListTenants_FullMethodName, tenantv1.TenantService_UpdateTenant_FullMethodName} {
		requirement, _ := resolve(method)
		if requirement.Scope != platformauthz.ScopePlatform {
			t.Fatalf("method %q scope = %v, want platform", method, requirement.Scope)
		}
	}
	for _, method := range []string{tenantv1.TenantService_ListMemberships_FullMethodName, tenantv1.TenantService_ListOrganizationUnits_FullMethodName, tenantv1.TenantService_ListGroups_FullMethodName} {
		requirement, _ := resolve(method)
		if requirement.Scope != platformauthz.ScopePrincipal {
			t.Fatalf("method %q scope = %v, want principal-derived", method, requirement.Scope)
		}
	}
}

func TestAuthenticateGRPC_PSKWildcard(t *testing.T) {
	t.Parallel()
	const key = "01234567890123456789012345678901"
	authService := auth.New(config.Config{JWT: config.JWT{Issuer: "test", Secret: key, TTL: time.Hour}})
	cfg := config.Auth{
		SkipGRPCMethods: []string{"/hello.v1.UserService/*"},
		PSK:             config.PSK{Enabled: true, Key: key, GRPCMethods: []string{"/hello.v1.UserService/*"}},
	}
	for _, test := range []struct {
		name   string
		header string
		code   codes.Code
	}{
		{name: "valid", header: "PSK " + key, code: codes.OK},
		{name: "PSK precedes skip", code: codes.Unauthenticated},
		{name: "bearer rejected", header: "Bearer " + key, code: codes.Unauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", test.header))
			_, err := authenticateGRPC(ctx, "/hello.v1.UserService/GetUser", authService, cfg)
			if got := status.Code(err); got != test.code {
				t.Fatalf("status code = %s, want %s", got, test.code)
			}
		})
	}
}
