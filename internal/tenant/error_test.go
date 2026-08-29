package tenant

import (
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lihongjie0209/tenant-service/internal/apperror"
)

func TestTranslateUniqueViolationToConflict(t *testing.T) {
	t.Parallel()
	err := translate(&mysql.MySQLError{Number: 1062, Message: "duplicate"})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("translate()=%v", err)
	}
}
