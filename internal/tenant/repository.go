package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

var (
	ErrNotFound     = errors.New("tenant resource not found")
	ErrConflict     = errors.New("tenant resource already exists")
	ErrStaleVersion = errors.New("tenant resource version is stale")
)

type Repository interface {
	CreateTenant(context.Context, sqlx.ExtContext, Tenant) error
	CreateMembership(context.Context, sqlx.ExtContext, Membership) error
	GetTenant(context.Context, string) (Tenant, error)
	UpdateTenant(context.Context, sqlx.ExtContext, Tenant) error
	GetMembership(context.Context, string) (Membership, error)
	ValidateMembership(context.Context, string, string) (Tenant, Membership, error)
	ListUserTenants(context.Context, string, int, int) ([]Tenant, int64, error)
	ListMemberships(context.Context, string, string, string, int, int) ([]Membership, int64, error)
	BatchGetMemberships(context.Context, string, []string) ([]Membership, error)
	ListTenants(context.Context, string, string, int, int) ([]Tenant, int64, error)
	UpdateMembership(context.Context, sqlx.ExtContext, Membership) error
	AddOutbox(context.Context, sqlx.ExtContext, OutboxEvent) error
	CreateOrganizationUnit(context.Context, sqlx.ExtContext, OrganizationUnit) error
	GetOrganizationUnit(context.Context, string) (OrganizationUnit, error)
	ListOrganizationUnits(context.Context, string) ([]OrganizationUnit, error)
	UpdateOrganizationUnit(context.Context, sqlx.ExtContext, OrganizationUnit, string) error
	ResolveOrganizationScope(context.Context, string, string) ([]string, error)
	CreateInvitation(context.Context, sqlx.ExtContext, Invitation) error
	GetInvitation(context.Context, string) (Invitation, error)
	GetInvitationByTokenHash(context.Context, string) (Invitation, error)
	UpdateInvitation(context.Context, sqlx.ExtContext, Invitation) error
	ListInvitations(context.Context, string, int, int) ([]Invitation, int64, error)
	CreateGroup(context.Context, sqlx.ExtContext, Group) error
	GetGroup(context.Context, string) (Group, error)
	UpdateGroup(context.Context, sqlx.ExtContext, Group) error
	ListGroups(context.Context, string) ([]Group, error)
	SearchGroups(context.Context, string, string, string, int, int) ([]Group, int64, error)
	CreateGroupMember(context.Context, sqlx.ExtContext, GroupMember) error
	GetGroupMember(context.Context, string, string) (GroupMember, error)
	UpdateGroupMember(context.Context, sqlx.ExtContext, GroupMember) error
	ListGroupMembers(context.Context, string) ([]GroupMember, error)
	GetQuota(context.Context, string, string) (Quota, error)
	ListQuotas(context.Context, string, string, int, int) ([]Quota, int64, error)
	CreateQuota(context.Context, sqlx.ExtContext, Quota) error
	UpdateQuota(context.Context, sqlx.ExtContext, Quota) error
	ConsumeQuota(context.Context, sqlx.ExtContext, string, string, int64, time.Time, string) (Quota, bool, error)
}

type SQLRepository struct{ db *sqlx.DB }

func NewRepository(db *sqlx.DB) Repository { return &SQLRepository{db: db} }

const tenantColumns = "id, code, name, status, version, created_at, updated_at, created_by, updated_by"
const membershipColumns = "id, tenant_id, user_id, status, primary_organization_unit_id, joined_at, version, created_at, updated_at, created_by, updated_by"
const membershipSelectColumns = "id, tenant_id, user_id, status, COALESCE(primary_organization_unit_id, '') AS primary_organization_unit_id, joined_at, version, created_at, updated_at, created_by, updated_by"
const organizationColumns = "id, tenant_id, COALESCE(parent_id, '') AS parent_id, code, name, path, status, version, created_at, updated_at, created_by, updated_by"
const invitationColumns = "id, tenant_id, email, token_hash, status, expires_at, COALESCE(accepted_by_user_id, '') AS accepted_by_user_id, version, created_at, updated_at, created_by, updated_by"
const groupColumns = "id, tenant_id, code, name, status, version, created_at, updated_at, created_by, updated_by"
const groupMemberColumns = "id, tenant_id, group_id, membership_id, status, version, created_at, updated_at, created_by, updated_by"
const quotaColumns = "tenant_id, quota_key, limit_value, used_value, version, created_at, updated_at, created_by, updated_by"

