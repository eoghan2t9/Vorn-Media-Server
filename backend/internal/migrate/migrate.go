// Package migrate applies Postgres schema migrations on backend startup.
package migrate

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Up applies all pending migrations to the database at dsn.
func Up(dsn string) error {
	source, err := iofs.New(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: initializing source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		return fmt.Errorf("migrate: initializing migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: applying migrations: %w", err)
	}

	return nil
}
