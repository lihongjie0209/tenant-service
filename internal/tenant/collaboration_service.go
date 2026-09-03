package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/audit"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	tenantv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/tenant/v1"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) CreateInvitation(ctx context.Context, tenantID, email string, expiresIn time.Duration) (Invitation, string, error) {
	tenantID, email = strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(email))
	address, parseErr := mail.ParseAddress(email)
	if tenantID == "" || parseErr != nil || address.Address != email || expiresIn <= 0 || expiresIn > 30*24*time.Hour {
		return Invitation{}, "", apperror.Invalid("invalid tenant invitation", parseErr)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return Invitation{}, "", err
	}
	if _, err := s.repository.GetTenant(ctx, tenantID); err != nil {
		return Invitation{}, "", translate(err)
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return Invitation{}, "", apperror.Unauthorized("authenticated actor is required")
	}
	token, err := secureToken()
	if err != nil {
		return Invitation{}, "", apperror.Internal(err)
	}
	value := Invitation{ID: uuid.NewString(), TenantID: tenantID, Email: email, TokenHash: hashToken(token), Status: "pending", ExpiresAt: fields.CreatedAt.Add(expiresIn), Version: 1, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	event, err := newOutboxEvent(ctx, "platform.tenant.invitation.changed.v1", "platform.tenant.v1.InvitationChanged", value.ID, tenantID, fields.CreatedAt, &tenantv1.InvitationChangedEvent{Invitation: eventInvitation(value), ChangeType: "created"})
	if err != nil {
		return Invitation{}, "", apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.CreateInvitation(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	if err != nil {
		return Invitation{}, "", translate(err)
	}
	return value, token, nil
}

func (s *Service) AcceptInvitation(ctx context.Context, token, userID string) (Invitation, Membership, error) {
	token, userID = strings.TrimSpace(token), strings.TrimSpace(userID)
	if token == "" || userID == "" {
		return Invitation{}, Membership{}, apperror.Invalid("token and user_id are required", nil)
	}
	actor, err := principal.Require(ctx)
	if err != nil {
		return Invitation{}, Membership{}, apperror.Unauthorized("authenticated actor is required")
	}
	if actor.Type == principal.TypeUser && actor.ID != userID {
		return Invitation{}, Membership{}, apperror.Forbidden("invitation can only be accepted for the authenticated user")
	}
	var accepted Invitation
	var membership Membership
	err = s.withResourceLock(ctx, "tenant:invitation:"+hashToken(token), func() error {
		invitation, getErr := s.repository.GetInvitationByTokenHash(ctx, hashToken(token))
		if getErr != nil {
			return getErr
		}
		if invitation.Status != "pending" || !s.now().Before(invitation.ExpiresAt) {
			return apperror.Conflict("invitation is no longer valid", nil)
		}
		now := s.now()
		invitation.Status, invitation.AcceptedByUserID, invitation.UpdatedAt, invitation.UpdatedBy = "accepted", userID, now, actor.ID
		membership = Membership{ID: uuid.NewString(), TenantID: invitation.TenantID, UserID: userID, Status: "active", JoinedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actor.ID, UpdatedBy: actor.ID}
		eventValue := invitation
		eventValue.Version++
		event, eventErr := newOutboxEvent(ctx, "platform.tenant.invitation.changed.v1", "platform.tenant.v1.InvitationChanged", invitation.ID, invitation.TenantID, now, &tenantv1.InvitationChangedEvent{Invitation: eventInvitation(eventValue), ChangeType: "accepted"})
		if eventErr != nil {
			return eventErr
		}
		if txErr := s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
			if err := s.repository.UpdateInvitation(ctx, tx, invitation); err != nil {
				return err
			}
			if err := s.repository.CreateMembership(ctx, tx, membership); err != nil {
				return err
			}
			return s.repository.AddOutbox(ctx, tx, event)
		}); txErr != nil {
			return txErr
		}
		accepted, getErr = s.repository.GetInvitation(ctx, invitation.ID)
		return getErr
	})
	if err != nil {
		return Invitation{}, Membership{}, translate(err)
	}
	return accepted, membership, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, id string, version int64) (Invitation, error) {
	if id == "" || version < 1 {
		return Invitation{}, apperror.Invalid("invitation_id and version are required", nil)
	}
	value, err := s.repository.GetInvitation(ctx, id)
	if err != nil {
		return Invitation{}, translate(err)
	}
	if err := authorizeTenant(ctx, value.TenantID); err != nil {
		return Invitation{}, err
	}
	if value.Status != "pending" {
		return Invitation{}, apperror.Conflict("only pending invitations can be revoked", nil)
	}
	actor, now, err := audit.UpdatedBy(ctx, s.now())
	if err != nil {
		return Invitation{}, apperror.Unauthorized("authenticated actor is required")
	}
	value.Status, value.Version, value.UpdatedAt, value.UpdatedBy = "revoked", version, now, actor
	eventValue := value
	eventValue.Version++
	event, err := newOutboxEvent(ctx, "platform.tenant.invitation.changed.v1", "platform.tenant.v1.InvitationChanged", value.ID, value.TenantID, now, &tenantv1.InvitationChangedEvent{Invitation: eventInvitation(eventValue), ChangeType: "revoked"})
	if err != nil {
		return Invitation{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.UpdateInvitation(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	if err != nil {
		return Invitation{}, translate(err)
	}
	return s.repository.GetInvitation(ctx, id)
}

func (s *Service) GetInvitation(ctx context.Context, id string) (Invitation, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Invitation{}, apperror.Invalid("invitation_id is required", nil)
	}
	value, err := s.repository.GetInvitation(ctx, id)
	if err != nil {
		return Invitation{}, translate(err)
	}
	if err := authorizeTenant(ctx, value.TenantID); err != nil {
		return Invitation{}, err
	}
	return value, nil
}

func (s *Service) ListInvitations(ctx context.Context, tenantID string, page, pageSize int) (InvitationPage, error) {
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return InvitationPage{}, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return InvitationPage{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.ListInvitations(ctx, tenantID, pageSize, (page-1)*pageSize)
	return InvitationPage{Invitations: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}

func (s *Service) CreateGroup(ctx context.Context, tenantID, code, name string) (Group, error) {
	tenantID, code, name = strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(code)), strings.TrimSpace(name)
	if tenantID == "" || code == "" || name == "" {
		return Group{}, apperror.Invalid("tenant_id, code and name are required", nil)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return Group{}, err
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return Group{}, apperror.Unauthorized("authenticated actor is required")
	}
	value := Group{ID: uuid.NewString(), TenantID: tenantID, Code: code, Name: name, Status: "active", Version: 1, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	event, err := newOutboxEvent(ctx, "platform.tenant.group.changed.v1", "platform.tenant.v1.GroupChanged", value.ID, tenantID, fields.CreatedAt, &tenantv1.GroupChangedEvent{Group: eventGroup(value), ChangeType: "created"})
	if err != nil {
		return Group{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.CreateGroup(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	return value, translate(err)
}
func (s *Service) UpdateGroup(ctx context.Context, id, name, status string, version int64) (Group, error) {
	if id == "" || name == "" || version < 1 || (status != "active" && status != "disabled") {
		return Group{}, apperror.Invalid("invalid group update", nil)
	}
	value, err := s.repository.GetGroup(ctx, id)
	if err != nil {
		return Group{}, translate(err)
	}
	if err := authorizeTenant(ctx, value.TenantID); err != nil {
		return Group{}, err
	}
	actor, now, err := audit.UpdatedBy(ctx, s.now())
	if err != nil {
		return Group{}, apperror.Unauthorized("authenticated actor is required")
	}
	value.Name, value.Status, value.Version, value.UpdatedAt, value.UpdatedBy = name, status, version, now, actor
	eventValue := value
	eventValue.Version++
	event, err := newOutboxEvent(ctx, "platform.tenant.group.changed.v1", "platform.tenant.v1.GroupChanged", id, value.TenantID, now, &tenantv1.GroupChangedEvent{Group: eventGroup(eventValue), ChangeType: "updated"})
	if err != nil {
		return Group{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.UpdateGroup(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	if err != nil {
		return Group{}, translate(err)
	}
	return s.repository.GetGroup(ctx, id)
}
func (s *Service) ListGroups(ctx context.Context, tenantID string) ([]Group, error) {
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	values, err := s.repository.ListGroups(ctx, tenantID)
	return values, translate(err)
}

func (s *Service) SearchGroups(ctx context.Context, tenantID, keyword, status string, page, pageSize int) (GroupPage, error) {
	tenantID, keyword, status = strings.TrimSpace(tenantID), strings.TrimSpace(keyword), strings.TrimSpace(status)
	if tenantID == "" || (status != "" && status != "active" && status != "disabled") {
		return GroupPage{}, apperror.Invalid("invalid group query", nil)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return GroupPage{}, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return GroupPage{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.SearchGroups(ctx, tenantID, keyword, status, pageSize, (page-1)*pageSize)
	return GroupPage{Groups: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}
func (s *Service) AddGroupMember(ctx context.Context, groupID, membershipID string) error {
	group, err := s.repository.GetGroup(ctx, groupID)
	if err != nil {
		return translate(err)
	}
	if err := authorizeTenant(ctx, group.TenantID); err != nil {
		return err
	}
	membership, err := s.repository.GetMembership(ctx, membershipID)
	if err != nil {
		return translate(err)
	}
	if group.TenantID != membership.TenantID || group.Status != "active" || membership.Status != "active" {
		return apperror.Invalid("group and membership must be active in the same tenant", nil)
	}
	existing, existingErr := s.repository.GetGroupMember(ctx, groupID, membershipID)
	if existingErr == nil && existing.Status == "active" {
		return nil
	}
	if existingErr != nil && !errors.Is(existingErr, ErrNotFound) {
		return translate(existingErr)
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return apperror.Unauthorized("authenticated actor is required")
	}
	value := GroupMember{ID: uuid.NewString(), TenantID: group.TenantID, GroupID: groupID, MembershipID: membershipID, Status: "active", Version: 1, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	operation := func(tx *sqlx.Tx) error { return s.repository.CreateGroupMember(ctx, tx, value) }
	if existingErr == nil {
		value = existing
		value.Status, value.UpdatedAt, value.UpdatedBy = "active", fields.UpdatedAt, fields.UpdatedBy
		operation = func(tx *sqlx.Tx) error { return s.repository.UpdateGroupMember(ctx, tx, value) }
	}
	event, err := newOutboxEvent(ctx, "platform.tenant.group.changed.v1", "platform.tenant.v1.GroupChanged", groupID, group.TenantID, fields.CreatedAt, &tenantv1.GroupChangedEvent{Group: eventGroup(group), MembershipId: membershipID, ChangeType: "member_added"})
	if err != nil {
		return apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := operation(tx); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	return translate(err)
}
func (s *Service) ListGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, apperror.Invalid("group_id is required", nil)
	}
	group, err := s.repository.GetGroup(ctx, groupID)
	if err != nil {
		return nil, translate(err)
	}
	if err := authorizeTenant(ctx, group.TenantID); err != nil {
		return nil, err
	}
	values, err := s.repository.ListGroupMembers(ctx, groupID)
	return values, translate(err)
}

func (s *Service) BatchGetGroupMembers(ctx context.Context, groupID string, membershipIDs []string) ([]GroupMember, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, apperror.Invalid("group_id is required", nil)
	}
	group, err := s.repository.GetGroup(ctx, groupID)
	if err != nil {
		return nil, translate(err)
	}
	if err := authorizeTenant(ctx, group.TenantID); err != nil {
		return nil, err
	}
	unique := make([]string, 0, len(membershipIDs))
	seen := make(map[string]struct{}, len(membershipIDs))
	for _, id := range membershipIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, apperror.Invalid("membership_ids must not contain empty values", nil)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []GroupMember{}, nil
	}
	if len(unique) > 100 {
		return nil, apperror.Invalid("membership_ids must not contain more than 100 values", nil)
	}
	values, err := s.repository.BatchGetGroupMembers(ctx, groupID, unique)
	return values, translate(err)
}
func (s *Service) GetGroupMember(ctx context.Context, groupID, membershipID string) (GroupMember, error) {
	groupID, membershipID = strings.TrimSpace(groupID), strings.TrimSpace(membershipID)
	if groupID == "" || membershipID == "" {
		return GroupMember{}, apperror.Invalid("group_id and membership_id are required", nil)
	}
	value, err := s.repository.GetGroupMember(ctx, groupID, membershipID)
	if err != nil {
		return GroupMember{}, translate(err)
	}
	if err := authorizeTenant(ctx, value.TenantID); err != nil {
		return GroupMember{}, err
	}
	return value, nil
}
func (s *Service) RemoveGroupMember(ctx context.Context, groupID, membershipID string, version int64) error {
	value, err := s.repository.GetGroupMember(ctx, groupID, membershipID)
	if err != nil {
		return translate(err)
	}
	if err := authorizeTenant(ctx, value.TenantID); err != nil {
		return err
	}
	actor, now, err := audit.UpdatedBy(ctx, s.now())
	if err != nil {
		return apperror.Unauthorized("authenticated actor is required")
	}
	value.Status, value.Version, value.UpdatedAt, value.UpdatedBy = "removed", version, now, actor
	group, err := s.repository.GetGroup(ctx, groupID)
	if err != nil {
		return translate(err)
	}
	event, err := newOutboxEvent(ctx, "platform.tenant.group.changed.v1", "platform.tenant.v1.GroupChanged", groupID, group.TenantID, now, &tenantv1.GroupChangedEvent{Group: eventGroup(group), MembershipId: membershipID, ChangeType: "member_removed"})
	if err != nil {
		return apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.UpdateGroupMember(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	return translate(err)
}

func (s *Service) GetQuota(ctx context.Context, tenantID, key string) (Quota, error) {
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return Quota{}, err
	}
	value, err := s.repository.GetQuota(ctx, tenantID, key)
	return value, translate(err)
}
func (s *Service) ListQuotas(ctx context.Context, tenantID, keyword string, page, pageSize int) (QuotaPage, error) {
	tenantID, keyword = strings.TrimSpace(tenantID), strings.TrimSpace(keyword)
	if tenantID == "" {
		return QuotaPage{}, apperror.Invalid("tenant_id is required", nil)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return QuotaPage{}, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return QuotaPage{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.ListQuotas(ctx, tenantID, keyword, pageSize, (page-1)*pageSize)
	return QuotaPage{Quotas: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}
func (s *Service) SetQuota(ctx context.Context, tenantID, key string, limit, expectedVersion int64) (Quota, error) {
	if tenantID == "" || key == "" || limit < 0 || expectedVersion < 0 {
		return Quota{}, apperror.Invalid("invalid quota", nil)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return Quota{}, err
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return Quota{}, apperror.Unauthorized("authenticated actor is required")
	}
	current, getErr := s.repository.GetQuota(ctx, tenantID, key)
	create := errors.Is(getErr, ErrNotFound)
	if getErr != nil && !create {
		return Quota{}, translate(getErr)
	}
	if create && expectedVersion != 0 {
		return Quota{}, apperror.StaleVersion(ErrStaleVersion)
	}
	if !create && current.Version != expectedVersion {
		return Quota{}, apperror.StaleVersion(ErrStaleVersion)
	}
	value := current
	if create {
		value = Quota{TenantID: tenantID, Key: key, Used: 0, Version: 1, CreatedAt: fields.CreatedAt, CreatedBy: fields.CreatedBy}
	}
	value.Limit, value.UpdatedAt, value.UpdatedBy = limit, fields.UpdatedAt, fields.UpdatedBy
	eventValue := value
	if !create {
		eventValue.Version++
	}
	event, err := newOutboxEvent(ctx, "platform.tenant.quota.changed.v1", "platform.tenant.v1.QuotaChanged", tenantID+":"+key, tenantID, fields.UpdatedAt, &tenantv1.QuotaChangedEvent{Quota: eventQuota(eventValue), ChangeType: "limit_set"})
	if err != nil {
		return Quota{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if create {
			if err := s.repository.CreateQuota(ctx, tx, value); err != nil {
				return err
			}
		} else if err := s.repository.UpdateQuota(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	if err != nil {
		return Quota{}, translate(err)
	}
	return s.repository.GetQuota(ctx, tenantID, key)
}
func (s *Service) ConsumeQuota(ctx context.Context, tenantID, key string, amount int64) (Quota, bool, error) {
	if tenantID == "" || key == "" || amount <= 0 {
		return Quota{}, false, apperror.Invalid("invalid quota consumption", nil)
	}
	if err := authorizeTenant(ctx, tenantID); err != nil {
		return Quota{}, false, err
	}
	var allowed bool
	var consumed Quota
	err := s.withResourceLock(ctx, "tenant:quota:"+tenantID+":"+key, func() error {
		actor, now, auditErr := audit.UpdatedBy(ctx, s.now())
		if auditErr != nil {
			return auditErr
		}
		return s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
			var consumeErr error
			consumed, allowed, consumeErr = s.repository.ConsumeQuota(ctx, tx, tenantID, key, amount, now, actor)
			if consumeErr != nil || !allowed {
				return consumeErr
			}
			event, eventErr := newOutboxEvent(ctx, "platform.tenant.quota.changed.v1", "platform.tenant.v1.QuotaChanged", tenantID+":"+key, tenantID, now, &tenantv1.QuotaChangedEvent{Quota: eventQuota(consumed), Delta: amount, ChangeType: "consumed"})
			if eventErr != nil {
				return eventErr
			}
			return s.repository.AddOutbox(ctx, tx, event)
		})
	})
	if err != nil {
		return Quota{}, false, translate(err)
	}
	if !allowed {
		value, getErr := s.repository.GetQuota(ctx, tenantID, key)
		if getErr != nil {
			return Quota{}, false, translate(getErr)
		}
		return value, false, nil
	}
	return consumed, true, nil
}

func (s *Service) withResourceLock(ctx context.Context, key string, operation func() error) error {
	if s.locker == nil {
		return operation()
	}
	lock, err := s.locker.Lock(ctx, key, 15*time.Second, 50*time.Millisecond)
	if err != nil {
		return err
	}
	operationErr := operation()
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return errors.Join(operationErr, lock.Unlock(unlockCtx))
}

func secureToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func eventInvitation(value Invitation) *tenantv1.Invitation {
	return &tenantv1.Invitation{Id: value.ID, TenantId: value.TenantID, Email: value.Email, Status: value.Status, ExpiresAt: timestamppb.New(value.ExpiresAt), AcceptedByUserId: value.AcceptedByUserID, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func eventGroup(value Group) *tenantv1.Group {
	return &tenantv1.Group{Id: value.ID, TenantId: value.TenantID, Code: value.Code, Name: value.Name, Status: value.Status, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
func eventQuota(value Quota) *tenantv1.Quota {
	return &tenantv1.Quota{TenantId: value.TenantID, Key: value.Key, Limit: value.Limit, Used: value.Used, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}