func (r *SQLRepository) CreateTenant(ctx context.Context, exec sqlx.ExtContext, value Tenant) error {
	query := r.db.Rebind("INSERT INTO tenants (" + tenantColumns + ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if _, err := exec.ExecContext(ctx, query, value.ID, value.Code, value.Name, value.Status, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy); err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

func (r *SQLRepository) CreateMembership(ctx context.Context, exec sqlx.ExtContext, value Membership) error {
	query := r.db.Rebind("INSERT INTO memberships (" + membershipColumns + ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if _, err := exec.ExecContext(ctx, query, value.ID, value.TenantID, value.UserID, value.Status, nullableID(value.PrimaryOrganizationUnitID), value.JoinedAt, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy); err != nil {
		return fmt.Errorf("insert membership: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetTenant(ctx context.Context, id string) (Tenant, error) {
	var value Tenant
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+tenantColumns+" FROM tenants WHERE id = ?"), id); err != nil {
		return Tenant{}, mapNotFound(err, "select tenant")
	}
	return value, nil
}

func (r *SQLRepository) UpdateTenant(ctx context.Context, exec sqlx.ExtContext, value Tenant) error {
	query := r.db.Rebind("UPDATE tenants SET name = ?, status = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Name, value.Status, value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	return affected(result, err, "update tenant")
}

func (r *SQLRepository) GetMembership(ctx context.Context, id string) (Membership, error) {
	var value Membership
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+membershipSelectColumns+" FROM memberships WHERE id = ?"), id); err != nil {
		return Membership{}, mapNotFound(err, "select membership")
	}
	return value, nil
}

func (r *SQLRepository) ValidateMembership(ctx context.Context, userID, tenantID string) (Tenant, Membership, error) {
	var membership Membership
	query := r.db.Rebind("SELECT " + membershipSelectColumns + " FROM memberships WHERE user_id = ? AND tenant_id = ? AND status = 'active'")
	if err := r.db.GetContext(ctx, &membership, query, userID, tenantID); err != nil {
		return Tenant{}, Membership{}, mapNotFound(err, "validate membership")
	}
	tenantValue, err := r.GetTenant(ctx, tenantID)
	return tenantValue, membership, err
}

func (r *SQLRepository) ListUserTenants(ctx context.Context, userID string, limit, offset int) ([]Tenant, int64, error) {
	var total int64
	count := r.db.Rebind("SELECT COUNT(*) FROM memberships m JOIN tenants t ON t.id = m.tenant_id WHERE m.user_id = ? AND m.status = 'active' AND t.status = 'active'")
	if err := r.db.GetContext(ctx, &total, count, userID); err != nil {
		return nil, 0, fmt.Errorf("count user tenants: %w", err)
	}
	items := make([]Tenant, 0)
	query := r.db.Rebind("SELECT " + prefixedTenantColumns("t") + " FROM memberships m JOIN tenants t ON t.id = m.tenant_id WHERE m.user_id = ? AND m.status = 'active' AND t.status = 'active' ORDER BY t.created_at DESC, t.id DESC LIMIT ? OFFSET ?")
	if err := r.db.SelectContext(ctx, &items, query, userID, limit, offset); err != nil {
		return nil, 0, fmt.Errorf("list user tenants: %w", err)
	}
	return items, total, nil
}

func (r *SQLRepository) ListMemberships(ctx context.Context, tenantID, userID, status string, limit, offset int) ([]Membership, int64, error) {
	where := " WHERE tenant_id = ?"
	args := []any{tenantID}
	if userID != "" {
		where += " AND user_id = ?"
		args = append(args, userID)
	}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind("SELECT COUNT(*) FROM memberships"+where), args...); err != nil {
		return nil, 0, fmt.Errorf("count memberships: %w", err)
	}
	items := make([]Membership, 0, limit)
	queryArgs := append(append([]any(nil), args...), limit, offset)
	query := r.db.Rebind("SELECT " + membershipSelectColumns + " FROM memberships" + where + " ORDER BY joined_at DESC, id DESC LIMIT ? OFFSET ?")
	if err := r.db.SelectContext(ctx, &items, query, queryArgs...); err != nil {
		return nil, 0, fmt.Errorf("list memberships: %w", err)
	}
	return items, total, nil
}

func (r *SQLRepository) BatchGetMemberships(ctx context.Context, tenantID string, ids []string) ([]Membership, error) {
	query, args, err := sqlx.In("SELECT "+membershipSelectColumns+" FROM memberships WHERE tenant_id = ? AND id IN (?) ORDER BY id", tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("build batch membership query: %w", err)
	}
	items := make([]Membership, 0, len(ids))
	if err := r.db.SelectContext(ctx, &items, r.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("batch get memberships: %w", err)
	}
	return items, nil
}

func (r *SQLRepository) ListTenants(ctx context.Context, keyword, status string, limit, offset int) ([]Tenant, int64, error) {
	where := " WHERE 1=1"
	args := make([]any, 0, 4)
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if keyword != "" {
		where += " AND (LOWER(code) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?))"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind("SELECT COUNT(*) FROM tenants"+where), args...); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	items := make([]Tenant, 0, limit)
	queryArgs := append(append([]any(nil), args...), limit, offset)
	query := r.db.Rebind("SELECT " + tenantColumns + " FROM tenants" + where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?")
	if err := r.db.SelectContext(ctx, &items, query, queryArgs...); err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	return items, total, nil
}

func (r *SQLRepository) UpdateMembership(ctx context.Context, exec sqlx.ExtContext, value Membership) error {
	query := r.db.Rebind("UPDATE memberships SET status = ?, primary_organization_unit_id = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Status, nullableID(value.PrimaryOrganizationUnitID), value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	return affected(result, err, "update membership")
}

func (r *SQLRepository) AddOutbox(ctx context.Context, exec sqlx.ExtContext, event OutboxEvent) error {
	query := r.db.Rebind("INSERT INTO tenant_outbox_events (id, subject, envelope, attempts, available_at, published_at, last_error, version, created_at, updated_at, created_by, updated_by) VALUES (?, ?, ?, 0, ?, NULL, '', ?, ?, ?, ?, ?)")
	if _, err := exec.ExecContext(ctx, query, event.ID, event.Subject, event.Envelope, event.AvailableAt, event.Version, event.CreatedAt, event.UpdatedAt, event.CreatedBy, event.UpdatedBy); err != nil {
		return fmt.Errorf("insert tenant outbox event: %w", err)
	}
	return nil
}

func (r *SQLRepository) CreateOrganizationUnit(ctx context.Context, exec sqlx.ExtContext, value OrganizationUnit) error {
	query := r.db.Rebind("INSERT INTO organization_units (id, tenant_id, parent_id, code, name, path, status, version, created_at, updated_at, created_by, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if _, err := exec.ExecContext(ctx, query, value.ID, value.TenantID, nullableID(value.ParentID), value.Code, value.Name, value.Path, value.Status, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy); err != nil {
		return fmt.Errorf("insert organization unit: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetOrganizationUnit(ctx context.Context, id string) (OrganizationUnit, error) {
	var value OrganizationUnit
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+organizationColumns+" FROM organization_units WHERE id = ?"), id); err != nil {
		return OrganizationUnit{}, mapNotFound(err, "select organization unit")
	}
	return value, nil
}

func (r *SQLRepository) ListOrganizationUnits(ctx context.Context, tenantID string) ([]OrganizationUnit, error) {
	items := make([]OrganizationUnit, 0)
	query := r.db.Rebind("SELECT " + organizationColumns + " FROM organization_units WHERE tenant_id = ? ORDER BY path, id")
	if err := r.db.SelectContext(ctx, &items, query, tenantID); err != nil {
		return nil, fmt.Errorf("list organization units: %w", err)
	}
	return items, nil
}

func (r *SQLRepository) UpdateOrganizationUnit(ctx context.Context, exec sqlx.ExtContext, value OrganizationUnit, oldPath string) error {
	query := r.db.Rebind("UPDATE organization_units SET parent_id = ?, name = ?, path = ?, status = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, nullableID(value.ParentID), value.Name, value.Path, value.Status, value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	if err := affected(result, err, "update organization unit"); err != nil {
		return err
	}
	if oldPath == value.Path {
		return nil
	}
	start := len(oldPath) + 1
	descendants := descendantMoveSQL(r.db.DriverName())
	_, err = exec.ExecContext(ctx, r.db.Rebind(descendants), value.Path, start, value.UpdatedAt, value.UpdatedBy, value.TenantID, oldPath+"%", value.ID)
	if err != nil {
		return fmt.Errorf("move organization descendants: %w", err)
	}
	return nil
}

func descendantMoveSQL(driver string) string {
	if driver == "mysql" {
		return "UPDATE organization_units SET path = CONCAT(?, SUBSTRING(path, ?)), version = version + 1, updated_at = ?, updated_by = ? WHERE tenant_id = ? AND path LIKE ? AND id <> ?"
	}
	return "UPDATE organization_units SET path = ? || SUBSTRING(path FROM CAST(? AS INTEGER)), version = version + 1, updated_at = ?, updated_by = ? WHERE tenant_id = ? AND path LIKE ? AND id <> ?"
}

func (r *SQLRepository) ResolveOrganizationScope(ctx context.Context, tenantID, rootPath string) ([]string, error) {
	ids := make([]string, 0)
	query := r.db.Rebind("SELECT id FROM organization_units WHERE tenant_id = ? AND path LIKE ? AND status = 'active' ORDER BY path, id")
	if err := r.db.SelectContext(ctx, &ids, query, tenantID, rootPath+"%"); err != nil {
		return nil, fmt.Errorf("resolve organization scope: %w", err)
	}
	return ids, nil
}

func (r *SQLRepository) CreateInvitation(ctx context.Context, exec sqlx.ExtContext, value Invitation) error {
	query := r.db.Rebind("INSERT INTO invitations (id, tenant_id, email, token_hash, status, expires_at, accepted_by_user_id, version, created_at, updated_at, created_by, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	_, err := exec.ExecContext(ctx, query, value.ID, value.TenantID, value.Email, value.TokenHash, value.Status, value.ExpiresAt, nullableID(value.AcceptedByUserID), value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert invitation: %w", err)
	}
	return nil
}
func (r *SQLRepository) GetInvitation(ctx context.Context, id string) (Invitation, error) {
	var value Invitation
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+invitationColumns+" FROM invitations WHERE id = ?"), id); err != nil {
		return Invitation{}, mapNotFound(err, "select invitation")
	}
	return value, nil
}
func (r *SQLRepository) GetInvitationByTokenHash(ctx context.Context, hash string) (Invitation, error) {
	var value Invitation
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+invitationColumns+" FROM invitations WHERE token_hash = ?"), hash); err != nil {
		return Invitation{}, mapNotFound(err, "select invitation by token")
	}
	return value, nil
}
func (r *SQLRepository) UpdateInvitation(ctx context.Context, exec sqlx.ExtContext, value Invitation) error {
	query := r.db.Rebind("UPDATE invitations SET status = ?, accepted_by_user_id = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Status, nullableID(value.AcceptedByUserID), value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	return affected(result, err, "update invitation")
}
func (r *SQLRepository) ListInvitations(ctx context.Context, tenantID string, limit, offset int) ([]Invitation, int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind("SELECT COUNT(*) FROM invitations WHERE tenant_id = ?"), tenantID); err != nil {
		return nil, 0, fmt.Errorf("count invitations: %w", err)
	}
	items := make([]Invitation, 0)
	if err := r.db.SelectContext(ctx, &items, r.db.Rebind("SELECT "+invitationColumns+" FROM invitations WHERE tenant_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"), tenantID, limit, offset); err != nil {
		return nil, 0, fmt.Errorf("list invitations: %w", err)
	}
	return items, total, nil
}

