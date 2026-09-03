package identityclient

import (
	"context"
	"errors"
	"time"

	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	identityv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/identity/v1"
	"github.com/lihongjie0209/tenant-service/internal/outbound"
)

var ErrUnavailable = errors.New("identity tenant-token issuer is unavailable")

type Issuer interface {
	IssueTenantToken(context.Context, string, string, string, string) (string, time.Time, error)
}

type Directory interface {
	ListUsers(context.Context, string, int, int) (UserPage, error)
}

type User struct {
	ID          string
	Username    string
	DisplayName string
	Status      string
}

type UserPage struct {
	Users    []User
	Total    uint64
	Page     int
	PageSize int
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

func (c *Client) ListUsers(ctx context.Context, keyword string, page, pageSize int) (UserPage, error) {
	response, err := c.client.ListUsers(ctx, &identityv1.ListUsersRequest{
		Keyword: keyword,
		Status:  identityv1.UserStatus_USER_STATUS_ACTIVE,
		Page:    &commonv1.PageRequest{Page: uint32(page), PageSize: uint32(pageSize)},
	})
	if err != nil {
		return UserPage{}, errors.Join(ErrUnavailable, err)
	}
	users := make([]User, 0, len(response.GetUsers()))
	for _, value := range response.GetUsers() {
		if value == nil {
			continue
		}
		users = append(users, User{
			ID:          value.GetId(),
			Username:    value.GetUsername(),
			DisplayName: value.GetDisplayName(),
			Status:      "active",
		})
	}
	result := response.GetPage()
	if result == nil {
		return UserPage{Users: users, Page: page, PageSize: pageSize}, nil
	}
	return UserPage{Users: users, Total: result.GetTotal(), Page: int(result.GetPage()), PageSize: int(result.GetPageSize())}, nil
}
