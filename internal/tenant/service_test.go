package tenant

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/database"
	"github.com/lihongjie0209/tenant-service/internal/idempotency"
	"github.com/redis/go-redis/v9"
)

type fakeRepository struct {
	tenant        Tenant
	membership    Membership
	invitation    Invitation
	group         Group
	groupMember   GroupMember
	quota         Quota
	updateErr     error
	outbox        []OutboxEvent
	organization  OrganizationUnit
	organizations map[string]OrganizationUnit
}

func TestAuthorizeTenantBindsUserToJWTClaim(t *testing.T) {
	t.Parallel()
	userContext := principal.WithContext(t.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	if err := authorizeTenant(userContext, "tenant-1"); err != nil {
		t.Fatalf("matching tenant rejected: %v", err)
	}
	if err := authorizeTenant(userContext, "tenant-2"); err == nil {
		t.Fatal("cross-tenant user request was accepted")
	}
	if err := authorizeTenant(WithPlatformAdministration(userContext), "tenant-2"); err != nil {
		t.Fatalf("platform-authorized target rejected: %v", err)
	}
	serviceContext := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	if err := authorizeTenant(serviceContext, "tenant-2"); err != nil {
		t.Fatalf("service orchestration rejected: %v", err)
	}
}

func TestCreateTenantRejectsDifferentOwnerForUser(t *testing.T) {
	t.Parallel()
	service := &Service{}
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser})
	_, _, err := service.Create(ctx, "tenant", "Tenant", "user-2")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeForbidden {
		t.Fatalf("Create() error = %#v, want forbidden", err)
	}
}

func TestCreateTenantAllowsDifferentOwnerAfterPlatformDecision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	service := NewService(&fakeRepository{}, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "platform-admin", Type: principal.TypeUser})
	_, owner, err := service.Create(WithPlatformAdministration(ctx), "tenant", "Tenant", "user-2")
	if err != nil {
		t.Fatal(err)
	}
	if owner.UserID != "user-2" {
		t.Fatalf("owner user = %q", owner.UserID)
	}
}

func (f *fakeRepository) CreateTenant(_ context.Context, _ sqlx.ExtContext, value Tenant) error {
	f.tenant = value
	return nil
}
func (f *fakeRepository) CreateMembership(_ context.Context, _ sqlx.ExtContext, value Membership) error {
	f.membership = value
	return nil
}
func (f *fakeRepository) GetTenant(context.Context, string) (Tenant, error) { return f.tenant, nil }
func (f *fakeRepository) UpdateTenant(context.Context, sqlx.ExtContext, Tenant) error {
	return f.updateErr
}
func (f *fakeRepository) GetMembership(context.Context, string) (Membership, error) {
	return f.membership, nil
}
func (f *fakeRepository) ValidateMembership(context.Context, string, string) (Tenant, Membership, error) {
	return f.tenant, f.membership, nil
}
func (f *fakeRepository) ListUserTenants(context.Context, string, int, int) ([]Tenant, int64, error) {
	return []Tenant{f.tenant}, 1, nil
}
func (f *fakeRepository) ListMemberships(context.Context, string, string, string, int, int) ([]Membership, int64, error) {
	return []Membership{f.membership}, 1, nil
}
func (f *fakeRepository) BatchGetMemberships(context.Context, string, []string) ([]Membership, error) {
	return []Membership{f.membership}, nil
}
func (f *fakeRepository) BatchGetOrganizationUnits(context.Context, string, []string) ([]OrganizationUnit, error) {
	return []OrganizationUnit{f.organization}, nil
}
func (f *fakeRepository) FindMembershipsByUserIDs(context.Context, string, []string, string) ([]Membership, error) {
	return []Membership{f.membership}, nil
}

type capturingBatchMembershipRepository struct {
	fakeRepository
	tenantID string
	ids      []string
}

type capturingBatchOrganizationRepository struct {
	fakeRepository
	tenantID string
	ids      []string
}

func (r *capturingBatchOrganizationRepository) BatchGetOrganizationUnits(_ context.Context, tenantID string, ids []string) ([]OrganizationUnit, error) {
	r.tenantID = tenantID
	r.ids = append([]string(nil), ids...)
	return []OrganizationUnit{{ID: ids[0], TenantID: tenantID}}, nil
}

type capturingGroupSearchRepository struct {
	fakeRepository
	tenantID string
	keyword  string
	status   string
	limit    int
	offset   int
}

type capturingGroupMemberBatchRepository struct {
	fakeRepository
	groupID       string
	membershipIDs []string
}

