package db

import (
	"context"

	"gorm.io/gorm"
)

type DatabaseModule struct {
	Service *DB
}

func NewDatabaseModule(gormDB *gorm.DB) *DatabaseModule {
	return &DatabaseModule{Service: NewDB(gormDB)}
}

func (m *DatabaseModule) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *DatabaseModule) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}
