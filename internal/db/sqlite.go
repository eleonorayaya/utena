package db

import (
	"fmt"
	"sync/atomic"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenSQLite(dbPath string) (*gorm.DB, error) {
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

	return db, nil
}

var memDBCounter atomic.Int64

func OpenInMemorySQLite() (*gorm.DB, error) {
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

	return db, nil
}

func Open(dbPath string) (*DB, error) {
	gormDB, err := OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	return NewDB(gormDB), nil
}

func OpenInMemory() (*DB, error) {
	gormDB, err := OpenInMemorySQLite()
	if err != nil {
		return nil, err
	}
	return NewDB(gormDB), nil
}
