package httptransport

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	stdpprof "net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	docs "github.com/lihongjie0209/tenant-service/docs"
	"github.com/lihongjie0209/tenant-service/internal/auth"
	"github.com/lihongjie0209/tenant-service/internal/buildinfo"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/health"
	"github.com/lihongjie0209/tenant-service/internal/idempotency"
	"github.com/lihongjie0209/tenant-service/internal/observability"
	"github.com/lihongjie0209/tenant-service/internal/ratelimit"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/fx"
)

func NewServer(lc fx.Lifecycle, cfg config.Config, handler *Handler, authService *auth.Service, authorizer platformauthz.Authorizer, limiter *ratelimit.Limiter, idempotencyManager *idempotency.Manager, metrics *observability.Metrics, tracing *observability.Tracing, logger *slog.Logger) (*http.Server, error) {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if err := router.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}
	_ = tracing
	router.Use(RequestID(), IdempotencyKey(logger), Environment(cfg.Runtime.ActiveProfile), otelgin.Middleware(cfg.App.Name), RequestLogger(logger), Recovery(logger), HTTPMetrics(metrics), SecurityHeaders(), CORS(cfg.HTTP.CORS), MaxBody(cfg.HTTP.MaxBodyBytes), Timeout(cfg.HTTP.RequestTimeout, logger), RequireJSON())
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		router.Handle(method, "/live", handler.Live)
		router.Handle(method, "/ready", handler.Ready)
	}
	if metrics.Enabled() {
		router.GET("/metrics", gin.WrapH(metrics.Handler()))
	}
	if cfg.Observability.PprofEnabled {
		registerPprof(router.Group("/debug/pprof", pprofAuth(cfg.Observability.PprofToken)))
	}
	if cfg.Swagger.Enabled {
		docs.SwaggerInfo.Version = buildinfo.Version
		swagger := router.Group("/swagger")
		if cfg.Swagger.RequireAuth {
			swagger.Use(JWT(authService, logger))
		}
		swagger.GET("/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	api := router.Group("/api/v1", RateLimit(limiter, cfg.RateLimit.IP, "ip", func(c *gin.Context) string { return c.ClientIP() }, logger), RateLimit(limiter, cfg.RateLimit.API, "api", func(c *gin.Context) string { return c.FullPath() }, logger), Authentication(authService, logger, cfg.Auth), Authorization(cfg.Authorization.Enabled, authorizer, logger), RateLimit(limiter, cfg.RateLimit.User, "user", func(c *gin.Context) string {
		value, _ := c.Get("subject")
		subject, _ := value.(string)
		return subject
	}, logger))
	api.Use(IdempotencyExecution(idempotencyManager, cfg.Idempotency.HTTPPaths, logger))
	api.POST("/version", handler.Version)
	api.POST("/me", handler.Me)
	api.POST("/tenants/create", handler.CreateTenant)
	api.POST("/tenants/manage/create", handler.CreateManagedTenant)
	api.POST("/tenants/select", handler.SelectTenant)
	api.POST("/tenants/get", handler.GetTenant)
	api.POST("/tenants/update", handler.UpdateTenant)
	api.POST("/tenants/list", handler.ListTenants)
	api.POST("/tenants/list-by-user", handler.ListUserTenants)
	api.POST("/memberships/add", handler.AddMembership)
	api.POST("/memberships/update", handler.UpdateMembership)
	api.POST("/memberships/list", handler.ListMemberships)
	api.POST("/memberships/batch-get", handler.BatchGetMemberships)
	api.POST("/memberships/directory/search", handler.SearchMembershipDirectory)
	api.POST("/organization-units/create", handler.CreateOrganizationUnit)
	api.POST("/organization-units/get", handler.GetOrganizationUnit)
	api.POST("/organization-units/update", handler.UpdateOrganizationUnit)
	api.POST("/organization-units/list", handler.ListOrganizationUnits)
	api.POST("/invitations/create", handler.CreateInvitation)
	api.POST("/invitations/accept", handler.AcceptInvitation)
	api.POST("/invitations/revoke", handler.RevokeInvitation)
	api.POST("/invitations/list", handler.ListInvitations)
	api.POST("/groups/create", handler.CreateGroup)
	api.POST("/groups/update", handler.UpdateGroup)
	api.POST("/groups/member-add", handler.AddGroupMember)
	api.POST("/groups/member-remove", handler.RemoveGroupMember)
	api.POST("/groups/members/list", handler.ListGroupMembers)
	api.POST("/groups/list", handler.ListGroups)
	api.POST("/groups/search", handler.SearchGroups)
	api.POST("/quotas/get", handler.GetQuota)
	api.POST("/quotas/list", handler.ListQuotas)
	api.POST("/quotas/set", handler.SetQuota)
	api.POST("/quotas/consume", handler.ConsumeQuota)
	server := &http.Server{Addr: cfg.HTTP.Address, Handler: router, ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout}
	var listener net.Listener
	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		var err error
		listener, err = net.Listen("tcp", server.Addr)
		if err != nil {
			return fmt.Errorf("listen http: %w", err)
		}
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				logger.Error("http server stopped unexpectedly", "error", serveErr)
			}
		}()
		logger.Info("http server started", "address", server.Addr)
		return nil
	}, OnStop: func(ctx context.Context) error { return server.Shutdown(ctx) }})
	return server, nil
}

func pprofAuth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

func registerPprof(group *gin.RouterGroup) {
	group.GET("/", gin.WrapF(stdpprof.Index))
	group.GET("/cmdline", gin.WrapF(stdpprof.Cmdline))
	group.GET("/profile", gin.WrapF(stdpprof.Profile))
	group.POST("/symbol", gin.WrapF(stdpprof.Symbol))
	group.GET("/symbol", gin.WrapF(stdpprof.Symbol))
	group.GET("/trace", gin.WrapF(stdpprof.Trace))
	for _, profile := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		group.GET("/"+profile, gin.WrapH(stdpprof.Handler(profile)))
	}
}

var Module = fx.Module("http", fx.Provide(auth.NewRuntime, health.New, ratelimit.New, NewHandler, NewServer), fx.Invoke(func(*http.Server) {}))
