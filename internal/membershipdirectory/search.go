package membershipdirectory

import (
	"context"
	"errors"

	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	"github.com/lihongjie0209/tenant-service/internal/tenant"
)

var ErrUnavailable = errors.New("membership directory is unavailable")

type MembershipFinder interface {
	FindMembershipsByUserIDs(context.Context, string, []string, string) ([]tenant.Membership, error)
}

type Entry struct {
	Membership tenant.Membership
	User       identityclient.User
}

func Search(ctx context.Context, memberships MembershipFinder, identities identityclient.Directory, tenantID, keyword string, limit int) ([]Entry, error) {
	if memberships == nil || identities == nil {
		return nil, ErrUnavailable
	}
	if tenantID == "" || limit < 1 || limit > 50 {
		return nil, apperror.Invalid("tenant ID and a limit from 1 to 50 are required", nil)
	}
	const candidatePageSize = 100
	const maxCandidatePages = 5
	entries := make([]Entry, 0, limit)
	for page := 1; page <= maxCandidatePages && len(entries) < limit; page++ {
		users, err := identities.ListUsers(ctx, keyword, page, candidatePageSize)
		if err != nil {
			return nil, errors.Join(ErrUnavailable, err)
		}
		userIDs := make([]string, 0, len(users.Users))
		for _, user := range users.Users {
			userIDs = append(userIDs, user.ID)
		}
		values, err := memberships.FindMembershipsByUserIDs(ctx, tenantID, userIDs, "active")
		if err != nil {
			return nil, err
		}
		byUserID := make(map[string]tenant.Membership, len(values))
		for _, value := range values {
			byUserID[value.UserID] = value
		}
		for _, user := range users.Users {
			membership, ok := byUserID[user.ID]
			if !ok {
				continue
			}
			entries = append(entries, Entry{Membership: membership, User: user})
			if len(entries) == limit {
				break
			}
		}
		if len(users.Users) < candidatePageSize || uint64(page*candidatePageSize) >= users.Total {
			break
		}
	}
	return entries, nil
}
