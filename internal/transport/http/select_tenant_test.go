package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/identityclient"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"
)

type membershipRepositoryStub struct {
	tenantdomain.Repository
	membership tenantdomain.Membership
}

func (r membershipRepositoryStub) ValidateMembership(context.Context, string, string) (tenantdomain.Tenant, tenantdomain.Membership, error) {
	return tenantdomain.Tenant{ID: r.membership.TenantID}, r.membership, nil
}

type tokenIssuerStub struct {
	userID, tenantID, membershipID, sessionID string
}

func (s *tokenIssuerStub) IssueTenantToken(_ context.Context, userID, tenantID, membershipID, sessionID string) (string, time.Time, error) {
	s.userID, s.tenantID, s.membershipID, s.sessionID = userID, tenantID, membershipID, sessionID
	return "tenant-token", time.Unix(100, 0), nil
}

func TestSelectTenantValidatesMembershipBeforeIssuingToken(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	service := tenantdomain.NewService(membershipRepositoryStub{membership: tenantdomain.Membership{ID: "membership-1", TenantID: "tenant-1", UserID: "user-1", Status: "active"}}, nil, nil)
	issuer := &tokenIssuerStub{}
	handler := &Handler{tenants: service, issuer: identityclient.Issuer(issuer), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	router := gin.New()
	router.Use(RequestID(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(principal.WithContext(c.Request.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser, SessionID: "session-1"}))
		c.Next()
	})
	router.POST("/api/v1/tenants/select", handler.SelectTenant)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/select", strings.NewReader(`{"tenant_id":"tenant-1"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Body SelectTenantResponseBody `json:"body"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Body.AccessToken != "tenant-token" || response.Body.MembershipID != "membership-1" {
		t.Fatalf("response = %+v", response.Body)
	}
	if issuer.userID != "user-1" || issuer.tenantID != "tenant-1" || issuer.membershipID != "membership-1" || issuer.sessionID != "session-1" {
		t.Fatalf("issuer scope = %+v", issuer)
	}
}
