package tenant

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
func (f *fakeRepository) GetQuota(context.Context, string, string) (Quota, error) {
	if f.quota.TenantID == "" {
		return Quota{}, ErrNotFound
	}
	return f.quota, nil
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
	created, owner, err := service.Create(ctx, " ACME ", "Acme", "user-1")
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
	firstTenant, firstOwner, err := service.Create(ctx, "acme", "Acme", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	secondTenant, secondOwner, err := service.Create(ctx, "acme", "Acme", "user-1")
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
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser})
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
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser})
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
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser})
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
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin-1", Type: principal.TypeUser})
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
