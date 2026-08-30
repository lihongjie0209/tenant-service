package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/lihongjie0209/microservice-platform-go/serviceregistry"
	registryv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/registry/v1"
	"github.com/lihongjie0209/tenant-service/internal/config"
	"github.com/lihongjie0209/tenant-service/internal/dictionarycontract"
	"github.com/lihongjie0209/tenant-service/internal/outbound"
	"go.uber.org/fx"
)

type dictionaryProviderRuntime struct {
	registrant *serviceregistry.Registrant
	cancel     context.CancelFunc
}

func newDictionaryProviderRuntime(lc fx.Lifecycle, cfg config.Config, outboundClients *outbound.Registry) (*dictionaryProviderRuntime, error) {
	if !cfg.DictionaryProvider.Enabled {
		return &dictionaryProviderRuntime{}, nil
	}
	connection, ok := outboundClients.GRPC(cfg.DictionaryProvider.RegistryClient)
	if !ok {
		return nil, errors.New("service registry gRPC client is not configured")
	}
	capabilities, err := json.Marshal(dictionarycontract.Capabilities())
	if err != nil {
		return nil, fmt.Errorf("encode dictionary capabilities: %w", err)
	}
	instanceID, _ := os.Hostname()
	if strings.TrimSpace(instanceID) == "" {
		instanceID = cfg.App.Name + "-" + uuid.NewString()
	}
	endpoint := cfg.DictionaryProvider.Target
	if !strings.Contains(endpoint, "://") {
		endpoint = "grpc://" + endpoint
	}
	registrant, err := serviceregistry.NewRegistrant(registryv1.NewRegistryServiceClient(connection), serviceregistry.RegistrantConfig{
		Instance: &registryv1.ServiceInstance{InstanceId: instanceID, ServiceName: cfg.App.Name, Endpoint: endpoint, Protocol: "grpc", Version: "v1", Metadata: map[string]string{
			"platform.dictionary.provider": "true", "platform.dictionary.capabilities": string(capabilities),
			"platform.dictionary.cache_ttl_seconds":    fmt.Sprint(int(cfg.DictionaryProvider.CacheTTL.Seconds())),
			"platform.dictionary.timeout_milliseconds": fmt.Sprint(cfg.DictionaryProvider.ProviderTimeout.Milliseconds()),
		}},
		Lease: cfg.DictionaryProvider.LeaseDuration, HeartbeatInterval: cfg.DictionaryProvider.LeaseDuration / 3,
		CallTimeout: cfg.Outbound.GRPC[cfg.DictionaryProvider.RegistryClient].Timeout, RetryMin: cfg.DictionaryProvider.RetryDelay,
	})
	if err != nil {
		return nil, err
	}
	runtime := &dictionaryProviderRuntime{registrant: registrant}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runCtx, cancel := context.WithCancel(context.Background())
			runtime.cancel = cancel
			go func() { _ = registrant.Run(runCtx) }()
			return nil
		},
		OnStop: func(context.Context) error {
			if runtime.cancel != nil {
				runtime.cancel()
			}
			return nil
		},
	})
	return runtime, nil
}

var DictionaryProviderModule = fx.Module("dictionary-provider", fx.Provide(newDictionaryProviderRuntime), fx.Invoke(func(*dictionaryProviderRuntime) {}))
