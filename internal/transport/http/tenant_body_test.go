package httptransport

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tenant "github.com/lihongjie0209/tenant-service/internal/tenant"
)

func TestInvitationBodyDoesNotExposeTokenHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(invitationBody(tenant.Invitation{
		ID: "invitation-1", TenantID: "tenant-1", Email: "user@example.test",
		TokenHash: "secret-token-hash", Status: "pending", ExpiresAt: now, Version: 1,
		CreatedAt: now, UpdatedAt: now, CreatedBy: "user-1", UpdatedBy: "user-1",
	}))
	if err != nil {
		t.Fatalf("marshal invitation body: %v", err)
	}
	if strings.Contains(string(encoded), "secret-token-hash") {
		t.Fatal("invitation body exposed token hash")
	}
	assertTenantJSONKeys(t, encoded, []string{
		"accepted_by_user_id", "created_at", "created_by", "email", "expires_at", "id", "status", "tenant_id",
		"updated_at", "updated_by", "version",
	})
}

func TestCreateInvitationResponseExposesOnlyIssuedToken(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(CreateInvitationResponseBody{
		Invitation: invitationBody(tenant.Invitation{TokenHash: "stored-hash"}),
		Token:      "one-time-token",
	})
	if err != nil {
		t.Fatalf("marshal create invitation response: %v", err)
	}
	if !strings.Contains(string(encoded), "one-time-token") || strings.Contains(string(encoded), "stored-hash") {
		t.Fatalf("unexpected invitation credentials in response: %s", encoded)
	}
}

func assertTenantJSONKeys(t *testing.T, encoded []byte, expected []string) {
	t.Helper()

	var body map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	actual := make([]string, 0, len(body))
	for key := range body {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("public json keys = %v, want %v", actual, expected)
	}
}
