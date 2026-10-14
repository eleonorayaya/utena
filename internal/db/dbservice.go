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
	return d.DB.Connection(func(conn *gorm.DB) error {
		return d.migrate(conn.Session(&gorm.Session{NewDB: true}), models)
	})
}

func (d *DB) migrate(conn *gorm.DB, models []any) error {
	if err := conn.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}
	defer conn.Exec("PRAGMA foreign_keys=ON")
	seqs, err := snapshotSequences(conn)
	if err != nil {
		return err
	}
	baseline, err := foreignKeyViolations(conn)
	if err != nil {
		return err
	}
	if len(baseline) > 0 {
		slog.Warn("database has pre-existing foreign key violations", "violations", describeViolations(baseline))
	}
	if err := d.runPreMigrations(conn, baseline); err != nil {
		return err
	}
	migrateErr := conn.AutoMigrate(models...)
	if err := restoreSequences(conn, seqs); err != nil {
		return errors.Join(migrateErr, err)
	}
	if migrateErr != nil {
		return migrateErr
	}
	fresh, err := newForeignKeyViolations(conn, baseline)
	if err != nil {
		slog.Error("foreign key check after auto-migrate failed", "error", err)
		return nil
	}
	if len(fresh) > 0 {
		slog.Error("foreign key violations after auto-migrate", "violations", describeViolations(fresh))
	}
	return nil
}

func (d *DB) runPreMigrations(conn *gorm.DB, baseline []foreignKeyViolation) error {
	if len(d.preMigrations) == 0 {
		return nil
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for _, fn := range d.preMigrations {
			if err := fn(tx); err != nil {
				return err
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
	})
}

func DropIndexesIfExist(m gorm.Migrator, table string, names ...string) error {
	for _, name := range names {
		if !m.HasIndex(table, name) {
			continue
		}
		if err := m.DropIndex(table, name); err != nil {
			return err
		}
	}
	return nil
}

func DropConstraintIfExists(m gorm.Migrator, table, name string) error {
	if !m.HasConstraint(table, name) {
		return nil
	}
	return m.DropConstraint(table, name)
}

type sequence struct {
	Name string
	Seq  int64
}

func snapshotSequences(tx *gorm.DB) ([]sequence, error) {
	if !tx.Migrator().HasTable("sqlite_sequence") {
		return nil, nil
	}
	var seqs []sequence
	if err := tx.Raw("SELECT name, seq FROM sqlite_sequence").Scan(&seqs).Error; err != nil {
		return nil, fmt.Errorf("snapshot sqlite_sequence: %w", err)
	}
	return seqs, nil
}

func restoreSequences(tx *gorm.DB, seqs []sequence) error {
	for _, s := range seqs {
		if !tx.Migrator().HasTable(s.Name) {
			continue
		}
		if err := tx.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = ? AND seq < ?", s.Seq, s.Name, s.Seq).Error; err != nil {
			return fmt.Errorf("restore sqlite_sequence for %s: %w", s.Name, err)
		}
		if err := tx.Exec("INSERT INTO sqlite_sequence (name, seq) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = ?)", s.Name, s.Seq, s.Name).Error; err != nil {
			return fmt.Errorf("restore sqlite_sequence for %s: %w", s.Name, err)
		}
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

func rowidString(v foreignKeyViolation) string {
	if v.Rowid == nil {
		return "?"
	}
	return fmt.Sprintf("%d", *v.Rowid)
}

func violationKey(v foreignKeyViolation) string {
	return v.Table + ":" + rowidString(v)
}

func describeViolations(violations []foreignKeyViolation) string {
	parts := make([]string, 0, len(violations))
	for _, v := range violations {
		parts = append(parts, fmt.Sprintf("%s rowid %s -> %s", v.Table, rowidString(v), v.Parent))
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
