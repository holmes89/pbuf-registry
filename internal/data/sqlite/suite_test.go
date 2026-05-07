package sqlite

import (
	"database/sql"
	"os"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/migrations"
)

type testSuite struct {
	db       *sql.DB
	registry data.RegistryRepository
	metadata data.MetadataRepository
	users    data.UserRepository
	acl      data.ACLRepository
	drift    data.DriftRepository
}

var suite testSuite

func TestMain(m *testing.M) {
	f, err := os.CreateTemp("", "pbuf-sqlite-test-*.db")
	if err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	path := f.Name()

	db, err := Open(path)
	if err != nil {
		panic(err)
	}

	migrations.MigrateSQLite(db)

	suite = testSuite{
		db:       db,
		registry: NewRegistryRepository(db, log.DefaultLogger),
		metadata: NewMetadataRepository(db, log.DefaultLogger),
		users:    NewUserRepository(db, log.DefaultLogger),
		acl:      NewACLRepository(db, log.DefaultLogger),
		drift:    NewDriftRepository(db, log.DefaultLogger),
	}

	code := m.Run()
	_ = db.Close()
	_ = os.Remove(path)
	os.Exit(code)
}
