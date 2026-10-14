package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

type Database interface {
	Create(value any) *gorm.DB
	Save(value any) *gorm.DB
	Delete(value any, conds ...any) *gorm.DB
	First(dest any, conds ...any) *gorm.DB
	Find(dest any, conds ...any) *gorm.DB
	Where(query any, args ...any) *gorm.DB
	Model(value any) *gorm.DB
	Order(value any) *gorm.DB
	Joins(query string, args ...any) *gorm.DB
	Preload(query string, args ...any) *gorm.DB
	Omit(columns ...string) *gorm.DB
	Exec(sql string, values ...any) *gorm.DB
	Migrate(models ...any) error
	Close() error
}

type PreMigration func(tx *gorm.DB) error

type DB struct {
	*gorm.DB
	models        []any
	preMigrations []PreMigration
}

func NewDB(gormDB *gorm.DB) *DB {
	return &DB{DB: gormDB}
}

func (d *DB) RegisterModels(models ...any) {
	d.models = append(d.models, models...)
}

func (d *DB) RegisterPreMigrations(fns ...PreMigration) {
	d.preMigrations = append(d.preMigrations, fns...)
}

func (d *DB) OnAppStart(ctx context.Context) error {
	if len(d.models) > 0 || len(d.preMigrations) > 0 {
		return d.Migrate(d.models...)
	}
	return nil
}

func (d *DB) OnAppEnd(ctx context.Context) error {
	return d.Close()
}

func (d *DB) Migrate(models ...any) error {
	if err := d.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer d.Exec("PRAGMA foreign_keys=ON")
	seqs, err := snapshotSequences(d.DB)
	if err != nil {
		return err
	}
	baseline, err := foreignKeyViolations(d.DB)
	if err != nil {
		return err
	}
	if len(baseline) > 0 {
		slog.Warn("database has pre-existing foreign key violations", "violations", describeViolations(baseline))
	}
	if len(d.preMigrations) > 0 {
		if err := d.Transaction(func(tx *gorm.DB) error {
			if err := requireForeignKeysOff(tx); err != nil {
				return err
			}
			for i, fn := range d.preMigrations {
				if err := fn(tx); err != nil {
					return fmt.Errorf("pre-migration %d: %w", i, err)
				}
			}
			fresh, err := newForeignKeyViolations(tx, baseline)
			if err != nil {
				return err
			}
			if len(fresh) > 0 {
				return fmt.Errorf("after pre-migrations: foreign key violations: %s", describeViolations(fresh))
			}
			return nil
		}); err != nil {
			return err
		}
	}
	migrateErr := d.AutoMigrate(models...)
	if err := restoreSequences(d.DB, seqs); err != nil {
		return errors.Join(migrateErr, err)
	}
	if migrateErr != nil {
		return migrateErr
	}
	fresh, err := newForeignKeyViolations(d.DB, baseline)
	if err != nil {
		slog.Error("foreign key check after auto-migrate failed", "error", err)
		return nil
	}
	if len(fresh) > 0 {
		slog.Error("foreign key violations after auto-migrate", "violations", describeViolations(fresh))
	}
	return nil
}

type sequence struct {
	Name string
	Seq  int64
}

func hasTable(tx *gorm.DB, name string) (bool, error) {
	var count int64
	if err := tx.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func snapshotSequences(tx *gorm.DB) ([]sequence, error) {
	ok, err := hasTable(tx, "sqlite_sequence")
	if err != nil || !ok {
		return nil, err
	}
	var seqs []sequence
	if err := tx.Raw("SELECT name, seq FROM sqlite_sequence").Scan(&seqs).Error; err != nil {
		return nil, fmt.Errorf("snapshot sqlite_sequence: %w", err)
	}
	return seqs, nil
}

func restoreSequences(tx *gorm.DB, seqs []sequence) error {
	for _, s := range seqs {
		exists, err := hasTable(tx, s.Name)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var rows int64
		if err := tx.Raw("SELECT count(*) FROM sqlite_sequence WHERE name = ?", s.Name).Scan(&rows).Error; err != nil {
			return fmt.Errorf("restore sqlite_sequence for %s: %w", s.Name, err)
		}
		q := "UPDATE sqlite_sequence SET seq = ? WHERE name = ? AND seq < ?"
		args := []any{s.Seq, s.Name, s.Seq}
		if rows == 0 {
			q = "INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)"
			args = []any{s.Name, s.Seq}
		}
		if err := tx.Exec(q, args...).Error; err != nil {
			return fmt.Errorf("restore sqlite_sequence for %s: %w", s.Name, err)
		}
	}
	return nil
}

func requireForeignKeysOff(tx *gorm.DB) error {
	var enabled int
	if err := tx.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
		return fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	if enabled != 0 {
		return fmt.Errorf("foreign keys must be off during pre-migrations")
	}
	return nil
}

type foreignKeyViolation struct {
	Table  string
	Rowid  *int64
	Parent string
}

func foreignKeyViolations(tx *gorm.DB) ([]foreignKeyViolation, error) {
	var violations []foreignKeyViolation
	if err := tx.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return nil, fmt.Errorf("foreign_key_check: %w", err)
	}
	return violations, nil
}

func violationKey(v foreignKeyViolation) string {
	if v.Rowid == nil {
		return v.Table + ":?"
	}
	return fmt.Sprintf("%s:%d", v.Table, *v.Rowid)
}

func describeViolations(violations []foreignKeyViolation) string {
	parts := make([]string, 0, len(violations))
	for _, v := range violations {
		rowid := "?"
		if v.Rowid != nil {
			rowid = fmt.Sprintf("%d", *v.Rowid)
		}
		parts = append(parts, fmt.Sprintf("%s rowid %s -> %s", v.Table, rowid, v.Parent))
	}
	return strings.Join(parts, "; ")
}

func newForeignKeyViolations(tx *gorm.DB, baseline []foreignKeyViolation) ([]foreignKeyViolation, error) {
	violations, err := foreignKeyViolations(tx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(baseline))
	for _, v := range baseline {
		known[violationKey(v)] = true
	}
	var fresh []foreignKeyViolation
	for _, v := range violations {
		if !known[violationKey(v)] {
			fresh = append(fresh, v)
		}
	}
	return fresh, nil
}

func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
