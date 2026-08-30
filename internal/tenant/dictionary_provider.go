package tenant

import (
	"context"
	"errors"
	"sort"
	"strings"
)

const OrganizationUnitDictionaryCode = "tenant.organization_units"

var (
	ErrDictionaryRequest     = errors.New("invalid dynamic dictionary request")
	ErrDictionaryUnsupported = errors.New("dynamic dictionary is not supported")
)

type OrganizationDictionaryRepository interface {
	ListOrganizationUnits(context.Context, string) ([]OrganizationUnit, error)
}

type DictionarySearch struct {
	Keyword    string
	Filters    map[string]string
	Sort       string
	Descending bool
	Page       int
	PageSize   int
}

type DictionaryPage struct {
	Items    []OrganizationUnit
	Total    int64
	Page     int
	PageSize int
}

type OrganizationTreeNode struct {
	Item     OrganizationUnit
	Children []OrganizationTreeNode
}

type DictionaryProvider struct {
	repository OrganizationDictionaryRepository
}

func NewDictionaryProvider(repository Repository) *DictionaryProvider {
	return &DictionaryProvider{repository: repository}
}

func (p *DictionaryProvider) Query(ctx context.Context, tenantID, code string, search DictionarySearch) (DictionaryPage, error) {
	if strings.TrimSpace(tenantID) == "" || code != OrganizationUnitDictionaryCode {
		return DictionaryPage{}, ErrDictionaryUnsupported
	}
	if search.Page == 0 {
		search.Page = 1
	}
	if search.PageSize == 0 {
		search.PageSize = 20
	}
	if search.Page < 1 || search.PageSize < 1 || search.PageSize > 100 || !validDictionaryFilters(search.Filters) || !validDictionarySort(search.Sort) {
		return DictionaryPage{}, ErrDictionaryRequest
	}
	items, err := p.repository.ListOrganizationUnits(ctx, tenantID)
	if err != nil {
		return DictionaryPage{}, err
	}
	items = filterOrganizationUnits(items, search.Keyword, search.Filters["status"])
	sortOrganizationUnits(items, search.Sort, search.Descending)
	total := int64(len(items))
	start := (search.Page - 1) * search.PageSize
	if start >= len(items) {
		items = []OrganizationUnit{}
	} else {
		end := min(start+search.PageSize, len(items))
		items = items[start:end]
	}
	return DictionaryPage{Items: items, Total: total, Page: search.Page, PageSize: search.PageSize}, nil
}

func (p *DictionaryProvider) Tree(ctx context.Context, tenantID, code, mode, parentID, keyword string, maxDepth, maxNodes int, filters map[string]string) ([]OrganizationTreeNode, bool, error) {
	if strings.TrimSpace(tenantID) == "" || code != OrganizationUnitDictionaryCode {
		return nil, false, ErrDictionaryUnsupported
	}
	if !validDictionaryFilters(filters) || (mode != "full" && mode != "children" && mode != "search_with_ancestors") {
		return nil, false, ErrDictionaryRequest
	}
	if maxDepth == 0 {
		maxDepth = 8
	}
	if maxNodes == 0 {
		maxNodes = 1000
	}
	if maxDepth < 1 || maxDepth > 32 || maxNodes < 1 || maxNodes > 5000 {
		return nil, false, ErrDictionaryRequest
	}
	items, err := p.repository.ListOrganizationUnits(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	items = filterOrganizationUnits(items, "", filters["status"])
	if mode == "search_with_ancestors" && strings.TrimSpace(keyword) != "" {
		items = withOrganizationAncestors(items, keyword)
	}
	roots, truncated := buildOrganizationTree(items, mode, parentID, maxDepth, maxNodes)
	return roots, truncated, nil
}

func (p *DictionaryProvider) ResolveCodes(ctx context.Context, tenantID, code string, codes []string) (map[string]OrganizationUnit, error) {
	if strings.TrimSpace(tenantID) == "" || code != OrganizationUnitDictionaryCode {
		return nil, ErrDictionaryUnsupported
	}
	if len(codes) > 500 {
		return nil, ErrDictionaryRequest
	}
	items, err := p.repository.ListOrganizationUnits(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(codes))
	for _, value := range codes {
		wanted[value] = struct{}{}
	}
	result := make(map[string]OrganizationUnit, len(wanted))
	for _, item := range items {
		if _, ok := wanted[item.Code]; ok {
			result[item.Code] = item
		}
	}
	return result, nil
}

func validDictionaryFilters(filters map[string]string) bool {
	for key := range filters {
		if key != "status" {
			return false
		}
	}
	return true
}
func validDictionarySort(value string) bool {
	return value == "" || value == "code" || value == "name" || value == "path"
}
func filterOrganizationUnits(items []OrganizationUnit, keyword, status string) []OrganizationUnit {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	result := make([]OrganizationUnit, 0, len(items))
	for _, item := range items {
		if status != "" && item.Status != status {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(item.Code), keyword) && !strings.Contains(strings.ToLower(item.Name), keyword) {
			continue
		}
		result = append(result, item)
	}
	return result
}
func sortOrganizationUnits(items []OrganizationUnit, field string, descending bool) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Path, items[j].Path
		switch field {
		case "code":
			left, right = items[i].Code, items[j].Code
		case "name":
			left, right = items[i].Name, items[j].Name
		}
		if left == right {
			left, right = items[i].ID, items[j].ID
		}
		if descending {
			return left > right
		}
		return left < right
	})
}
func withOrganizationAncestors(items []OrganizationUnit, keyword string) []OrganizationUnit {
	byID := make(map[string]OrganizationUnit, len(items))
	keep := map[string]bool{}
	for _, item := range items {
		byID[item.ID] = item
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	for _, item := range items {
		if !strings.Contains(strings.ToLower(item.Code), keyword) && !strings.Contains(strings.ToLower(item.Name), keyword) {
			continue
		}
		for current := item; current.ID != "" && !keep[current.ID]; current = byID[current.ParentID] {
			keep[current.ID] = true
		}
	}
	result := make([]OrganizationUnit, 0, len(keep))
	for _, item := range items {
		if keep[item.ID] {
			result = append(result, item)
		}
	}
	return result
}
func buildOrganizationTree(items []OrganizationUnit, mode, parentID string, maxDepth, maxNodes int) ([]OrganizationTreeNode, bool) {
	children := map[string][]OrganizationUnit{}
	for _, item := range items {
		children[item.ParentID] = append(children[item.ParentID], item)
	}
	for key := range children {
		sortOrganizationUnits(children[key], "path", false)
	}
	root := ""
	if mode == "children" {
		root = parentID
	}
	count, truncated := 0, false
	var build func(string, int, map[string]bool) []OrganizationTreeNode
	build = func(parent string, depth int, ancestors map[string]bool) []OrganizationTreeNode {
		if depth > maxDepth {
			if len(children[parent]) > 0 {
				truncated = true
			}
			return nil
		}
		result := []OrganizationTreeNode{}
		for _, item := range children[parent] {
			if count >= maxNodes {
				truncated = true
				break
			}
			if ancestors[item.ID] {
				truncated = true
				continue
			}
			count++
			next := make(map[string]bool, len(ancestors)+1)
			for key, value := range ancestors {
				next[key] = value
			}
			next[item.ID] = true
			result = append(result, OrganizationTreeNode{Item: item, Children: build(item.ID, depth+1, next)})
		}
		return result
	}
	return build(root, 1, map[string]bool{}), truncated
}
