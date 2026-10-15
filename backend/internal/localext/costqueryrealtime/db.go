package costqueryrealtime

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	_ "github.com/lib/pq"
)

var (
	pgOnce sync.Once
	pgDB   *sql.DB
	pgErr  error
)

func postgresDB() (*sql.DB, error) {
	pgOnce.Do(func() {
		cfg, err := config.Load()
		if err != nil {
			pgErr = fmt.Errorf("load config: %w", err)
			return
		}
		dsn := cfg.Database.DSNWithTimezone("Asia/Shanghai")
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			pgErr = err
			return
		}
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(2)
		if err := db.Ping(); err != nil {
			_ = db.Close()
			pgErr = err
			return
		}
		pgDB = db
	})
	return pgDB, pgErr
}