func (r *capturingGroupMemberBatchRepository) BatchGetGroupMembers(_ context.Context, groupID string, membershipIDs []string) ([]GroupMember, error) {
	r.groupID, r.membershipIDs = groupID, append([]string(nil), membershipIDs...)
	return []GroupMember{r.groupMember}, nil
}

func (r *capturingGroupSearchRepository) SearchGroups(_ context.Context, tenantID, keyword, status string, limit, offset int) ([]Group, int64, error) {
	r.tenantID, r.keyword, r.status, r.limit, r.offset = tenantID, keyword, status, limit, offset
	return []Group{r.group}, 1, nil
}

func (r *capturingBatchMembershipRepository) BatchGetMemberships(_ context.Context, tenantID string, ids []string) ([]Membership, error) {
	r.tenantID = tenantID
	r.ids = append([]string(nil), ids...)
	return []Membership{r.membership}, nil
}
func (f *fakeRepository) ListTenants(context.Context, string, string, int, int) ([]Tenant, int64, error) {
	return nil, 0, nil
}
func (f *fakeRepository) UpdateMembership(context.Context, sqlx.ExtContext, Membership) error {
	return f.updateErr
}
func (f *fakeRepository) AddOutbox(_ context.Context, _ sqlx.ExtContext, event OutboxEvent) error {
	f.outbox = append(f.outbox, event)
	return nil
}
func (f *fakeRepository) CreateOrganizationUnit(_ context.Context, _ sqlx.ExtContext, value OrganizationUnit) error {
	f.organization = value
	return nil
}
func (f *fakeRepository) GetOrganizationUnit(_ context.Context, id string) (OrganizationUnit, error) {
	if f.organizations != nil {
		value, ok := f.organizations[id]
		if !ok {
			return OrganizationUnit{}, ErrNotFound
		}
		return value, nil
	}
	return f.organization, nil
}
func (f *fakeRepository) ListOrganizationUnits(context.Context, string) ([]OrganizationUnit, error) {
	return []OrganizationUnit{f.organization}, nil
}
func (f *fakeRepository) UpdateOrganizationUnit(_ context.Context, _ sqlx.ExtContext, value OrganizationUnit, _ string) error {
	f.organization = value
	return f.updateErr
}
func (f *fakeRepository) ResolveOrganizationScope(context.Context, string, string) ([]string, error) {
	return []string{f.organization.ID}, nil
}

func (f *fakeRepository) CreateInvitation(_ context.Context, _ sqlx.ExtContext, value Invitation) error {
	f.invitation = value
	return nil
}
func (f *fakeRepository) GetInvitation(context.Context, string) (Invitation, error) {
	if f.invitation.ID == "" {
		return Invitation{}, ErrNotFound
	}
	return f.invitation, nil
}
func (f *fakeRepository) GetInvitationByTokenHash(_ context.Context, hash string) (Invitation, error) {
	if f.invitation.TokenHash != hash {
		return Invitation{}, ErrNotFound
	}
	return f.invitation, nil
}
func (f *fakeRepository) UpdateInvitation(_ context.Context, _ sqlx.ExtContext, value Invitation) error {
	if f.updateErr == nil {
		value.Version++
		f.invitation = value
	}
	return f.updateErr
}
func (f *fakeRepository) ListInvitations(context.Context, string, int, int) ([]Invitation, int64, error) {
	return []Invitation{f.invitation}, 1, nil
}
func (f *fakeRepository) CreateGroup(_ context.Context, _ sqlx.ExtContext, value Group) error {
	f.group = value
	return nil
}
func (f *fakeRepository) GetGroup(context.Context, string) (Group, error) {
	if f.group.ID == "" {
		return Group{}, ErrNotFound
	}
	return f.group, nil
}
func (f *fakeRepository) UpdateGroup(_ context.Context, _ sqlx.ExtContext, value Group) error {
	if f.updateErr == nil {
		value.Version++
		f.group = value
	}
	return f.updateErr
}
func (f *fakeRepository) ListGroups(context.Context, string) ([]Group, error) {
	return []Group{f.group}, nil
}
func (f *fakeRepository) SearchGroups(context.Context, string, string, string, int, int) ([]Group, int64, error) {
	return []Group{f.group}, 1, nil
}
func (f *fakeRepository) CreateGroupMember(_ context.Context, _ sqlx.ExtContext, value GroupMember) error {
	f.groupMember = value
	return nil
}
func (f *fakeRepository) GetGroupMember(context.Context, string, string) (GroupMember, error) {
	if f.groupMember.ID == "" {
		return GroupMember{}, ErrNotFound
	}
	return f.groupMember, nil
}
func (f *fakeRepository) UpdateGroupMember(_ context.Context, _ sqlx.ExtContext, value GroupMember) error {
	if f.updateErr == nil {
		value.Version++
		f.groupMember = value
	}
	return f.updateErr
}
func (f *fakeRepository) ListGroupMembers(context.Context, string) ([]GroupMember, error) {
	return []GroupMember{f.groupMember}, nil
}
func (f *fakeRepository) BatchGetGroupMembers(context.Context, string, []string) ([]GroupMember, error) {
	return []GroupMember{f.groupMember}, nil
}
func (f *fakeRepository) GetQuota(context.Context, string, string) (Quota, error) {
	if f.quota.TenantID == "" {
		return Quota{}, ErrNotFound
	}
	return f.quota, nil
}
func (f *fakeRepository) ListQuotas(context.Context, string, string, int, int) ([]Quota, int64, error) {
	return []Quota{f.quota}, 1, nil
}
func (f *fakeRepository) CreateQuota(_ context.Context, _ sqlx.ExtContext, value Quota) error {
	f.quota = value
	return nil
}
func (f *fakeRepository) UpdateQuota(_ context.Context, _ sqlx.ExtContext, value Quota) error {
	if f.updateErr == nil {
		value.Version++
		f.quota = value
	}
	return f.updateErr
}
func (f *fakeRepository) ConsumeQuota(_ context.Context, _ sqlx.ExtContext, _ string, _ string, amount int64, now time.Time, actor string) (Quota, bool, error) {
	if f.quota.Used+amount > f.quota.Limit {
		return Quota{}, false, nil
	}
	f.quota.Used += amount
	f.quota.Version++
	f.quota.UpdatedAt = now
	f.quota.UpdatedBy = actor
	return f.quota, true, nil
}

