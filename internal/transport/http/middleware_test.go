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
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/tenant-service/internal/auth"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/idempotency"
)

type fakeIdempotencyManager struct {
	decision  idempotency.Decision
	beginKey  string
	completed *Response
}

func (*fakeIdempotencyManager) Enabled() bool { return true }
func (m *fakeIdempotencyManager) Begin(_ context.Context, key, _ string) (idempotency.Decision, error) {
	m.beginKey = key
	return m.decision, nil
}
func (m *fakeIdempotencyManager) Complete(_ context.Context, _, _ string, response any) error {
	value, ok := response.(Response)
	if ok {
		m.completed = &value
	}
	return nil
}
func (*fakeIdempotencyManager) Fail(context.Context, string, string, idempotency.Failure) error {
	return nil
}

func TestIdempotencyExecutionCompletesAndReplaysTenantMutation(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := &fakeIdempotencyManager{decision: idempotency.Decision{State: idempotency.StateAcquired, Owner: "owner-1"}}
	calls := 0
	router := gin.New()
	router.Use(RequestID(), func(c *gin.Context) {
		c.Set("subject", "administrator-1")
		c.Request = c.Request.WithContext(idempotency.WithContext(c.Request.Context(), "operation-1"))
		c.Next()
	}, IdempotencyExecution(manager, []string{"/api/v1/quotas/consume"}, logger))
	router.POST("/api/v1/quotas/consume", func(c *gin.Context) { calls++; OK(c, gin.H{"remaining": 9}) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/quotas/consume", strings.NewReader(`{"amount":1}`)))
	if calls != 1 || manager.beginKey != "operation-1" || manager.completed == nil || manager.completed.RequestID != "" {
		t.Fatalf("calls=%d key=%q completed=%+v", calls, manager.beginKey, manager.completed)
	}
	stored, err := json.Marshal(*manager.completed)
	if err != nil {
		t.Fatal(err)
	}
	manager.decision = idempotency.Decision{State: idempotency.StateCompleted, Response: stored}
	replay := httptest.NewRequest(http.MethodPost, "/api/v1/quotas/consume", strings.NewReader(`{"amount":1}`))
	replay.Header.Set("X-Request-ID", "current-request")
	replayRecorder := httptest.NewRecorder()
	router.ServeHTTP(replayRecorder, replay)
	var response Response
	if err := json.Unmarshal(replayRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || response.RequestID != "current-request" {
		t.Fatalf("calls=%d response=%+v", calls, response)
	}
}

func TestIdempotencyExecutionExcludesTokenAndDomainIdempotencyRoutes(t *testing.T) {
	t.Parallel()
	manager := &fakeIdempotencyManager{decision: idempotency.Decision{State: idempotency.StateConflict}}
	for _, route := range []string{"/api/v1/tenants/create", "/api/v1/tenants/select", "/api/v1/invitations/create", "/api/v1/tenants/list"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			calls := 0
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(idempotency.WithContext(c.Request.Context(), "operation-1"))
				c.Next()
			}, IdempotencyExecution(manager, []string{"/api/v1/quotas/consume"}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			router.POST(route, func(c *gin.Context) { calls++; OK(c, nil) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, route, nil))
			if calls != 1 || recorder.Code != http.StatusOK {
				t.Fatalf("calls=%d status=%d", calls, recorder.Code)
			}
		})
	}
	if manager.beginKey != "" {
		t.Fatalf("unexpected idempotency begin key %q", manager.beginKey)
	}
}

type authorizationStub struct{ err error }

func (a authorizationStub) Authorize(context.Context, principal.Principal, platformauthz.Requirement) error {
	return a.err
}

func TestTenantHTTPRequirementCoverageAndSelfServiceExclusions(t *testing.T) {
	t.Parallel()
	protected := []string{
		"/api/v1/tenants/manage/create", "/api/v1/tenants/get", "/api/v1/tenants/update", "/api/v1/tenants/list", "/api/v1/memberships/add", "/api/v1/memberships/update", "/api/v1/memberships/list",
		"/api/v1/organization-units/create", "/api/v1/organization-units/get", "/api/v1/organization-units/update", "/api/v1/organization-units/list", "/api/v1/organization-units/tree",
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
	for _, route := range []string{"/api/v1/tenants/manage/create", "/api/v1/tenants/get", "/api/v1/tenants/update", "/api/v1/tenants/list"} {
		requirement, _ := tenantHTTPRequirement(route)
		if requirement.Scope != platformauthz.ScopePlatform {
			t.Fatalf("route %q scope = %v, want platform", route, requirement.Scope)
		}
	}
	for _, route := range []string{
		"/api/v1/memberships/list",
		"/api/v1/memberships/batch-get",
		"/api/v1/memberships/directory/search",
		"/api/v1/organization-units/list",
		"/api/v1/organization-units/tree",
		"/api/v1/groups/list",
		"/api/v1/groups/search",
		"/api/v1/groups/members/batch-get",
	} {
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
