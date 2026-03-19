package snapshot

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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

	filename, err := snapshotFilename()
	if err != nil {
		return "", fmt.Errorf("generate snapshot filename: %w", err)
	}
	destPath := filepath.Join(snapshotDir, filename)
	tempFile, err := os.CreateTemp(snapshotDir, "."+filename+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp snapshot file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close temp snapshot file: %w", err)
	}
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := copySQLiteDB(ctx, db, tempPath); err != nil {
		return "", fmt.Errorf("export snapshot: %w", err)
	}
	if err := os.Rename(tempPath, destPath); err != nil {
		return "", fmt.Errorf("move snapshot into place: %w", err)
	}
	return destPath, nil
}

func snapshotFilename() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"snapshot-%s-%s.sqlite",
		time.Now().UTC().Format("20060102-150405.000000000"),
		hex.EncodeToString(suffix[:]),
	), nil
}

func copySQLiteDB(ctx context.Context, src *sql.DB, destPath string) error {
	dest, err := sql.Open("sqlite", destPath)
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

	tableDDLs, otherDDLs, err := loadSchemaDDLs(ctx, readTx)
	if err != nil {
		return err
	}
	if err := execDDLs(ctx, dest, tableDDLs); err != nil {
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
	if err := execDDLs(ctx, dest, otherDDLs); err != nil {
		return err
	}
	return nil
}

type schemaDDL struct {
	objectType string
	sql        string
}

func loadSchemaDDLs(ctx context.Context, db queryContexter) ([]string, []string, error) {
	rows, err := db.QueryContext(ctx, `SELECT type, sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY rowid`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var tableDDLs []string
	var otherDDLs []string
	for rows.Next() {
		var ddl schemaDDL
		if err := rows.Scan(&ddl.objectType, &ddl.sql); err != nil {
			return nil, nil, err
		}
		if ddl.objectType == "table" {
			tableDDLs = append(tableDDLs, ddl.sql)
			continue
		}
		otherDDLs = append(otherDDLs, ddl.sql)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return tableDDLs, otherDDLs, nil
}

func execDDLs(ctx context.Context, db *sql.DB, ddls []string) error {
	for _, ddl := range ddls {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("exec ddl: %w", err)
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
		if stmt != nil {
			_ = stmt.Close()
		}
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	closeStmt := func() error {
		if stmt == nil {
			return nil
		}
		err := stmt.Close()
		if err == nil {
			stmt = nil
		}
		return err
	}

	rowCount := 0
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			if closeErr := closeStmt(); closeErr != nil {
				_ = tx.Rollback()
				return closeErr
			}
			return err
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			if closeErr := closeStmt(); closeErr != nil {
				_ = tx.Rollback()
				return closeErr
			}
			return err
		}
		rowCount++
		if rowCount%500 == 0 {
			if err := closeStmt(); err != nil {
				_ = tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			tx = nil
			tx, stmt, err = startBatch()
			if err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		if closeErr := closeStmt(); closeErr != nil {
			_ = tx.Rollback()
			return closeErr
		}
		return err
	}
	if err := closeStmt(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
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
	return strings.HasPrefix(name, "sqlite_")
}