func TestService_ListMemberships(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{membership: Membership{ID: "membership-1", TenantID: "tenant-1", UserID: "user-1", Status: "active"}}
	service := NewService(repository, &database.Transactor{}, nil)

	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	page, err := service.ListMemberships(ctx, "tenant-1", "user-1", "active", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Page != 1 || page.PageSize != 20 || len(page.Memberships) != 1 {
		t.Fatalf("ListMemberships() = %+v", page)
	}
}

func TestService_ListMembershipsRejectsInvalidQuery(t *testing.T) {
	t.Parallel()
	service := NewService(&fakeRepository{}, &database.Transactor{}, nil)
	tests := []struct {
		name     string
		tenantID string
		status   string
		pageSize int
	}{
		{name: "missing tenant", status: "active", pageSize: 20},
		{name: "invalid status", tenantID: "tenant-1", status: "unknown", pageSize: 20},
		{name: "oversized page", tenantID: "tenant-1", status: "active", pageSize: 101},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
			_, err := service.ListMemberships(ctx, test.tenantID, "", test.status, 1, test.pageSize)
			var appErr *apperror.Error
			if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
				t.Fatalf("ListMemberships() error = %v", err)
			}
		})
	}
}

func TestService_BatchGetMembershipsValidatesAndDeduplicatesIDs(t *testing.T) {
	t.Parallel()
	repository := &capturingBatchMembershipRepository{fakeRepository: fakeRepository{membership: Membership{ID: "membership-1"}}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})

	items, err := service.BatchGetMemberships(ctx, " tenant-1 ", []string{" membership-1 ", "membership-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(repository.ids) != 1 || repository.ids[0] != "membership-1" || repository.tenantID != "tenant-1" {
		t.Fatalf("BatchGetMemberships() items=%+v tenant=%q ids=%v", items, repository.tenantID, repository.ids)
	}

	_, err = service.BatchGetMemberships(ctx, "tenant-1", []string{""})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("empty membership ID error = %#v", err)
	}

	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("membership-%d", index)
	}
	_, err = service.BatchGetMemberships(ctx, "tenant-1", tooMany)
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("too many membership IDs error = %#v", err)
	}
}

