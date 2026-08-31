package tenant

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestSQLRepositoryListTenantsFiltersAndPaginates(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repository := NewRepository(sqlx.NewDb(db, "sqlmock"))
	where := " WHERE 1=1 AND status = ? AND (LOWER(code) LIKE LOWER(?) OR LOWER(name) LIKE LOWER(?))"
	pattern := "%north%"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM tenants"+where)).
		WithArgs("active", pattern, pattern).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+tenantColumns+" FROM tenants"+where+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?")).
		WithArgs("active", pattern, pattern, 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "status", "version", "created_at", "updated_at", "created_by", "updated_by"}).
			AddRow("tenant-1", "north", "North", "active", 1, now, now, "admin", "admin"))

	items, total, err := repository.ListTenants(t.Context(), "north", "active", 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "tenant-1" {
		t.Fatalf("ListTenants() = items:%#v total:%d", items, total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
