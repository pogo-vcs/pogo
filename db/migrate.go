package db

import (
	"context"
	"embed"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	if _, err := db.Exec(ctx, `
	CREATE TABLE IF NOT EXISTS migrations (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP 
	);
	`); err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	appliedMigrations, err := getAppliedMigrations(ctx, tx)
	if err != nil {
		return err
	}

	for name, content := range getAllMigrations {
		if slices.Contains(appliedMigrations, name) {
			continue
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO migrations (name) VALUES ($1);
		`, name); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func getAllMigrations(yield func(name string, content []byte) bool) {
	_ = fs.WalkDir(migrations, ".", func(filename string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := migrations.ReadFile(filename)
		if err != nil {
			return err
		}
		name := filepath.Base(filename[:len(filename)-4])
		if !yield(name, content) {
			return fs.SkipAll
		}
		return nil
	})
}

func getAppliedMigrations(ctx context.Context, db pgx.Tx) ([]string, error) {
	var migrations []string
	rows, err := db.Query(ctx, `
		SELECT name FROM migrations ORDER BY created_at ASC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		migrations = append(migrations, name)
	}

	return migrations, nil
}
