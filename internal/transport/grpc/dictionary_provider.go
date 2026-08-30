package grpctransport

import (
	"context"
	"errors"

	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	dictionaryv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/dictionary/v1"
	"github.com/lihongjie0209/tenant-service/internal/dictionarycontract"
	tenantdomain "github.com/lihongjie0209/tenant-service/internal/tenant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type dictionaryProviderServer struct {
	dictionaryv1.UnimplementedDictionaryProviderServiceServer
	provider *tenantdomain.DictionaryProvider
}

func (s *dictionaryProviderServer) Describe(context.Context, *dictionaryv1.DictionaryProviderServiceDescribeRequest) (*dictionaryv1.DictionaryProviderServiceDescribeResponse, error) {
	return &dictionaryv1.DictionaryProviderServiceDescribeResponse{ServiceName: "tenant-service", Capabilities: dictionarycontract.Capabilities()}, nil
}

func (s *dictionaryProviderServer) Query(ctx context.Context, request *dictionaryv1.DictionaryProviderServiceQueryRequest) (*dictionaryv1.DictionaryProviderServiceQueryResponse, error) {
	query := request.GetQuery()
	search := query.GetSearch()
	page := search.GetPage()
	result, err := s.provider.Query(ctx, query.GetTenantId(), query.GetDictionaryCode(), tenantdomain.DictionarySearch{Keyword: search.GetKeyword(), Filters: search.GetFilters(), Sort: search.GetSort(), Descending: search.GetDescending(), Page: int(page.GetPage()), PageSize: int(page.GetPageSize())})
	if err != nil {
		return nil, dictionaryProviderError(err)
	}
	items := make([]*dictionaryv1.DictionaryItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, organizationDictionaryItem(item))
	}
	return &dictionaryv1.DictionaryProviderServiceQueryResponse{Result: &dictionaryv1.QueryResponse{Items: items, Result: &dictionaryv1.ResultPage{Page: &commonv1.PageResult{Total: uint64(result.Total), Page: uint32(result.Page), PageSize: uint32(result.PageSize)}}}}, nil
}

func (s *dictionaryProviderServer) Tree(ctx context.Context, request *dictionaryv1.DictionaryProviderServiceTreeRequest) (*dictionaryv1.DictionaryProviderServiceTreeResponse, error) {
	query := request.GetQuery()
	mode := "full"
	switch query.GetMode() {
	case dictionaryv1.TreeMode_TREE_MODE_CHILDREN:
		mode = "children"
	case dictionaryv1.TreeMode_TREE_MODE_SEARCH_WITH_ANCESTORS:
		mode = "search_with_ancestors"
	}
	roots, truncated, err := s.provider.Tree(ctx, query.GetTenantId(), query.GetDictionaryCode(), mode, query.GetParentId(), query.GetKeyword(), int(query.GetMaxDepth()), int(query.GetMaxNodes()), query.GetFilters())
	if err != nil {
		return nil, dictionaryProviderError(err)
	}
	return &dictionaryv1.DictionaryProviderServiceTreeResponse{Result: &dictionaryv1.TreeResponse{Roots: organizationTreeNodes(roots), Truncated: truncated}}, nil
}

func (s *dictionaryProviderServer) ResolveCodes(ctx context.Context, request *dictionaryv1.DictionaryProviderServiceResolveCodesRequest) (*dictionaryv1.DictionaryProviderServiceResolveCodesResponse, error) {
	query := request.GetQuery()
	resolved, err := s.provider.ResolveCodes(ctx, query.GetTenantId(), query.GetDictionaryCode(), query.GetCodes())
	if err != nil {
		return nil, dictionaryProviderError(err)
	}
	values := make([]*dictionaryv1.ResolvedCode, 0, len(query.GetCodes()))
	for _, code := range query.GetCodes() {
		item, found := resolved[code]
		value := &dictionaryv1.ResolvedCode{Code: code, Found: found}
		if found {
			value.Item = organizationDictionaryItem(item)
		}
		values = append(values, value)
	}
	return &dictionaryv1.DictionaryProviderServiceResolveCodesResponse{Result: &dictionaryv1.ResolveCodesResponse{Values: values}}, nil
}

func organizationDictionaryItem(value tenantdomain.OrganizationUnit) *dictionaryv1.DictionaryItem {
	metadata, _ := structpb.NewStruct(map[string]any{"path": value.Path, "tenant_id": value.TenantID})
	return &dictionaryv1.DictionaryItem{Id: value.ID, DictionaryCode: tenantdomain.OrganizationUnitDictionaryCode, Code: value.Code, Name: value.Name, ParentId: value.ParentID, Status: value.Status, Metadata: metadata, Audit: &dictionaryv1.AuditFields{CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy, Version: value.Version}}
}
func organizationTreeNodes(values []tenantdomain.OrganizationTreeNode) []*dictionaryv1.TreeNode {
	result := make([]*dictionaryv1.TreeNode, 0, len(values))
	for _, value := range values {
		item := organizationDictionaryItem(value.Item)
		item.Leaf = len(value.Children) == 0
		result = append(result, &dictionaryv1.TreeNode{Item: item, Children: organizationTreeNodes(value.Children)})
	}
	return result
}
func dictionaryProviderError(err error) error {
	switch {
	case errors.Is(err, tenantdomain.ErrDictionaryRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, tenantdomain.ErrDictionaryUnsupported):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, "dynamic dictionary query failed")
	}
}