func TestService_BatchGetOrganizationUnitsValidatesAndDeduplicatesIDs(t *testing.T) {
	t.Parallel()
	repository := &capturingBatchOrganizationRepository{}
	service := NewService(repository, nil, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	items, err := service.BatchGetOrganizationUnits(ctx, " tenant-1 ", []string{" unit-1 ", "unit-1"})
	if err != nil || len(items) != 1 || repository.tenantID != "tenant-1" || len(repository.ids) != 1 || repository.ids[0] != "unit-1" {
		t.Fatalf("BatchGetOrganizationUnits() items=%+v tenant=%q ids=%v err=%v", items, repository.tenantID, repository.ids, err)
	}
	if _, err := service.BatchGetOrganizationUnits(ctx, "tenant-1", []string{""}); err == nil {
		t.Fatal("BatchGetOrganizationUnits() accepted an empty ID")
	}
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("unit-%d", index)
	}
	if _, err := service.BatchGetOrganizationUnits(ctx, "tenant-1", tooMany); err == nil {
		t.Fatal("BatchGetOrganizationUnits() accepted more than 100 IDs")
	}
}

func TestService_FindMembershipsByUserIDsValidatesInput(t *testing.T) {
	t.Parallel()
	service := NewService(&fakeRepository{}, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	items, err := service.FindMembershipsByUserIDs(ctx, "tenant-1", nil, "active")
	if err != nil || len(items) != 0 {
		t.Fatalf("empty FindMembershipsByUserIDs() = (%+v, %v)", items, err)
	}
	_, err = service.FindMembershipsByUserIDs(ctx, "tenant-1", []string{"user-1"}, "unknown")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("invalid status error = %#v", err)
	}
	userIDs := make([]string, 101)
	for index := range userIDs {
		userIDs[index] = fmt.Sprintf("user-%d", index)
	}
	_, err = service.FindMembershipsByUserIDs(ctx, "tenant-1", userIDs, "active")
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("oversized user list error = %#v", err)
	}
}

func TestService_AddMembershipRejectsOrganizationFromAnotherTenant(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{organizations: map[string]OrganizationUnit{
		"org-2": {ID: "org-2", TenantID: "tenant-2", Status: "active"},
	}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	_, err := service.AddMembership(ctx, "tenant-1", "user-1", "org-2")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("AddMembership() error = %v", err)
	}
}

