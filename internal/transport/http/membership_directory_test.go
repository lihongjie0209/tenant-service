package httptransport

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/database"
	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"
)

type directoryStub struct {
	requestedPage int
}

func (s *directoryStub) ListUsers(_ context.Context, _ string, page, pageSize int) (identityclient.UserPage, error) {
	s.requestedPage = page
	return identityclient.UserPage{
		Users:    []identityclient.User{{ID: "user-1", Username: "alice", DisplayName: "Alice", Status: "active"}},
		Total:    1,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

type directoryRepository struct {
	tenantdomain.Repository
}

func (r *directoryRepository) FindMembershipsByUserIDs(_ context.Context, tenantID string, userIDs []string, status string) ([]tenantdomain.Membership, error) {
	return []tenantdomain.Membership{{ID: "membership-1", TenantID: tenantID, UserID: userIDs[0], Status: status, Version: 1}}, nil
}

func TestSearchMembershipDirectoryJoinsIdentityOverServiceBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	directory := &directoryStub{}
	service := tenantdomain.NewService(&directoryRepository{}, &database.Transactor{}, nil)
	handler := &Handler{
		tenants:  service,
		identity: directory,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/memberships/directory/search", bytes.NewBufferString(`{"tenant_id":"tenant-1","keyword":"ali","limit":20}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(principal.WithContext(request.Context(), principal.Principal{ID: "service-1", Type: principal.TypeServiceAccount}))
	ctx.Request = request

	handler.SearchMembershipDirectory(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if directory.requestedPage != 1 || !bytes.Contains(recorder.Body.Bytes(), []byte(`"membership-1"`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"alice"`)) {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}
