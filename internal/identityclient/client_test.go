package identityclient

import (
	"context"
	"net"
	"testing"
	"time"

	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	identityv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/identity/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type identityServerStub struct {
	identityv1.UnimplementedIdentityServiceServer
	request     *identityv1.IssueTenantTokenRequest
	listRequest *identityv1.ListUsersRequest
}

func (s *identityServerStub) ListUsers(_ context.Context, request *identityv1.ListUsersRequest) (*identityv1.ListUsersResponse, error) {
	s.listRequest = request
	return &identityv1.ListUsersResponse{
		Users: []*identityv1.User{{Id: "user-1", Username: "alice", DisplayName: "Alice", Status: identityv1.UserStatus_USER_STATUS_ACTIVE}},
		Page:  &commonv1.PageResult{Total: 1, Page: request.GetPage().GetPage(), PageSize: request.GetPage().GetPageSize()},
	}, nil
}

func (s *identityServerStub) IssueTenantToken(_ context.Context, request *identityv1.IssueTenantTokenRequest) (*identityv1.IssueTenantTokenResponse, error) {
	s.request = request
	return &identityv1.IssueTenantTokenResponse{AccessToken: "scoped-token", ExpiresAt: timestamppb.New(time.Unix(100, 0))}, nil
}

func TestClientIssuesSessionScopedTenantToken(t *testing.T) {
	t.Parallel()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	stub := &identityServerStub{}
	identityv1.RegisterIdentityServiceServer(server, stub)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///bufconn", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	client := &Client{client: identityv1.NewIdentityServiceClient(connection)}
	token, expiresAt, err := client.IssueTenantToken(t.Context(), "user-1", "tenant-1", "membership-1", "session-1")
	if err != nil || token != "scoped-token" || !expiresAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("IssueTenantToken() = (%q, %s, %v)", token, expiresAt, err)
	}
	if stub.request.GetSessionId() != "session-1" || stub.request.GetMembershipId() != "membership-1" {
		t.Fatalf("request = %+v", stub.request)
	}
}

func TestClientListsActiveUsersWithBoundedPage(t *testing.T) {
	t.Parallel()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	stub := &identityServerStub{}
	identityv1.RegisterIdentityServiceServer(server, stub)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///bufconn", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	client := &Client{client: identityv1.NewIdentityServiceClient(connection)}
	page, err := client.ListUsers(t.Context(), "ali", 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 1 || page.Users[0].Username != "alice" || page.Users[0].Status != "active" || page.Total != 1 {
		t.Fatalf("ListUsers() = %+v", page)
	}
	if stub.listRequest.GetKeyword() != "ali" || stub.listRequest.GetStatus() != identityv1.UserStatus_USER_STATUS_ACTIVE || stub.listRequest.GetPage().GetPage() != 2 {
		t.Fatalf("request = %+v", stub.listRequest)
	}
}
