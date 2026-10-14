package db

import (
	"context"
	"fmt"
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

type PreMigration func(m gorm.Migrator) error

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
	d.Exec("PRAGMA foreign_keys=OFF")
	defer d.Exec("PRAGMA foreign_keys=ON")
	for i, fn := range d.preMigrations {
		if err := fn(d.Migrator()); err != nil {
			return fmt.Errorf("pre-migration %d: %w", i, err)
		}
	}
	return d.AutoMigrate(models...)
}

func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