func (r *SQLRepository) CreateGroup(ctx context.Context, exec sqlx.ExtContext, value Group) error {
	query := r.db.Rebind("INSERT INTO member_groups (" + groupColumns + ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	_, err := exec.ExecContext(ctx, query, value.ID, value.TenantID, value.Code, value.Name, value.Status, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert member group: %w", err)
	}
	return nil
}
func (r *SQLRepository) GetGroup(ctx context.Context, id string) (Group, error) {
	var value Group
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+groupColumns+" FROM member_groups WHERE id = ?"), id); err != nil {
		return Group{}, mapNotFound(err, "select member group")
	}
	return value, nil
}
func (r *SQLRepository) UpdateGroup(ctx context.Context, exec sqlx.ExtContext, value Group) error {
	query := r.db.Rebind("UPDATE member_groups SET name = ?, status = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Name, value.Status, value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	return affected(result, err, "update member group")
}
func (r *SQLRepository) ListGroups(ctx context.Context, tenantID string) ([]Group, error) {
	items := make([]Group, 0)
	if err := r.db.SelectContext(ctx, &items, r.db.Rebind("SELECT "+groupColumns+" FROM member_groups WHERE tenant_id = ? ORDER BY created_at, id"), tenantID); err != nil {
		return nil, fmt.Errorf("list member groups: %w", err)
	}
	return items, nil
}

func (r *SQLRepository) SearchGroups(ctx context.Context, tenantID, keyword, status string, limit, offset int) ([]Group, int64, error) {
	where := " WHERE tenant_id = ?"
	args := []any{tenantID}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if keyword != "" {
		where += " AND (LOWER(code) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?))"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind("SELECT COUNT(*) FROM member_groups"+where), args...); err != nil {
		return nil, 0, fmt.Errorf("count member groups: %w", err)
	}
	items := make([]Group, 0, limit)
	queryArgs := append(append([]any(nil), args...), limit, offset)
	query := "SELECT " + groupColumns + " FROM member_groups" + where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	if err := r.db.SelectContext(ctx, &items, r.db.Rebind(query), queryArgs...); err != nil {
		return nil, 0, fmt.Errorf("search member groups: %w", err)
	}
	return items, total, nil
}
func (r *SQLRepository) CreateGroupMember(ctx context.Context, exec sqlx.ExtContext, value GroupMember) error {
	query := r.db.Rebind(createGroupMemberSQL())
	_, err := exec.ExecContext(ctx, query, value.ID, value.TenantID, value.GroupID, value.MembershipID, value.Status, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert group member: %w", err)
	}
	return nil
}

func createGroupMemberSQL() string {
	return "INSERT INTO group_members (" + groupMemberColumns + ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
}
func (r *SQLRepository) GetGroupMember(ctx context.Context, groupID, membershipID string) (GroupMember, error) {
	var value GroupMember
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+groupMemberColumns+" FROM group_members WHERE group_id = ? AND membership_id = ?"), groupID, membershipID); err != nil {
		return GroupMember{}, mapNotFound(err, "select group member")
	}
	return value, nil
}
func (r *SQLRepository) UpdateGroupMember(ctx context.Context, exec sqlx.ExtContext, value GroupMember) error {
	query := r.db.Rebind("UPDATE group_members SET status = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Status, value.UpdatedAt, value.UpdatedBy, value.ID, value.Version)
	return affected(result, err, "update group member")
}
func (r *SQLRepository) ListGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	items := make([]GroupMember, 0)
	query := r.db.Rebind("SELECT " + groupMemberColumns + " FROM group_members WHERE group_id = ? ORDER BY created_at, id")
	if err := r.db.SelectContext(ctx, &items, query, groupID); err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return items, nil
}

