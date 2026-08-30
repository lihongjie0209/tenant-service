// Package dictionarycontract contains the tenant service's authoritative
// dynamic dictionary capability declaration used by both registration and
// the provider's Describe RPC.
package dictionarycontract

import (
	dictionaryv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/dictionary/v1"
	"github.com/lihongjie0209/tenant-service/internal/tenant"
)

func Capabilities() []*dictionaryv1.ProviderCapability {
	return []*dictionaryv1.ProviderCapability{{
		DictionaryCode: tenant.OrganizationUnitDictionaryCode,
		SupportsSearch: true,
		SupportsTree:   true,
		FilterKeys:     []string{"status"},
		SortKeys:       []string{"code", "name", "path"},
		MaxPageSize:    100,
		MaxTreeDepth:   32,
		MaxTreeNodes:   5000,
	}}
}
