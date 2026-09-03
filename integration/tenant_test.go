//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/eventbus"
	platformoutbox "github.com/lihongjie0209/microservice-platform-go/outbox"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	tenantv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/tenant/v1"
	"github.com/lihongjie0209/tenant-service/internal/config"
	appdb "github.com/lihongjie0209/tenant-service/internal/database"
	"github.com/lihongjie0209/tenant-service/internal/migration"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"
	"google.golang.org/protobuf/proto"
)

type recordingPublisher struct{ ids []string }

func (p *recordingPublisher) Publish(_ context.Context, _ string, envelope *commonv1.EventEnvelope) error {
	p.ids = append(p.ids, envelope.GetEventId())
	return nil
}

func TestTenantRepositoryCompatibility(t *testing.T) {
	for _, databaseType := range []string{"postgres", "mysql"} {
		t.Run(databaseType, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			dsn, migrationURL := startDatabase(t, ctx, databaseType)
			path, err := filepath.Abs(filepath.Join("..", "migrations", databaseType))
			if err != nil {
				t.Fatal(err)
			}
			schema := ""
			if databaseType == "postgres" {
				schema = "tenant_domain"
			}
			migrationCfg := config.Migration{Path: path, DatabaseURL: migrationURL, Table: "tenant_domain_schema_migrations", Schema: schema, CreateSchema: schema != ""}
			if err := migration.Run(migrationCfg, "up", 0); err != nil {
				t.Fatal(err)
			}
			db, err := appdb.Open(ctx, config.Database{Type: databaseType, DSN: dsn, Schema: schema, MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			repository := tenantdomain.NewRepository(db)
			transactor := appdb.NewTransactor(db)
			now := time.Now().Truncate(time.Microsecond)
			tenantValue := tenantdomain.Tenant{ID: uuid.NewString(), Code: "acme-" + databaseType, Name: "Acme", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			membership := tenantdomain.Membership{ID: uuid.NewString(), TenantID: tenantValue.ID, UserID: uuid.NewString(), Status: "active", JoinedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
				if err := repository.CreateTenant(ctx, tx, tenantValue); err != nil {
					return err
				}
				return repository.CreateMembership(ctx, tx, membership)
			}); err != nil {
				t.Fatal(err)
			}
			foundTenant, foundMembership, err := repository.ValidateMembership(ctx, membership.UserID, tenantValue.ID)
			if err != nil || foundTenant.ID != tenantValue.ID || foundMembership.ID != membership.ID {
				t.Fatalf("ValidateMembership() = (%+v, %+v, %v)", foundTenant, foundMembership, err)
			}
			batch, err := repository.BatchGetMemberships(ctx, tenantValue.ID, []string{membership.ID, uuid.NewString()})
			if err != nil || len(batch) != 1 || batch[0].ID != membership.ID {
				t.Fatalf("BatchGetMemberships() = (%+v, %v)", batch, err)
			}
			directoryMemberships, err := repository.FindMembershipsByUserIDs(ctx, tenantValue.ID, []string{membership.UserID, uuid.NewString()}, "active")
			if err != nil || len(directoryMemberships) != 1 || directoryMemberships[0].ID != membership.ID {
				t.Fatalf("FindMembershipsByUserIDs() = (%+v, %v)", directoryMemberships, err)
			}
			tenantValue.Name, tenantValue.UpdatedAt = "Acme 2", now.Add(time.Second)
			if err := repository.UpdateTenant(ctx, db, tenantValue); err != nil {
				t.Fatal(err)
			}
			if err := repository.UpdateTenant(ctx, db, tenantValue); !errors.Is(err, tenantdomain.ErrStaleVersion) {
				t.Fatalf("stale UpdateTenant() error = %v", err)
			}
			rootA := tenantdomain.OrganizationUnit{ID: uuid.NewString(), TenantID: tenantValue.ID, Code: "root-a", Name: "Root A", Path: "", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			rootA.Path = "/" + rootA.ID + "/"
			rootB := tenantdomain.OrganizationUnit{ID: uuid.NewString(), TenantID: tenantValue.ID, Code: "root-b", Name: "Root B", Path: "", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			rootB.Path = "/" + rootB.ID + "/"
			child := tenantdomain.OrganizationUnit{ID: uuid.NewString(), TenantID: tenantValue.ID, ParentID: rootA.ID, Code: "child", Name: "Child", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			child.Path = rootA.Path + child.ID + "/"
			grandchild := tenantdomain.OrganizationUnit{ID: uuid.NewString(), TenantID: tenantValue.ID, ParentID: child.ID, Code: "grandchild", Name: "Grandchild", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			grandchild.Path = child.Path + grandchild.ID + "/"
			for _, unit := range []tenantdomain.OrganizationUnit{rootA, rootB, child, grandchild} {
				if err := repository.CreateOrganizationUnit(ctx, db, unit); err != nil {
					t.Fatal(err)
				}
			}
			oldChildPath := child.Path
			child.ParentID, child.Path, child.UpdatedAt = rootB.ID, rootB.Path+child.ID+"/", now.Add(time.Second)
			if err := repository.UpdateOrganizationUnit(ctx, db, child, oldChildPath); err != nil {
				t.Fatal(err)
			}
			movedGrandchild, err := repository.GetOrganizationUnit(ctx, grandchild.ID)
			if err != nil {
				t.Fatal(err)
			}
			if want := child.Path + grandchild.ID + "/"; movedGrandchild.Path != want {
				t.Fatalf("grandchild path = %q, want %q", movedGrandchild.Path, want)
			}

			invitation := tenantdomain.Invitation{ID: uuid.NewString(), TenantID: tenantValue.ID, Email: "member@example.com", TokenHash: strings.Repeat("a", 64), Status: "pending", ExpiresAt: now.Add(24 * time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := repository.CreateInvitation(ctx, db, invitation); err != nil {
				t.Fatal(err)
			}
			foundInvitation, err := repository.GetInvitationByTokenHash(ctx, invitation.TokenHash)
			if err != nil || foundInvitation.ID != invitation.ID {
				t.Fatalf("GetInvitationByTokenHash() = (%+v, %v)", foundInvitation, err)
			}
			invitation.Status, invitation.AcceptedByUserID, invitation.UpdatedAt = "accepted", membership.UserID, now.Add(time.Second)
			if err := repository.UpdateInvitation(ctx, db, invitation); err != nil {
				t.Fatal(err)
			}
			if err := repository.UpdateInvitation(ctx, db, invitation); !errors.Is(err, tenantdomain.ErrStaleVersion) {
				t.Fatalf("stale UpdateInvitation() error = %v", err)
			}

			group := tenantdomain.Group{ID: uuid.NewString(), TenantID: tenantValue.ID, Code: "operators", Name: "Operators", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := repository.CreateGroup(ctx, db, group); err != nil {
				t.Fatal(err)
			}
			groups, groupTotal, err := repository.SearchGroups(ctx, tenantValue.ID, "oper", "active", 20, 0)
			if err != nil || groupTotal != 1 || len(groups) != 1 || groups[0].ID != group.ID {
				t.Fatalf("SearchGroups() = (%+v, %d, %v)", groups, groupTotal, err)
			}
			groupMember := tenantdomain.GroupMember{ID: uuid.NewString(), TenantID: tenantValue.ID, GroupID: group.ID, MembershipID: membership.ID, Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := repository.CreateGroupMember(ctx, db, groupMember); err != nil {
				t.Fatal(err)
			}
			foundGroupMember, err := repository.GetGroupMember(ctx, group.ID, membership.ID)
			if err != nil || foundGroupMember.ID != groupMember.ID {
				t.Fatalf("GetGroupMember() = (%+v, %v)", foundGroupMember, err)
			}

			quota := tenantdomain.Quota{TenantID: tenantValue.ID, Key: "api_calls", Limit: 5, Used: 0, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := repository.CreateQuota(ctx, db, quota); err != nil {
				t.Fatal(err)
			}
			allowedResults := make(chan bool, 10)
			errorResults := make(chan error, 10)
			for range 10 {
				go func() {
					_, allowed, consumeErr := repository.ConsumeQuota(ctx, db, tenantValue.ID, quota.Key, 1, time.Now(), "worker")
					allowedResults <- allowed
					errorResults <- consumeErr
				}()
			}
			allowedCount := 0
			for range 10 {
				if consumeErr := <-errorResults; consumeErr != nil {
					t.Fatal(consumeErr)
				}
				if <-allowedResults {
					allowedCount++
				}
			}
			finalQuota, err := repository.GetQuota(ctx, tenantValue.ID, quota.Key)
			if err != nil || allowedCount != 5 || finalQuota.Used != 5 || finalQuota.Version != 6 {
				t.Fatalf("concurrent quota: allowed=%d quota=%+v err=%v", allowedCount, finalQuota, err)
			}

			envelope, err := eventbus.NewEnvelope(eventbus.Metadata{EventID: uuid.NewString(), EventType: "platform.tenant.v1.TenantCreated", AggregateID: tenantValue.ID, AggregateType: "tenant", TenantID: tenantValue.ID, SchemaVersion: 1, ActorID: "admin", OccurredAt: now}, &tenantv1.TenantCreatedEvent{Tenant: &tenantv1.Tenant{Id: tenantValue.ID}})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := proto.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			outboxEvent := tenantdomain.OutboxEvent{ID: envelope.GetEventId(), Subject: "platform.tenant.tenant.created.v1", Envelope: encoded, AvailableAt: now, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "admin", UpdatedBy: "admin"}
			if err := repository.AddOutbox(ctx, db, outboxEvent); err != nil {
				t.Fatal(err)
			}
			publisher := &recordingPublisher{}
			outboxStore, err := platformoutbox.NewSQLStore(db, "tenant_outbox_events")
			if err != nil {
				t.Fatal(err)
			}
			dispatcher, err := platformoutbox.New(outboxStore, publisher, platformoutbox.Config{Lease: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			published, err := dispatcher.RunOnce(ctx)
			if err != nil || published != 1 || len(publisher.ids) != 1 || publisher.ids[0] != envelope.GetEventId() {
				t.Fatalf("RunOnce() = (%d, %v), ids=%v", published, err, publisher.ids)
			}
			var publishedAt *time.Time
			if err := db.GetContext(ctx, &publishedAt, db.Rebind("SELECT published_at FROM tenant_outbox_events WHERE id = ?"), envelope.GetEventId()); err != nil || publishedAt == nil {
				t.Fatalf("published_at=%v err=%v", publishedAt, err)
			}
		})
	}
}