func (r *SQLRepository) GetQuota(ctx context.Context, tenantID, key string) (Quota, error) {
	var value Quota
	if err := r.db.GetContext(ctx, &value, r.db.Rebind("SELECT "+quotaColumns+" FROM tenant_quotas WHERE tenant_id = ? AND quota_key = ?"), tenantID, key); err != nil {
		return Quota{}, mapNotFound(err, "select tenant quota")
	}
	return value, nil
}
func (r *SQLRepository) ListQuotas(ctx context.Context, tenantID, keyword string, limit, offset int) ([]Quota, int64, error) {
	where := " WHERE tenant_id = ?"
	args := []any{tenantID}
	if keyword != "" {
		where += " AND LOWER(quota_key) LIKE LOWER(?)"
		args = append(args, "%"+keyword+"%")
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind("SELECT COUNT(*) FROM tenant_quotas"+where), args...); err != nil {
		return nil, 0, fmt.Errorf("count tenant quotas: %w", err)
	}
	items := make([]Quota, 0, limit)
	queryArgs := append(append([]any(nil), args...), limit, offset)
	query := r.db.Rebind("SELECT " + quotaColumns + " FROM tenant_quotas" + where + " ORDER BY quota_key, tenant_id LIMIT ? OFFSET ?")
	if err := r.db.SelectContext(ctx, &items, query, queryArgs...); err != nil {
		return nil, 0, fmt.Errorf("list tenant quotas: %w", err)
	}
	return items, total, nil
}
func (r *SQLRepository) CreateQuota(ctx context.Context, exec sqlx.ExtContext, value Quota) error {
	query := r.db.Rebind("INSERT INTO tenant_quotas (" + quotaColumns + ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)")
	_, err := exec.ExecContext(ctx, query, value.TenantID, value.Key, value.Limit, value.Used, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	if err != nil {
		return fmt.Errorf("insert tenant quota: %w", err)
	}
	return nil
}
func (r *SQLRepository) UpdateQuota(ctx context.Context, exec sqlx.ExtContext, value Quota) error {
	query := r.db.Rebind("UPDATE tenant_quotas SET limit_value = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE tenant_id = ? AND quota_key = ? AND version = ?")
	result, err := exec.ExecContext(ctx, query, value.Limit, value.UpdatedAt, value.UpdatedBy, value.TenantID, value.Key, value.Version)
	return affected(result, err, "update tenant quota")
}
func (r *SQLRepository) ConsumeQuota(ctx context.Context, exec sqlx.ExtContext, tenantID, key string, amount int64, now time.Time, actor string) (Quota, bool, error) {
	query := r.db.Rebind("UPDATE tenant_quotas SET used_value = used_value + ?, version = version + 1, updated_at = ?, updated_by = ? WHERE tenant_id = ? AND quota_key = ? AND used_value + ? <= limit_value")
	result, err := exec.ExecContext(ctx, query, amount, now, actor, tenantID, key, amount)
	if err != nil {
		return Quota{}, false, fmt.Errorf("consume tenant quota: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Quota{}, false, fmt.Errorf("consume tenant quota affected rows: %w", err)
	}
	if count == 0 {
		return Quota{}, false, nil
	}
	var value Quota
	if err := sqlx.GetContext(ctx, exec, &value, r.db.Rebind("SELECT "+quotaColumns+" FROM tenant_quotas WHERE tenant_id = ? AND quota_key = ?"), tenantID, key); err != nil {
		return Quota{}, false, fmt.Errorf("select consumed tenant quota: %w", err)
	}
	return value, true, nil
}

func mapNotFound(err error, operation string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func affected(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s affected rows: %w", operation, err)
	}
	if count == 0 {
		return ErrStaleVersion
	}
	return nil
}

func prefixedTenantColumns(alias string) string {
	return alias + ".id, " + alias + ".code, " + alias + ".name, " + alias + ".status, " + alias + ".version, " + alias + ".created_at, " + alias + ".updated_at, " + alias + ".created_by, " + alias + ".updated_by"
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
