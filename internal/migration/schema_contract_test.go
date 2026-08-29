package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMySQLTenantIndexesFitInnoDBLimit(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "mysql", "000002_tenant_domain.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "path VARCHAR(700) NOT NULL") {
		t.Fatal("organization path must stay within the utf8mb4 composite index byte limit")
	}
}
