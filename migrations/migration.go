package migrations

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var postgresMigrations embed.FS

//go:embed sqlite/*.sql
var sqliteMigrations embed.FS

func Migrate(db *sql.DB) {
	goose.SetBaseFS(postgresMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
	if err := goose.Up(db, "."); err != nil {
		panic(err)
	}
}

func MigrateSQLite(db *sql.DB) {
	goose.SetBaseFS(sqliteMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		panic(err)
	}
	if err := goose.Up(db, "sqlite"); err != nil {
		panic(err)
	}
}
