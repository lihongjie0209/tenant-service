package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestDictionaryProvider_QueryFiltersSortsAndPages(t *testing.T) {
	provider := &DictionaryProvider{repository: fakeOrganizationDictionaryRepository{items: organizationFixture()}}
	page, err := provider.Query(t.Context(), "tenant-1", OrganizationUnitDictionaryCode, DictionarySearch{Keyword: "研发", Filters: map[string]string{"status": "active"}, Sort: "code", Descending: true, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].Code != "rd-product" {
		t.Fatalf("Query() = %+v", page)
	}
}

func TestDictionaryProvider_QueryRejectsUnownedFilters(t *testing.T) {
	provider := &DictionaryProvider{repository: fakeOrganizationDictionaryRepository{}}
	_, err := provider.Query(t.Context(), "tenant-1", OrganizationUnitDictionaryCode, DictionarySearch{Filters: map[string]string{"sql": "drop"}})
	if !errors.Is(err, ErrDictionaryRequest) {
		t.Fatalf("Query() error = %v", err)
	}
}

func TestDictionaryProvider_TreeSearchIncludesAncestorsAndBoundsNodes(t *testing.T) {
	provider := &DictionaryProvider{repository: fakeOrganizationDictionaryRepository{items: organizationFixture()}}
	roots, truncated, err := provider.Tree(t.Context(), "tenant-1", OrganizationUnitDictionaryCode, "search_with_ancestors", "", "平台", 8, 10, nil)
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if truncated || len(roots) != 1 || roots[0].Item.Code != "rd" || len(roots[0].Children) != 1 || roots[0].Children[0].Item.Code != "rd-platform" {
		t.Fatalf("Tree() = %+v, truncated=%v", roots, truncated)
	}
	_, truncated, err = provider.Tree(t.Context(), "tenant-1", OrganizationUnitDictionaryCode, "full", "", "", 8, 1, nil)
	if err != nil || !truncated {
		t.Fatalf("bounded Tree() truncated=%v error=%v", truncated, err)
	}
}

func TestDictionaryProvider_ResolvePreservesDomainOwnership(t *testing.T) {
	provider := &DictionaryProvider{repository: fakeOrganizationDictionaryRepository{items: organizationFixture()}}
	values, err := provider.ResolveCodes(t.Context(), "tenant-1", OrganizationUnitDictionaryCode, []string{"rd", "missing"})
	if err != nil {
		t.Fatalf("ResolveCodes() error = %v", err)
	}
	if len(values) != 1 || values["rd"].Name != "研发中心" {
		t.Fatalf("ResolveCodes() = %+v", values)
	}
	if _, err = provider.ResolveCodes(t.Context(), "tenant-1", "identity.users", nil); !errors.Is(err, ErrDictionaryUnsupported) {
		t.Fatalf("foreign dictionary error = %v", err)
	}
}

type fakeOrganizationDictionaryRepository struct {
	items []OrganizationUnit
	err   error
}

func (f fakeOrganizationDictionaryRepository) ListOrganizationUnits(context.Context, string) ([]OrganizationUnit, error) {
	return append([]OrganizationUnit(nil), f.items...), f.err
}

func organizationFixture() []OrganizationUnit {
	return []OrganizationUnit{
		{ID: "rd", TenantID: "tenant-1", Code: "rd", Name: "研发中心", Path: "/rd", Status: "active"},
		{ID: "platform", TenantID: "tenant-1", ParentID: "rd", Code: "rd-platform", Name: "平台研发", Path: "/rd/platform", Status: "active"},
		{ID: "product", TenantID: "tenant-1", ParentID: "rd", Code: "rd-product", Name: "产品研发", Path: "/rd/product", Status: "active"},
		{ID: "old", TenantID: "tenant-1", Code: "old", Name: "旧部门", Path: "/old", Status: "inactive"},
	}
}
