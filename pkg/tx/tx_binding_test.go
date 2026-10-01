package tx

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestBindGormTransactionRejectsNonSQLTransaction(t *testing.T) {
	gormTx := &gorm.DB{Statement: &gorm.Statement{ConnPool: &sql.DB{}}}
	txCtx, err := bindGormTransaction(context.Background(), gormTx)
	if err == nil || txCtx != nil || !strings.Contains(err.Error(), "want *sql.Tx") {
		t.Fatalf("ctx=%v err=%v", txCtx, err)
	}
}

func TestBindGormTransactionRejectsMissingStatement(t *testing.T) {
	if txCtx, err := bindGormTransaction(context.Background(), nil); err == nil || txCtx != nil {
		t.Fatalf("ctx=%v err=%v", txCtx, err)
	}
}
