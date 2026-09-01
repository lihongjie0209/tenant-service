package identityclient

import (
	"context"
	"errors"
	"time"

	identityv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/identity/v1"
	"github.com/lihongjie0209/tenant-service/internal/outbound"
)

var ErrUnavailable = errors.New("identity tenant-token issuer is unavailable")

type Issuer interface {
	IssueTenantToken(context.Context, string, string, string, string) (string, time.Time, error)
}

type Client struct {
	client identityv1.IdentityServiceClient
}

func New(registry *outbound.Registry) (*Client, error) {
	connection, ok := registry.GRPC("identity")
	if !ok {
		return nil, ErrUnavailable
	}
	return &Client{client: identityv1.NewIdentityServiceClient(connection)}, nil
}

func (c *Client) IssueTenantToken(ctx context.Context, userID, tenantID, membershipID, sessionID string) (string, time.Time, error) {
	response, err := c.client.IssueTenantToken(ctx, &identityv1.IssueTenantTokenRequest{
		UserId: userID, TenantId: tenantID, MembershipId: membershipID, SessionId: sessionID,
	})
	if err != nil {
		return "", time.Time{}, errors.Join(ErrUnavailable, err)
	}
	if response.GetAccessToken() == "" || response.GetExpiresAt() == nil || !response.GetExpiresAt().IsValid() {
		return "", time.Time{}, ErrUnavailable
	}
	return response.GetAccessToken(), response.GetExpiresAt().AsTime(), nil
}
