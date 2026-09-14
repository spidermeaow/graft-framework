package cli

import (
	"database/sql"
	"errors"
	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq" // PostgreSQL wire protocol, kept out of the HTTP core.
	"github.com/spidermeaow/graft-framework/migration"
	mysqlstore "github.com/spidermeaow/graft-framework/migration/mysql"
	"github.com/spidermeaow/graft-framework/migration/postgres"
	"strings"
	"time"
)

func migrationDriver(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postgres", "postgresql":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	default:
		return "", errors.New("unsupported DATABASE_DRIVER; use postgres or mysql")
	}
}

func openMigrationStore(dialect, dsn string) (*sql.DB, migration.Store, error) {
	if dialect == "mysql" {
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil || cfg.DBName == "" {
			return nil, nil, errors.New("invalid MySQL DATABASE_URL; expected user:password@tcp(host:3306)/database")
		}
		cfg.ParseTime, cfg.MultiStatements, cfg.Loc = true, true, time.UTC
		connector, err := mysqldriver.NewConnector(cfg)
		if err != nil {
			return nil, nil, errors.New("invalid MySQL connection configuration")
		}
		db := sql.OpenDB(connector)
		return db, mysqlstore.New(db), nil
	}
	if dialect != "postgres" {
		return nil, nil, errors.New("unsupported DATABASE_DRIVER; use postgres or mysql")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, nil, errors.New("invalid PostgreSQL connection configuration")
	}
	return db, postgres.New(db), nil
}
