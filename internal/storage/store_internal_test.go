package storage

import (
	"net/url"
	"testing"
)

func TestSQLiteDSNEnablesImmediateTxLocking(t *testing.T) {
	t.Parallel()

	dsn := sqliteDSN("file:test.db")
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if got := parsed.Query().Get("_txlock"); got != "immediate" {
		t.Fatalf("sqliteDSN() _txlock = %q, want %q", got, "immediate")
	}
}
