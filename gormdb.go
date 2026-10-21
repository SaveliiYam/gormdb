// Package gormdb — адаптер gorm.io/gorm к портам github.com/SaveliiYam/database.
//
// Репозиторий работает через DB(ctx):
//
//	func (r *Repo) Create(ctx context.Context, u *User) error {
//		return r.db.DB(ctx).Create(u).Error
//	}
//
// DB(ctx) внутри WithinTx возвращает *gorm.DB транзакции, вне — обычный
// (с WithContext(ctx)). Смена на pgx: репозиторий переписывается, сервис
// (Transactor) и main — нет.
package gormdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/SaveliiYam/database"
)

// Config — настройки. Dialector имеет приоритет над DSN и позволяет
// подставить любую СУБД/тестовый dialector — адаптер не зашит в postgres.
type Config struct {
	DSN             string `env:"DSN"`
	Dialector       gorm.Dialector
	MaxOpenConns    int           `env:"MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns    int           `env:"MAX_IDLE_CONNS" envDefault:"5"`
	ConnMaxLifetime time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"1h"`
	Silent          bool          // отключить gorm-лог SQL
}

// DB — обёртка над *gorm.DB, реализует database.DB.
type DB struct {
	db  *gorm.DB
	sql *sql.DB
}

// New открывает соединение. По умолчанию — postgres по DSN.
func New(cfg Config) (*DB, error) {
	dial := cfg.Dialector
	if dial == nil {
		if cfg.DSN == "" {
			return nil, errors.New("gormdb: DSN is empty and no Dialector given")
		}
		dial = postgres.Open(cfg.DSN)
	}
	gcfg := &gorm.Config{}
	if cfg.Silent {
		gcfg.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	}
	gdb, err := gorm.Open(dial, gcfg)
	if err != nil {
		return nil, fmt.Errorf("gormdb: open: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("gormdb: sql db: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return &DB{db: gdb, sql: sqlDB}, nil
}

// NewFromDB оборачивает готовый *gorm.DB.
func NewFromDB(gdb *gorm.DB) (*DB, error) {
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	return &DB{db: gdb, sql: sqlDB}, nil
}

// Native — escape hatch: нативный *gorm.DB без контекста.
func (d *DB) Native() *gorm.DB { return d.db }

type txKey struct{}

// DB возвращает tx из ctx внутри WithinTx, иначе сессию с WithContext(ctx).
func (d *DB) DB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return d.db.WithContext(ctx)
}

func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }
func (d *DB) Close() error                   { return d.sql.Close() }

// --- database.Transactor ---

func (d *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return fn(ctx) // уже в транзакции
	}
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

func (d *DB) WithinTxOptions(ctx context.Context, opts database.TxOptions, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return fn(ctx)
	}
	var iso sql.IsolationLevel
	switch opts.Level {
	case database.LevelReadCommitted:
		iso = sql.LevelReadCommitted
	case database.LevelRepeatableRead:
		iso = sql.LevelRepeatableRead
	case database.LevelSerializable:
		iso = sql.LevelSerializable
	default:
		iso = sql.LevelDefault
	}
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	}, &sql.TxOptions{Isolation: iso, ReadOnly: opts.ReadOnly})
}

// --- app.Component ---

func (d *DB) Name() string { return "gorm" }
func (d *DB) Start(ctx context.Context) error {
	if err := d.Ping(ctx); err != nil {
		return fmt.Errorf("gormdb: ping: %w", err)
	}
	<-ctx.Done()
	return nil
}
func (d *DB) Stop(context.Context) error { return d.Close() }

// IsNotFound нормализует gorm.ErrRecordNotFound к database.ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || database.IsNotFound(err)
}

// MapNotFound превращает gorm.ErrRecordNotFound в database.ErrNotFound.
func MapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.ErrNotFound
	}
	return err
}

var (
	_ database.DB           = (*DB)(nil)
	_ database.TxTransactor = (*DB)(nil)
)
