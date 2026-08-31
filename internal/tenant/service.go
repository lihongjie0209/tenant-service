package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/audit"
	"github.com/lihongjie0209/microservice-platform-go/eventbus"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	tenantv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/tenant/v1"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
	"github.com/lihongjie0209/tenant-service/internal/cache"
	"github.com/lihongjie0209/tenant-service/internal/database"
	"github.com/lihongjie0209/tenant-service/internal/idempotency"
	"go.uber.org/fx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	repository  Repository
	transactor  *database.Transactor
	now         func() time.Time
	locker      *cache.Locker
	idempotency *idempotency.Manager
	logger      *slog.Logger
}

func NewService(repository Repository, transactor *database.Transactor, locker *cache.Locker) *Service {
	return &Service{repository: repository, transactor: transactor, locker: locker, now: time.Now}
}

func NewRuntimeService(repository Repository, transactor *database.Transactor, locker *cache.Locker, manager *idempotency.Manager, logger *slog.Logger) *Service {
	service := NewService(repository, transactor, locker)
	service.idempotency, service.logger = manager, logger
	return service
}

type createResult struct {
	Tenant     Tenant     `json:"tenant"`
	Membership Membership `json:"owner_membership"`
}

func (s *Service) Create(ctx context.Context, code, name, ownerUserID string) (Tenant, Membership, error) {
	code, name, ownerUserID = strings.ToLower(strings.TrimSpace(code)), strings.TrimSpace(name), strings.TrimSpace(ownerUserID)
	if code == "" || name == "" || ownerUserID == "" {
		return Tenant{}, Membership{}, apperror.Invalid("code, name and owner_user_id are required", nil)
	}
	key, hasKey := idempotency.FromContext(ctx)
	if !hasKey {
		return s.createOnce(ctx, code, name, ownerUserID)
	}
	if s.idempotency == nil || !s.idempotency.Enabled() {
		return Tenant{}, Membership{}, apperror.Unavailable("idempotency is unavailable", nil)
	}
	fingerprint := sha256.Sum256([]byte("tenant.create\x00" + code + "\x00" + name + "\x00" + ownerUserID))
	decision, err := s.idempotency.Begin(ctx, key, hex.EncodeToString(fingerprint[:]))
	if err != nil {
		return Tenant{}, Membership{}, apperror.Unavailable("idempotency is unavailable", err)
	}
	switch decision.State {
	case idempotency.StateCompleted:
		var replay createResult
		if err := json.Unmarshal(decision.Response, &replay); err != nil {
			return Tenant{}, Membership{}, apperror.Internal(err)
		}
		return replay.Tenant, replay.Membership, nil
	case idempotency.StateFailed:
		return Tenant{}, Membership{}, apperror.New(decision.Failure.Code, decision.Failure.Message, decision.Failure.HTTPStatus, nil)
	case idempotency.StateProcessing:
		return Tenant{}, Membership{}, apperror.RequestInProgress()
	case idempotency.StateConflict:
		return Tenant{}, Membership{}, apperror.Conflict("idempotency key was used with a different request", nil)
	case idempotency.StateAcquired:
	default:
		return Tenant{}, Membership{}, apperror.Internal(errors.New("unknown idempotency state"))
	}
	created, membership, createErr := s.createOnce(ctx, code, name, ownerUserID)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if createErr != nil {
		var appErr *apperror.Error
		if !errors.As(createErr, &appErr) {
			appErr = apperror.Internal(createErr)
		}
		if err := s.idempotency.Fail(finishCtx, key, decision.Owner, idempotency.Failure{Code: appErr.Code, Message: appErr.Message, HTTPStatus: appErr.HTTPStatus}); err != nil && s.logger != nil {
			s.logger.WarnContext(finishCtx, "store tenant idempotency failure failed", "error", err)
		}
		return Tenant{}, Membership{}, createErr
	}
	if err := s.idempotency.Complete(finishCtx, key, decision.Owner, createResult{Tenant: created, Membership: membership}); err != nil && s.logger != nil {
		s.logger.WarnContext(finishCtx, "store tenant idempotency result failed", "error", err)
	}
	return created, membership, nil
}

