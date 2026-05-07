package sqlite

import (
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/migrations"
)

func init() {
	data.RegisterDriver("sqlite", openSQLite)
}

func openSQLite(dsn string, logger log.Logger) (data.RepositorySet, func(), error) {
	db, err := Open(dsn)
	if err != nil {
		return data.RepositorySet{}, nil, fmt.Errorf("failed to open sqlite: %w", err)
	}
	migrations.MigrateSQLite(db)
	return data.RepositorySet{
		Registry: NewRegistryRepository(db, logger),
		Metadata: NewMetadataRepository(db, logger),
		Users:    NewUserRepository(db, logger),
		ACL:      NewACLRepository(db, logger),
		Drift:    NewDriftRepository(db, logger),
	}, func() { _ = db.Close() }, nil
}
