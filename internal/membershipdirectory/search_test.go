package membershipdirectory

import (
	"context"
	"testing"

	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	"github.com/lihongjie0209/tenant-service/internal/tenant"
)

type fakeIdentities struct {
	pages map[int]identityclient.UserPage
}

func (f fakeIdentities) ListUsers(_ context.Context, _ string, page, _ int) (identityclient.UserPage, error) {
	return f.pages[page], nil
}

type fakeMemberships struct {
	active map[string]tenant.Membership
}

func (f fakeMemberships) FindMembershipsByUserIDs(_ context.Context, tenantID string, userIDs []string, status string) ([]tenant.Membership, error) {
	if tenantID != "tenant-1" || status != "active" {
		return nil, nil
	}
	result := make([]tenant.Membership, 0, len(userIDs))
	for _, userID := range userIDs {
		if value, ok := f.active[userID]; ok {
			result = append(result, value)
		}
	}
	return result, nil
}

func TestSearchReturnsOnlyActiveTenantMembersInIdentityOrder(t *testing.T) {
	t.Parallel()
	users := make([]identityclient.User, 100)
	for index := range users {
		users[index] = identityclient.User{ID: "not-member"}
	}
	users[99] = identityclient.User{ID: "user-1", Username: "alice"}
	identities := fakeIdentities{pages: map[int]identityclient.UserPage{
		1: {Users: users, Total: 101},
		2: {Users: []identityclient.User{{ID: "user-2", Username: "bob"}}, Total: 101},
	}}
	memberships := fakeMemberships{active: map[string]tenant.Membership{
		"user-1": {ID: "membership-1", TenantID: "tenant-1", UserID: "user-1", Status: "active"},
		"user-2": {ID: "membership-2", TenantID: "tenant-1", UserID: "user-2", Status: "active"},
	}}

	entries, err := Search(t.Context(), memberships, identities, "tenant-1", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].User.ID != "user-1" || entries[1].User.ID != "user-2" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestSearchRejectsUnboundedLimit(t *testing.T) {
	t.Parallel()
	if _, err := Search(t.Context(), fakeMemberships{}, fakeIdentities{}, "tenant-1", "", 51); err == nil {
		t.Fatal("Search() accepted an unbounded limit")
	}
}
