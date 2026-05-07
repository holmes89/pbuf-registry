package data

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pbufio/pbuf-registry/migrations"
)

func init() {
	RegisterDriver("postgres", openPostgres)
}

func openPostgres(dsn string, logger log.Logger) (RepositorySet, func(), error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return RepositorySet{}, nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		pool.Close()
		return RepositorySet{}, nil, fmt.Errorf("failed to open sql.DB for migrations: %w", err)
	}
	migrations.Migrate(sqlDB)
	sqlDB.Close()

	return RepositorySet{
		Registry: NewRegistryRepository(pool, logger),
		Metadata: NewMetadataRepository(pool, logger),
		Users:    NewUserRepository(pool, logger),
		ACL:      NewACLRepository(pool, logger),
		Drift:    NewDriftRepository(pool, logger),
	}, pool.Close, nil
}
