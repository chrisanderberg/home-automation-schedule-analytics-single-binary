package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func Export(ctx context.Context, db *sql.DB, snapshotDir string) (string, error) {
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}

	filename := fmt.Sprintf("snapshot-%s.sqlite", time.Now().UTC().Format("20060102-150405"))
	destPath := filepath.Join(snapshotDir, filename)

	if err := copySQLiteDB(ctx, db, destPath); err != nil {
		return "", fmt.Errorf("export snapshot: %w", err)
	}
	return destPath, nil
}

func copySQLiteDB(ctx context.Context, src *sql.DB, destPath string) error {
	dest, err := sql.Open("sqlite", destPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err := dest.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer func() {
		if _, err := dest.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
			log.Printf("restore foreign keys on snapshot db: %v", err)
		}
	}()

	readTx, err := src.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read tx: %w", err)
	}
	defer readTx.Rollback()

	rows, err := readTx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			return err
		}
		if _, err := dest.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("exec ddl: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tables, err := tableNames(ctx, readTx)
	if err != nil {
		return err
	}

	for _, table := range tables {
		if err := copyTable(ctx, readTx, dest, table); err != nil {
			return fmt.Errorf("copy table %s: %w", table, err)
		}
	}
	return nil
}

type queryContexter interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func tableNames(ctx context.Context, db queryContexter) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if isInternalSQLiteName(name) {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func copyTable(ctx context.Context, src queryContexter, dest *sql.DB, table string) error {
	if isInternalSQLiteName(table) {
		return nil
	}

	rows, err := src.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %q", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil
	}

	placeholders := ""
	for i := range cols {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %q VALUES (%s)", table, placeholders)

	startBatch := func() (*sql.Tx, *sql.Stmt, error) {
		tx, err := dest.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			_ = tx.Rollback()
			return nil, nil, err
		}
		return tx, stmt, nil
	}

	tx, stmt, err := startBatch()
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	rowCount := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return err
		}
		rowCount++
		if rowCount%500 == 0 {
			if err := stmt.Close(); err != nil {
				_ = tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				_ = tx.Rollback()
				return err
			}
			tx, stmt, err = startBatch()
			if err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	tx = nil
	return nil
}

type SnapshotInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
}

func ListSnapshots(snapshotDir string) ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var infos []SnapshotInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sqlite" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			log.Printf("read snapshot entry info for %s: %v", e.Name(), err)
			continue
		}
		infos = append(infos, SnapshotInfo{
			Path:    filepath.Join(snapshotDir, e.Name()),
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ModTime.After(infos[j].ModTime)
	})
	return infos, nil
}

func isInternalSQLiteName(name string) bool {
	return name == "sqlite_sequence" || strings.HasPrefix(name, "sqlite_")
}
