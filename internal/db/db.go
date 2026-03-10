package db

import (
	"fmt"
	"sync/atomic"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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
	Omit(columns ...string) *gorm.DB
	Migrate(models ...any) error
	Close() error
}

type DB struct {
	*gorm.DB
}

func Open(dbPath string) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)

	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec("PRAGMA busy_timeout=5000")

	return &DB{db}, nil
}

var memDBCounter atomic.Int64

func OpenInMemory() (*DB, error) {
	dsn := fmt.Sprintf("file:memdb_%d?mode=memory&cache=shared", memDBCounter.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)

	db.Exec("PRAGMA foreign_keys=ON")

	return &DB{db}, nil
}

func (d *DB) Migrate(models ...any) error {
	return d.AutoMigrate(models...)
}

func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