func TestSQLRepository_ListMembershipsAppliesTenantUserAndStatusFilters(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	countQuery := "SELECT COUNT(*) FROM memberships WHERE tenant_id = ? AND user_id = ? AND status = ?"
	listQuery := "SELECT " + membershipSelectColumns + " FROM memberships WHERE tenant_id = ? AND user_id = ? AND status = ? ORDER BY joined_at DESC, id DESC LIMIT ? OFFSET ?"
	mock.ExpectQuery(regexp.QuoteMeta(countQuery)).WithArgs("tenant-1", "user-1", "active").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	now := time.Date(2026, 8, 31, 22, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(listQuery)).WithArgs("tenant-1", "user-1", "active", 20, 0).WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "status", "primary_organization_unit_id", "joined_at", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("membership-1", "tenant-1", "user-1", "active", "org-1", now, 1, now, now, "admin-1", "admin-1"),
	)
	items, total, err := repository.ListMemberships(t.Context(), "tenant-1", "user-1", "active", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "membership-1" {
		t.Fatalf("ListMemberships() items=%+v total=%d", items, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_BatchGetMembershipsScopesIDsToTenant(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	query := "SELECT " + membershipSelectColumns + " FROM memberships WHERE tenant_id = ? AND id IN (?, ?) ORDER BY id"
	now := time.Date(2026, 8, 31, 22, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("tenant-1", "membership-1", "membership-2").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "status", "primary_organization_unit_id", "joined_at", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("membership-1", "tenant-1", "user-1", "active", "", now, 1, now, now, "admin-1", "admin-1"),
	)
	items, err := repository.BatchGetMemberships(t.Context(), "tenant-1", []string{"membership-1", "membership-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TenantID != "tenant-1" {
		t.Fatalf("BatchGetMemberships() = %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_BatchGetOrganizationUnitsScopesIDsToTenant(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	query := "SELECT " + organizationColumns + " FROM organization_units WHERE tenant_id = ? AND id IN (?, ?) ORDER BY id"
	now := time.Date(2026, 8, 31, 22, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("tenant-1", "unit-1", "unit-2").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "parent_id", "code", "name", "path", "status", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("unit-1", "tenant-1", "", "unit-1", "Unit 1", "/unit-1/", "active", 1, now, now, "admin-1", "admin-1"),
	)
	items, err := repository.BatchGetOrganizationUnits(t.Context(), "tenant-1", []string{"unit-1", "unit-2"})
	if err != nil || len(items) != 1 || items[0].TenantID != "tenant-1" {
		t.Fatalf("BatchGetOrganizationUnits() = (%+v, %v)", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_ListOrganizationChildrenIsBoundedAndReportsDescendants(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	const selectedColumns = "unit.id, unit.tenant_id, COALESCE(unit.parent_id, '') AS parent_id, unit.code, unit.name, unit.path, unit.status, unit.version, unit.created_at, unit.updated_at, unit.created_by, unit.updated_by"
	query := "SELECT " + selectedColumns + ", EXISTS (SELECT 1 FROM organization_units child WHERE child.tenant_id = unit.tenant_id AND child.parent_id = unit.id AND child.status = ?) AS has_children FROM organization_units unit WHERE unit.tenant_id = ? AND unit.parent_id IS NULL AND unit.status = ? ORDER BY unit.path, unit.id LIMIT ?"
	now := time.Date(2026, 8, 31, 22, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	rows := sqlmock.NewRows([]string{"id", "tenant_id", "parent_id", "code", "name", "path", "status", "version", "created_at", "updated_at", "created_by", "updated_by", "has_children"}).
		AddRow("unit-1", "tenant-1", "", "unit-1", "Unit 1", "/unit-1/", "active", 1, now, now, "admin-1", "admin-1", true).
		AddRow("unit-2", "tenant-1", "", "unit-2", "Unit 2", "/unit-2/", "active", 1, now, now, "admin-1", "admin-1", false).
		AddRow("unit-3", "tenant-1", "", "unit-3", "Unit 3", "/unit-3/", "active", 1, now, now, "admin-1", "admin-1", false)
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("active", "tenant-1", "active", 3).WillReturnRows(rows)
	nodes, truncated, err := repository.ListOrganizationChildren(t.Context(), "tenant-1", "", "active", 2)
	if err != nil || !truncated || len(nodes) != 2 || !nodes[0].HasChildren || nodes[1].HasChildren {
		t.Fatalf("ListOrganizationChildren() = (%+v, %v, %v)", nodes, truncated, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_SearchOrganizationUnitsLoadsOnlyMatchedPathsAndAncestors(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	now := time.Date(2026, 8, 31, 22, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	columns := []string{"id", "tenant_id", "parent_id", "code", "name", "path", "status", "version", "created_at", "updated_at", "created_by", "updated_by"}
	searchQuery := "SELECT " + organizationColumns + " FROM organization_units WHERE tenant_id = ? AND status = ? AND (LOWER(code) LIKE ? OR LOWER(name) LIKE ?) ORDER BY path, id LIMIT ?"
	mock.ExpectQuery(regexp.QuoteMeta(searchQuery)).WithArgs("tenant-1", "active", "%platform%", "%platform%", 3).WillReturnRows(
		sqlmock.NewRows(columns).AddRow("child", "tenant-1", "root", "platform", "Platform", "/root/child/", "active", 1, now, now, "admin-1", "admin-1"),
	)
	ancestorQuery := "SELECT " + organizationColumns + " FROM organization_units WHERE tenant_id = ? AND id IN (?) ORDER BY id"
	mock.ExpectQuery(regexp.QuoteMeta(ancestorQuery)).WithArgs("tenant-1", "root").WillReturnRows(
		sqlmock.NewRows(columns).AddRow("root", "tenant-1", "", "root", "Root", "/root/", "active", 1, now, now, "admin-1", "admin-1"),
	)
	items, truncated, err := repository.SearchOrganizationUnitsWithAncestors(t.Context(), "tenant-1", " Platform ", "active", 2)
	if err != nil || truncated || len(items) != 2 || items[0].ID != "root" || items[1].ID != "child" {
		t.Fatalf("SearchOrganizationUnitsWithAncestors() = (%+v, %v, %v)", items, truncated, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_FindMembershipsByUserIDsScopesTenantAndStatus(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	query := "SELECT " + membershipSelectColumns + " FROM memberships WHERE tenant_id = ? AND user_id IN (?, ?) AND status = ? ORDER BY id"
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("tenant-1", "user-1", "user-2", "active").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "status", "primary_organization_unit_id", "joined_at", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("membership-1", "tenant-1", "user-1", "active", "", now, 1, now, now, "admin", "admin"),
	)
	items, err := repository.FindMembershipsByUserIDs(t.Context(), "tenant-1", []string{"user-1", "user-2"}, "active")
	if err != nil || len(items) != 1 || items[0].UserID != "user-1" {
		t.Fatalf("FindMembershipsByUserIDs() = (%+v, %v)", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestService_SearchGroupsNormalizesAndBoundsQuery(t *testing.T) {
	t.Parallel()
	repository := &capturingGroupSearchRepository{fakeRepository: fakeRepository{group: Group{ID: "group-1"}}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	page, err := service.SearchGroups(ctx, " tenant-1 ", " Ops ", "active", 2, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Groups) != 1 || repository.tenantID != "tenant-1" || repository.keyword != "Ops" || repository.limit != 25 || repository.offset != 25 {
		t.Fatalf("SearchGroups() page=%+v repository=%+v", page, repository)
	}
	if _, err := service.SearchGroups(ctx, "tenant-1", "", "disabled", 1, 20); err != nil {
		t.Fatalf("disabled group query rejected: %v", err)
	}
	_, err = service.SearchGroups(ctx, "tenant-1", "", "unknown", 1, 20)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("invalid status error = %#v", err)
	}
	_, err = service.SearchGroups(ctx, "tenant-1", "", "", 1, 101)
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("oversized page error = %#v", err)
	}
}

func TestService_BatchGetGroupMembersNormalizesAndBoundsIDs(t *testing.T) {
	t.Parallel()
	repository := &capturingGroupMemberBatchRepository{fakeRepository: fakeRepository{
		group:       Group{ID: "group-1", TenantID: "tenant-1"},
		groupMember: GroupMember{ID: "assignment-1"},
	}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	items, err := service.BatchGetGroupMembers(ctx, " group-1 ", []string{" membership-1 ", "membership-1"})
	if err != nil || len(items) != 1 || repository.groupID != "group-1" || len(repository.membershipIDs) != 1 {
		t.Fatalf("BatchGetGroupMembers() items=%+v repository=%+v err=%v", items, repository, err)
	}
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("membership-%d", index)
	}
	if _, err := service.BatchGetGroupMembers(ctx, "group-1", tooMany); err == nil {
		t.Fatal("oversized group member batch must fail")
	}
}

func TestSQLRepository_BatchGetGroupMembersFiltersGroupAndMemberships(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	query := "SELECT " + groupMemberColumns + " FROM group_members WHERE group_id = ? AND membership_id IN (?, ?) ORDER BY id"
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("group-1", "membership-1", "membership-2").WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "group_id", "membership_id", "status", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("assignment-1", "tenant-1", "group-1", "membership-1", "active", 1, now, now, "admin", "admin"),
	)
	items, err := repository.BatchGetGroupMembers(t.Context(), "group-1", []string{"membership-1", "membership-2"})
	if err != nil || len(items) != 1 || items[0].MembershipID != "membership-1" {
		t.Fatalf("BatchGetGroupMembers() = (%+v, %v)", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_SearchGroupsScopesAndFiltersQuery(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	where := " WHERE tenant_id = ? AND status = ? AND (LOWER(code) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?))"
	args := []driver.Value{"tenant-1", "active", "%ops%", "%ops%"}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM member_groups" + where)).WithArgs(args...).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	query := "SELECT " + groupColumns + " FROM member_groups" + where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("tenant-1", "active", "%ops%", "%ops%", 20, 0).WillReturnRows(
		sqlmock.NewRows([]string{"id", "tenant_id", "code", "name", "status", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("group-1", "tenant-1", "ops", "Operations", "active", 1, now, now, "admin", "admin"),
	)
	items, total, err := repository.SearchGroups(t.Context(), "tenant-1", "ops", "active", 20, 0)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("SearchGroups() = (%+v, %d, %v)", items, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestService_CreateInjectsAuditActor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	repository := &fakeRepository{}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	service.now = func() time.Time { return now }
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser})
	created, owner, err := service.Create(ctx, " ACME ", "Acme", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.Code != "acme" || created.Version != 1 || created.CreatedBy != "admin-1" || owner.CreatedBy != "admin-1" {
		t.Fatalf("created tenant=%+v owner=%+v", created, owner)
	}
	if len(repository.outbox) != 1 || repository.outbox[0].Subject != "platform.tenant.tenant.created.v1" {
		t.Fatalf("outbox = %+v", repository.outbox)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestService_CreateReplaysIdempotentResult(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	cfg := config.Config{Idempotency: config.Idempotency{Enabled: true, ProcessingTTL: time.Minute, ResultTTL: time.Hour, FailureTTL: time.Minute}}
	service := NewRuntimeService(&fakeRepository{}, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil, idempotency.New(client, cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := idempotency.WithContext(principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser}), "tenant-create-0001")
	firstTenant, firstOwner, err := service.Create(ctx, "acme", "Acme", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	secondTenant, secondOwner, err := service.Create(ctx, "acme", "Acme", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if secondTenant.ID != firstTenant.ID || secondOwner.ID != firstOwner.ID {
		t.Fatalf("replay changed result: first=(%s,%s) second=(%s,%s)", firstTenant.ID, firstOwner.ID, secondTenant.ID, secondOwner.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestService_UpdateMapsStaleVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectRollback()
	service := NewService(&fakeRepository{tenant: Tenant{ID: "tenant-1", Status: "active"}, updateErr: ErrStaleVersion}, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	_, err = service.Update(ctx, "tenant-1", "Acme", "active", 1)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeStaleVersion {
		t.Fatalf("Update() error = %v, want stale-version code", err)
	}
}

func TestService_CreateRequiresActor(t *testing.T) {
	service := NewService(&fakeRepository{}, &database.Transactor{}, nil)
	_, _, err := service.Create(t.Context(), "acme", "Acme", "user-1")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Fatalf("Create() error = %v, want unauthorized", err)
	}
}

func TestNullableID(t *testing.T) {
	t.Parallel()
	if nullableID("") != nil {
		t.Fatal("nullableID(empty) != nil")
	}
	if got := nullableID("org-1"); got != "org-1" {
		t.Fatalf("nullableID(org-1) = %v", got)
	}
}

func TestService_CreateOrganizationUnitBuildsMaterializedPath(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	repository := &fakeRepository{tenant: Tenant{ID: "tenant-1", Status: "active"}}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	created, err := service.CreateOrganizationUnit(ctx, "tenant-1", "", "HEAD", "Headquarters")
	if err != nil {
		t.Fatal(err)
	}
	if created.Code != "head" || created.Path != "/"+created.ID+"/" || len(repository.outbox) != 1 {
		t.Fatalf("created=%+v outbox=%v", created, repository.outbox)
	}
}

func TestService_UpdateOrganizationUnitRejectsCycle(t *testing.T) {
	current := OrganizationUnit{ID: "root", TenantID: "tenant-1", Path: "/root/", Status: "active", Version: 1}
	child := OrganizationUnit{ID: "child", TenantID: "tenant-1", ParentID: "root", Path: "/root/child/", Status: "active", Version: 1}
	repository := &fakeRepository{organizations: map[string]OrganizationUnit{"root": current, "child": child}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	_, err := service.UpdateOrganizationUnit(ctx, "root", "child", "Root", "active", 1)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("UpdateOrganizationUnit() error = %v", err)
	}
}

func TestDescendantMoveSQL_PostgresCastsSubstringOffset(t *testing.T) {
	t.Parallel()
	if !strings.Contains(descendantMoveSQL("pgx"), "CAST(? AS INTEGER)") {
		t.Fatalf("postgres move SQL = %q", descendantMoveSQL("pgx"))
	}
}

func TestCreateGroupMemberSQL_HasOnePlaceholderPerColumn(t *testing.T) {
	t.Parallel()
	columnCount := strings.Count(groupMemberColumns, ",") + 1
	if placeholderCount := strings.Count(createGroupMemberSQL(), "?"); placeholderCount != columnCount {
		t.Fatalf("group member insert has %d placeholders for %d columns: %s", placeholderCount, columnCount, createGroupMemberSQL())
	}
}

func TestService_CreateInvitationStoresOnlyTokenHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	repository := &fakeRepository{tenant: Tenant{ID: "tenant-1", Status: "active"}}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	invitation, token, err := service.CreateInvitation(ctx, "tenant-1", "MEMBER@example.com", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || len(invitation.TokenHash) != 64 || invitation.TokenHash == token || invitation.TokenHash != hashToken(token) {
		t.Fatalf("token/hash were not stored safely: token=%q hash=%q", token, invitation.TokenHash)
	}
	if invitation.Email != "member@example.com" || repository.invitation.TokenHash != invitation.TokenHash {
		t.Fatalf("invitation = %+v, repository = %+v", invitation, repository.invitation)
	}
	if len(repository.outbox) != 1 || repository.outbox[0].Subject != "platform.tenant.invitation.changed.v1" {
		t.Fatalf("outbox = %+v", repository.outbox)
	}
}

func TestService_AcceptInvitationRejectsDifferentUser(t *testing.T) {
	service := NewService(&fakeRepository{}, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser})
	_, _, err := service.AcceptInvitation(ctx, "valid-looking-token", "user-2")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeForbidden {
		t.Fatalf("AcceptInvitation() error = %v, want forbidden", err)
	}
}

func TestService_ListUserTenantsRejectsDifferentUser(t *testing.T) {
	t.Parallel()
	service := NewService(&fakeRepository{}, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser})
	_, err := service.ListUserTenants(ctx, "user-2", 1, 20)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeForbidden {
		t.Fatalf("ListUserTenants() error = %v, want forbidden", err)
	}
}

func TestService_ConsumeQuotaHonorsLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &fakeRepository{quota: Quota{TenantID: "tenant-1", Key: "users", Limit: 2, Version: 1}}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	for wantUsed := int64(1); wantUsed <= 2; wantUsed++ {
		mock.ExpectBegin()
		mock.ExpectCommit()
		quota, allowed, consumeErr := service.ConsumeQuota(ctx, "tenant-1", "users", 1)
		if consumeErr != nil || !allowed || quota.Used != wantUsed {
			t.Fatalf("ConsumeQuota() = (%+v, %v, %v), want used=%d", quota, allowed, consumeErr, wantUsed)
		}
	}
	mock.ExpectBegin()
	mock.ExpectCommit()
	quota, allowed, err := service.ConsumeQuota(ctx, "tenant-1", "users", 1)
	if err != nil || allowed || quota.Used != 2 {
		t.Fatalf("exhausted ConsumeQuota() = (%+v, %v, %v)", quota, allowed, err)
	}
}

func TestService_ListQuotas(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{quota: Quota{TenantID: "tenant-1", Key: "users", Limit: 100, Used: 2, Version: 1}}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	page, err := service.ListQuotas(ctx, " tenant-1 ", " user ", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Page != 1 || page.PageSize != 20 || len(page.Quotas) != 1 {
		t.Fatalf("ListQuotas() = %+v", page)
	}
}

func TestService_AddGroupMemberReactivatesRemovedAssignment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	repository := &fakeRepository{
		group:       Group{ID: "group-1", TenantID: "tenant-1", Status: "active"},
		membership:  Membership{ID: "membership-1", TenantID: "tenant-1", Status: "active"},
		groupMember: GroupMember{ID: "group-member-1", TenantID: "tenant-1", GroupID: "group-1", MembershipID: "membership-1", Status: "removed", Version: 2},
	}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")), nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser, TenantID: "tenant-1"})
	if err := service.AddGroupMember(ctx, "group-1", "membership-1"); err != nil {
		t.Fatal(err)
	}
	if repository.groupMember.Status != "active" || repository.groupMember.Version != 3 || repository.groupMember.UpdatedBy != "admin-1" {
		t.Fatalf("group member = %+v", repository.groupMember)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestService_AddGroupMemberIsIdempotentWhenActive(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{
		group:       Group{ID: "group-1", TenantID: "tenant-1", Status: "active"},
		membership:  Membership{ID: "membership-1", TenantID: "tenant-1", Status: "active"},
		groupMember: GroupMember{ID: "group-member-1", TenantID: "tenant-1", GroupID: "group-1", MembershipID: "membership-1", Status: "active", Version: 1},
	}
	service := NewService(repository, &database.Transactor{}, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount})
	if err := service.AddGroupMember(ctx, "group-1", "membership-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLRepository_ListQuotasFiltersByTenantAndKeyword(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := &SQLRepository{db: sqlx.NewDb(db, "sqlmock")}
	countQuery := "SELECT COUNT(*) FROM tenant_quotas WHERE tenant_id = ? AND LOWER(quota_key) LIKE LOWER(?)"
	listQuery := "SELECT " + quotaColumns + " FROM tenant_quotas WHERE tenant_id = ? AND LOWER(quota_key) LIKE LOWER(?) ORDER BY quota_key, tenant_id LIMIT ? OFFSET ?"
	mock.ExpectQuery(regexp.QuoteMeta(countQuery)).WithArgs("tenant-1", "%user%").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	now := time.Date(2026, 8, 31, 22, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	mock.ExpectQuery(regexp.QuoteMeta(listQuery)).WithArgs("tenant-1", "%user%", 20, 0).WillReturnRows(
		sqlmock.NewRows([]string{"tenant_id", "quota_key", "limit_value", "used_value", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("tenant-1", "users", 100, 2, 1, now, now, "admin-1", "admin-1"),
	)
	items, total, err := repository.ListQuotas(t.Context(), "tenant-1", "user", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Key != "users" {
		t.Fatalf("ListQuotas() items=%+v total=%d", items, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
