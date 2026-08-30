package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/lihongjie0209/microservice-platform-go/dictionaryprovider"
	"github.com/lihongjie0209/microservice-platform-go/distlock"
	dictionaryv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/dictionary/v1"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/dictionarycontract"
	"github.com/lihongjie0209/tenant-service/internal/outbound"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

type dictionaryProviderRuntime struct {
	registrant *dictionaryprovider.Registrant
}

func newDictionaryProviderRuntime(lc fx.Lifecycle, cfg config.Config, outboundClients *outbound.Registry, redisClient *redis.Client, logger *slog.Logger) (*dictionaryProviderRuntime, error) {
	if !cfg.DictionaryProvider.Enabled {
		return &dictionaryProviderRuntime{}, nil
	}
	connection, ok := outboundClients.GRPC(cfg.DictionaryProvider.RegistryClient)
	if !ok {
		return nil, errors.New("dictionary registry gRPC client is not configured")
	}
	if redisClient == nil {
		return nil, errors.New("dictionary provider leader election requires Redis")
	}
	upstream := cfg.Outbound.GRPC[cfg.DictionaryProvider.RegistryClient]
	registrant, err := dictionaryprovider.New(dictionaryprovider.Config{
		ServiceName:     cfg.App.Name,
		Target:          cfg.DictionaryProvider.Target,
		Capabilities:    dictionarycontract.Capabilities(),
		CacheTTL:        cfg.DictionaryProvider.CacheTTL,
		CallTimeout:     upstream.Timeout,
		ProviderTimeout: cfg.DictionaryProvider.ProviderTimeout,
		LeaseDuration:   cfg.DictionaryProvider.LeaseDuration,
		RetryDelay:      cfg.DictionaryProvider.RetryDelay,
		LeaderTTL:       cfg.DictionaryProvider.LeaderTTL,
		LeaderLockKey:   "dictionary-provider:" + cfg.App.Name,
	}, dictionaryprovider.NewGRPCRegistry(dictionaryv1.NewDictionaryServiceClient(connection)), distlock.NewRedisLocker(redisClient), logger)
	if err != nil {
		return nil, err
	}
	runtime := &dictionaryProviderRuntime{registrant: registrant}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return registrant.Start(context.WithoutCancel(ctx)) },
		OnStop:  registrant.Stop,
	})
	return runtime, nil
}

var DictionaryProviderModule = fx.Module("dictionary-provider", fx.Provide(newDictionaryProviderRuntime), fx.Invoke(func(*dictionaryProviderRuntime) {}))