func (s *Service) createOnce(ctx context.Context, code, name, ownerUserID string) (Tenant, Membership, error) {
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return Tenant{}, Membership{}, apperror.Unauthorized("authenticated actor is required")
	}
	tenantValue := Tenant{ID: uuid.NewString(), Code: code, Name: name, Status: "active", Version: fields.Version, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	membership := Membership{ID: uuid.NewString(), TenantID: tenantValue.ID, UserID: ownerUserID, Status: "active", JoinedAt: fields.CreatedAt, Version: fields.Version, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	event, err := newOutboxEvent(ctx, "platform.tenant.tenant.created.v1", "platform.tenant.v1.TenantCreated", tenantValue.ID, tenantValue.ID, fields.CreatedAt, &tenantv1.TenantCreatedEvent{Tenant: eventTenant(tenantValue), OwnerMembershipId: membership.ID, OwnerUserId: membership.UserID})
	if err != nil {
		return Tenant{}, Membership{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.CreateTenant(ctx, tx, tenantValue); err != nil {
			return err
		}
		if err := s.repository.CreateMembership(ctx, tx, membership); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	if err != nil {
		return Tenant{}, Membership{}, translate(err)
	}
	return tenantValue, membership, nil
}

func (s *Service) Get(ctx context.Context, id string) (Tenant, error) {
	value, err := s.repository.GetTenant(ctx, strings.TrimSpace(id))
	return value, translate(err)
}

func (s *Service) Update(ctx context.Context, id, name, status string, version int64) (Tenant, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" || version < 1 || !validTenantStatus(status) {
		return Tenant{}, apperror.Invalid("invalid tenant update", nil)
	}
	actor, now, err := audit.UpdatedBy(ctx, s.now())
	if err != nil {
		return Tenant{}, apperror.Unauthorized("authenticated actor is required")
	}
	current, err := s.repository.GetTenant(ctx, id)
	if err != nil {
		return Tenant{}, translate(err)
	}
	value := Tenant{ID: id, Name: strings.TrimSpace(name), Status: status, Version: version, UpdatedAt: now, UpdatedBy: actor}
	event, err := newOutboxEvent(ctx, "platform.tenant.tenant.status-changed.v1", "platform.tenant.v1.TenantStatusChanged", id, id, now, &tenantv1.TenantStatusChangedEvent{TenantId: id, PreviousStatus: tenantStatusEvent(current.Status), CurrentStatus: tenantStatusEvent(status)})
	if err != nil {
		return Tenant{}, apperror.Internal(err)
	}
	if err := s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.UpdateTenant(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	}); err != nil {
		return Tenant{}, translate(err)
	}
	return s.repository.GetTenant(ctx, id)
}

func (s *Service) AddMembership(ctx context.Context, tenantID, userID, organizationUnitID string) (Membership, error) {
	tenantID, userID, organizationUnitID = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(organizationUnitID)
	if tenantID == "" || userID == "" {
		return Membership{}, apperror.Invalid("tenant_id and user_id are required", nil)
	}
	if err := s.validateMembershipOrganization(ctx, tenantID, organizationUnitID); err != nil {
		return Membership{}, err
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return Membership{}, apperror.Unauthorized("authenticated actor is required")
	}
	value := Membership{ID: uuid.NewString(), TenantID: tenantID, UserID: userID, Status: "active", PrimaryOrganizationUnitID: organizationUnitID, JoinedAt: fields.CreatedAt, Version: 1, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	event, eventErr := newOutboxEvent(ctx, "platform.tenant.membership.changed.v1", "platform.tenant.v1.MembershipChanged", value.ID, tenantID, fields.CreatedAt, &tenantv1.MembershipChangedEvent{MembershipId: value.ID, TenantId: tenantID, UserId: userID, CurrentStatus: tenantv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE})
	if eventErr != nil {
		return Membership{}, apperror.Internal(eventErr)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.CreateMembership(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	return value, translate(err)
}

func (s *Service) GetMembership(ctx context.Context, id string) (Membership, error) {
	value, err := s.repository.GetMembership(ctx, id)
	return value, translate(err)
}
func (s *Service) ValidateMembership(ctx context.Context, userID, tenantID string) (Tenant, Membership, bool) {
	t, m, err := s.repository.ValidateMembership(ctx, userID, tenantID)
	return t, m, err == nil
}
func (s *Service) ListUserTenants(ctx context.Context, userID string, page, pageSize int) (Page, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return Page{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.ListUserTenants(ctx, userID, pageSize, (page-1)*pageSize)
	return Page{Tenants: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}
func (s *Service) ListMemberships(ctx context.Context, tenantID, userID, status string, page, pageSize int) (MembershipPage, error) {
	tenantID, userID, status = strings.TrimSpace(tenantID), strings.TrimSpace(userID), strings.TrimSpace(status)
	if tenantID == "" || (status != "" && !validMembershipStatus(status)) {
		return MembershipPage{}, apperror.Invalid("invalid membership query", nil)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return MembershipPage{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.ListMemberships(ctx, tenantID, userID, status, pageSize, (page-1)*pageSize)
	return MembershipPage{Memberships: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}
func (s *Service) ListTenants(ctx context.Context, keyword, status string, page, pageSize int) (Page, error) {
	keyword, status = strings.TrimSpace(keyword), strings.TrimSpace(status)
	if status != "" && !validTenantStatus(status) {
		return Page{}, apperror.Invalid("invalid tenant status", nil)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		return Page{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	items, total, err := s.repository.ListTenants(ctx, keyword, status, pageSize, (page-1)*pageSize)
	return Page{Tenants: items, Total: total, Page: page, PageSize: pageSize}, translate(err)
}
func (s *Service) UpdateMembership(ctx context.Context, id, status, organizationUnitID string, version int64) (Membership, error) {
	id, organizationUnitID = strings.TrimSpace(id), strings.TrimSpace(organizationUnitID)
	if id == "" || version < 1 || !validMembershipStatus(status) {
		return Membership{}, apperror.Invalid("invalid membership update", nil)
	}
	actor, now, err := audit.UpdatedBy(ctx, s.now())
	if err != nil {
		return Membership{}, apperror.Unauthorized("authenticated actor is required")
	}
	current, err := s.repository.GetMembership(ctx, id)
	if err != nil {
		return Membership{}, translate(err)
	}
	if err := s.validateMembershipOrganization(ctx, current.TenantID, organizationUnitID); err != nil {
		return Membership{}, err
	}
	value := Membership{ID: id, Status: status, PrimaryOrganizationUnitID: organizationUnitID, Version: version, UpdatedAt: now, UpdatedBy: actor}
	event, err := newOutboxEvent(ctx, "platform.tenant.membership.changed.v1", "platform.tenant.v1.MembershipChanged", id, current.TenantID, now, &tenantv1.MembershipChangedEvent{MembershipId: id, TenantId: current.TenantID, UserId: current.UserID, PreviousStatus: membershipStatusEvent(current.Status), CurrentStatus: membershipStatusEvent(status)})
	if err != nil {
		return Membership{}, apperror.Internal(err)
	}
	if err := s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.UpdateMembership(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	}); err != nil {
		return Membership{}, translate(err)
	}
	return s.repository.GetMembership(ctx, id)
}

func (s *Service) validateMembershipOrganization(ctx context.Context, tenantID, organizationUnitID string) error {
	if organizationUnitID == "" {
		return nil
	}
	organization, err := s.repository.GetOrganizationUnit(ctx, organizationUnitID)
	if err != nil {
		return translate(err)
	}
	if organization.TenantID != tenantID || organization.Status != "active" {
		return apperror.Invalid("primary organization unit must be active in this tenant", nil)
	}
	return nil
}

func (s *Service) CreateOrganizationUnit(ctx context.Context, tenantID, parentID, code, name string) (OrganizationUnit, error) {
	tenantID, parentID, code, name = strings.TrimSpace(tenantID), strings.TrimSpace(parentID), strings.ToLower(strings.TrimSpace(code)), strings.TrimSpace(name)
	if tenantID == "" || code == "" || name == "" {
		return OrganizationUnit{}, apperror.Invalid("tenant_id, code and name are required", nil)
	}
	if _, err := s.repository.GetTenant(ctx, tenantID); err != nil {
		return OrganizationUnit{}, translate(err)
	}
	fields, err := audit.New(ctx, s.now())
	if err != nil {
		return OrganizationUnit{}, apperror.Unauthorized("authenticated actor is required")
	}
	id := uuid.NewString()
	pathValue := "/" + id + "/"
	if parentID != "" {
		parent, err := s.repository.GetOrganizationUnit(ctx, parentID)
		if err != nil {
			return OrganizationUnit{}, translate(err)
		}
		if parent.TenantID != tenantID || parent.Status != "active" {
			return OrganizationUnit{}, apperror.Invalid("parent organization unit is not active in this tenant", nil)
		}
		pathValue = parent.Path + id + "/"
	}
	value := OrganizationUnit{ID: id, TenantID: tenantID, ParentID: parentID, Code: code, Name: name, Path: pathValue, Status: "active", Version: 1, CreatedAt: fields.CreatedAt, UpdatedAt: fields.UpdatedAt, CreatedBy: fields.CreatedBy, UpdatedBy: fields.UpdatedBy}
	event, err := newOutboxEvent(ctx, "platform.tenant.organization-unit.changed.v1", "platform.tenant.v1.OrganizationUnitChanged", id, tenantID, fields.CreatedAt, &tenantv1.OrganizationUnitChangedEvent{OrganizationUnit: eventOrganizationUnit(value), ChangeType: "created"})
	if err != nil {
		return OrganizationUnit{}, apperror.Internal(err)
	}
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
		if err := s.repository.CreateOrganizationUnit(ctx, tx, value); err != nil {
			return err
		}
		return s.repository.AddOutbox(ctx, tx, event)
	})
	return value, translate(err)
}

func (s *Service) GetOrganizationUnit(ctx context.Context, id string) (OrganizationUnit, error) {
	value, err := s.repository.GetOrganizationUnit(ctx, strings.TrimSpace(id))
	return value, translate(err)
}

func (s *Service) ListOrganizationUnits(ctx context.Context, tenantID string) ([]OrganizationUnit, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, apperror.Invalid("tenant_id is required", nil)
	}
	items, err := s.repository.ListOrganizationUnits(ctx, tenantID)
	return items, translate(err)
}

func (s *Service) UpdateOrganizationUnit(ctx context.Context, id, parentID, name, status string, version int64) (result OrganizationUnit, resultErr error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" || version < 1 || (status != "active" && status != "disabled") {
		return OrganizationUnit{}, apperror.Invalid("invalid organization unit update", nil)
	}
	current, err := s.repository.GetOrganizationUnit(ctx, id)
	if err != nil {
		return OrganizationUnit{}, translate(err)
	}
	err = s.withOrganizationLock(ctx, current.TenantID, func() error {
		current, err = s.repository.GetOrganizationUnit(ctx, id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrStaleVersion
		}
		newPath := "/" + id + "/"
		if parentID != "" {
			parent, parentErr := s.repository.GetOrganizationUnit(ctx, parentID)
			if parentErr != nil {
				return parentErr
			}
			if parent.TenantID != current.TenantID || parent.Status != "active" {
				return apperror.Invalid("parent organization unit is not active in this tenant", nil)
			}
			if parent.ID == current.ID || strings.HasPrefix(parent.Path, current.Path) {
				return apperror.Invalid("organization unit cannot be moved below itself", nil)
			}
			newPath = parent.Path + id + "/"
		}
		actor, now, auditErr := audit.UpdatedBy(ctx, s.now())
		if auditErr != nil {
			return auditErr
		}
		updated := current
		updated.ParentID, updated.Name, updated.Status, updated.Path = parentID, strings.TrimSpace(name), status, newPath
		updated.UpdatedAt, updated.UpdatedBy = now, actor
		event, eventErr := newOutboxEvent(ctx, "platform.tenant.organization-unit.changed.v1", "platform.tenant.v1.OrganizationUnitChanged", id, current.TenantID, now, &tenantv1.OrganizationUnitChangedEvent{OrganizationUnit: eventOrganizationUnit(updated), PreviousParentId: current.ParentID, ChangeType: "updated"})
		if eventErr != nil {
			return eventErr
		}
		if txErr := s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error {
			if updateErr := s.repository.UpdateOrganizationUnit(ctx, tx, updated, current.Path); updateErr != nil {
				return updateErr
			}
			return s.repository.AddOutbox(ctx, tx, event)
		}); txErr != nil {
			return txErr
		}
		result, err = s.repository.GetOrganizationUnit(ctx, id)
		return err
	})
	if err != nil {
		return OrganizationUnit{}, translate(err)
	}
	return result, nil
}

func (s *Service) ResolveOrganizationScope(ctx context.Context, membershipID string) ([]string, error) {
	membership, err := s.repository.GetMembership(ctx, membershipID)
	if err != nil {
		return nil, translate(err)
	}
	if membership.PrimaryOrganizationUnitID == "" {
		return []string{}, nil
	}
	unit, err := s.repository.GetOrganizationUnit(ctx, membership.PrimaryOrganizationUnitID)
	if err != nil {
		return nil, translate(err)
	}
	ids, err := s.repository.ResolveOrganizationScope(ctx, membership.TenantID, unit.Path)
	return ids, translate(err)
}

func (s *Service) withOrganizationLock(ctx context.Context, tenantID string, operation func() error) error {
	if s.locker == nil {
		return operation()
	}
	lock, err := s.locker.Lock(ctx, "tenant:organization-tree:"+tenantID, 15*time.Second, 50*time.Millisecond)
	if err != nil {
		return err
	}
	operationErr := operation()
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return errors.Join(operationErr, lock.Unlock(unlockCtx))
}

func translate(err error) error {
	if err == nil {
		return nil
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return apperror.NotFound("tenant resource not found")
	case errors.Is(err, ErrConflict):
		return apperror.Conflict("tenant resource already exists", err)
	case uniqueViolation(err):
		return apperror.Conflict("tenant resource already exists", err)
	case errors.Is(err, ErrStaleVersion):
		return apperror.StaleVersion(err)
	default:
		return apperror.Internal(err)
	}
}
func uniqueViolation(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
func validTenantStatus(value string) bool {
	switch value {
	case "pending", "active", "suspended", "closing", "closed":
		return true
	}
	return false
}
func validMembershipStatus(value string) bool {
	switch value {
	case "invited", "active", "disabled", "removed":
		return true
	}
	return false
}

func newOutboxEvent(ctx context.Context, subject, eventType, aggregateID, tenantID string, occurredAt time.Time, payload proto.Message) (OutboxEvent, error) {
	actor, err := principal.Require(ctx)
	if err != nil {
		return OutboxEvent{}, err
	}
	id := uuid.NewString()
	envelope, err := eventbus.NewEnvelope(eventbus.Metadata{
		EventID: id, EventType: eventType, AggregateID: aggregateID,
		AggregateType: "tenant", TenantID: tenantID, SchemaVersion: 1,
		ActorID: actor.ID, ActorType: string(actor.Type), OccurredAt: occurredAt,
	}, payload)
	if err != nil {
		return OutboxEvent{}, err
	}
	encoded, err := proto.Marshal(envelope)
	if err != nil {
		return OutboxEvent{}, err
	}
	return OutboxEvent{ID: id, Subject: subject, Envelope: encoded, AvailableAt: occurredAt, Version: 1, CreatedAt: occurredAt, UpdatedAt: occurredAt, CreatedBy: actor.ID, UpdatedBy: actor.ID}, nil
}

func eventTenant(value Tenant) *tenantv1.Tenant {
	return &tenantv1.Tenant{Id: value.ID, Code: value.Code, Name: value.Name, Status: tenantStatusEvent(value.Status), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func eventOrganizationUnit(value OrganizationUnit) *tenantv1.OrganizationUnit {
	return &tenantv1.OrganizationUnit{Id: value.ID, TenantId: value.TenantID, ParentId: value.ParentID, Code: value.Code, Name: value.Name, Path: value.Path, Status: value.Status, Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func tenantStatusEvent(value string) tenantv1.TenantStatus {
	return tenantv1.TenantStatus(tenantv1.TenantStatus_value["TENANT_STATUS_"+strings.ToUpper(value)])
}

func membershipStatusEvent(value string) tenantv1.MembershipStatus {
	return tenantv1.MembershipStatus(tenantv1.MembershipStatus_value["MEMBERSHIP_STATUS_"+strings.ToUpper(value)])
}

var Module = fx.Module("tenant", fx.Provide(NewRepository, NewRuntimeService, NewDictionaryProvider))
