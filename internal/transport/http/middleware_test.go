package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/auth"
	"github.com/lihongjie0209/tenant-service/internal/config"
)

type authorizationStub struct{ err error }

func (a authorizationStub) Authorize(context.Context, principal.Principal, platformauthz.Requirement) error {
	return a.err
}

func TestTenantHTTPRequirementCoverageAndSelfServiceExclusions(t *testing.T) {
	t.Parallel()
	protected := []string{
		"/api/v1/tenants/get", "/api/v1/tenants/update", "/api/v1/tenants/list", "/api/v1/memberships/add", "/api/v1/memberships/update", "/api/v1/memberships/list",
		"/api/v1/organization-units/create", "/api/v1/organization-units/get", "/api/v1/organization-units/update", "/api/v1/organization-units/list",
		"/api/v1/invitations/create", "/api/v1/invitations/revoke", "/api/v1/invitations/list", "/api/v1/groups/create", "/api/v1/groups/update", "/api/v1/groups/member-add", "/api/v1/groups/member-remove", "/api/v1/groups/members/list", "/api/v1/groups/list",
		"/api/v1/quotas/get", "/api/v1/quotas/list", "/api/v1/quotas/set", "/api/v1/quotas/consume",
	}
	for _, route := range protected {
		if requirement, ok := tenantHTTPRequirement(route); !ok || requirement.Resource == "" || requirement.Action == "" {
			t.Fatalf("route %q requirement = %+v, %v", route, requirement, ok)
		}
	}
	for _, route := range []string{"/api/v1/tenants/create", "/api/v1/tenants/select", "/api/v1/tenants/list-by-user", "/api/v1/invitations/accept", "/api/v1/version", "/api/v1/me"} {
		if _, ok := tenantHTTPRequirement(route); ok {
			t.Fatalf("self-service route %q must not require an existing tenant membership decision", route)
		}
	}
}

func TestTenantHTTPRequirementSeparatesPlatformDirectoryFromTenantResources(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"/api/v1/tenants/get", "/api/v1/tenants/update", "/api/v1/tenants/list"} {
		requirement, _ := tenantHTTPRequirement(route)
		if requirement.Scope != platformauthz.ScopePlatform {
			t.Fatalf("route %q scope = %v, want platform", route, requirement.Scope)
		}
	}
	for _, route := range []string{"/api/v1/memberships/list", "/api/v1/organization-units/list", "/api/v1/groups/list"} {
		requirement, _ := tenantHTTPRequirement(route)
		if requirement.Scope != platformauthz.ScopePrincipal {
			t.Fatalf("route %q scope = %v, want principal-derived", route, requirement.Scope)
		}
	}
}

func TestAuthorizationFailsClosedAndClassifiesOutage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "denied", err: platformauthz.ErrDenied, status: http.StatusForbidden},
		{name: "unavailable", err: platformauthz.ErrDecisionUnavailable, status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID(), func(c *gin.Context) {
				c.Request = c.Request.WithContext(principal.WithContext(c.Request.Context(), principal.Principal{ID: "user-1", Type: principal.TypeUser, TenantID: "tenant-1", MembershipID: "membership-1"}))
				c.Next()
			}, Authorization(true, authorizationStub{err: test.err}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			router.POST("/api/v1/tenants/get", func(c *gin.Context) { OK(c, nil) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/get", nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.POST("/test", func(c *gin.Context) { OK(c, nil) })
	request := httptest.NewRequest(http.MethodPost, "/test", nil)
	request.Header.Set("X-Request-ID", "client-request-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Request-ID"); got != "client-request-1" {
		t.Fatalf("X-Request-ID = %q", got)
	}
	var response Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "client-request-1" {
		t.Fatalf("request_id = %q", response.RequestID)
	}
}

func TestAuthentication_PSKPrecedesSkipAndJWT(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	const key = "01234567890123456789012345678901"
	service := auth.New(config.Config{JWT: config.JWT{Issuer: "test", Secret: key, TTL: time.Hour}})
	for _, test := range []struct {
		name   string
		header string
		status int
	}{
		{name: "valid PSK", header: "PSK " + key, status: http.StatusOK},
		{name: "PSK route does not become public", status: http.StatusUnauthorized},
		{name: "bearer cannot access PSK route", header: "Bearer invalid", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := gin.New()
			router.Use(RequestID(), Authentication(service, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Auth{
				SkipHTTPPaths: []string{"/api/v1/external/*"},
				PSK:           config.PSK{Enabled: true, Key: key, HTTPPaths: []string{"/api/v1/external/*"}},
			}))
			router.POST("/api/v1/external/callback", func(c *gin.Context) { OK(c, nil) })
			request := httptest.NewRequest(http.MethodPost, "/api/v1/external/callback", nil)
			request.Header.Set("Authorization", test.header)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestRequireJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), RequireJSON())
	router.POST("/test", func(c *gin.Context) { OK(c, nil) })
	request := httptest.NewRequest(http.MethodPost, "/test", io.NopCloser(&oneByteReader{}))
	request.ContentLength = 1
	request.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestTimeoutPropagatesCancellation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gin.New()
	router.Use(RequestID(), Timeout(time.Millisecond, logger))
	router.POST("/test", func(c *gin.Context) { <-c.Request.Context().Done() })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/test", nil))
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
}

type oneByteReader struct{}

func (*oneByteReader) Read(buffer []byte) (int, error) { buffer[0] = 'x'; return 1, io.EOF }
