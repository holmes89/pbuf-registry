package main

import (
	"context"
	"database/sql"
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/pbufio/pbuf-registry/internal/background"
	"github.com/pbufio/pbuf-registry/internal/config"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/data/sqlite"
	"github.com/pbufio/pbuf-registry/internal/server"
	"github.com/pbufio/pbuf-registry/migrations"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name is the name of the compiled software.
	Name string
	// Version is the version of the compiled software.
	Version string

	id, _ = os.Hostname()
)

type Launcher struct {
	config *config.Config

	mainApp  *kratos.App
	debugApp *kratos.App

	compactionDaemon   background.Daemon
	protoParsingDaemon background.Daemon
}

func main() {
	config.NewLoader().MustLoad()

	logger := log.DefaultLogger
	logHelper := log.NewHelper(logger)

	var (
		registryRepository data.RegistryRepository
		metadataRepository data.MetadataRepository
		userRepository     data.UserRepository
		aclRepository      data.ACLRepository
		driftRepository    data.DriftRepository
	)

	driver := config.Cfg.Data.Database.Driver
	if driver == "" {
		driver = "postgres"
	}

	switch driver {
	case "sqlite":
		db, err := sqlite.Open(config.Cfg.Data.Database.DSN)
		if err != nil {
			logHelper.Errorf("failed to open sqlite database: %v", err)
			return
		}
		defer db.Close()
		migrations.MigrateSQLite(db)
		registryRepository = sqlite.NewRegistryRepository(db, logger)
		metadataRepository = sqlite.NewMetadataRepository(db, logger)
		userRepository = sqlite.NewUserRepository(db, logger)
		aclRepository = sqlite.NewACLRepository(db, logger)
		driftRepository = sqlite.NewDriftRepository(db, logger)

	default:
		pool, err := pgxpool.New(context.Background(), config.Cfg.Data.Database.DSN)
		if err != nil {
			logHelper.Errorf("failed to connect to database: %v", err)
			return
		}
		defer pool.Close()

		sqlDB, err := sql.Open("postgres", config.Cfg.Data.Database.DSN)
		if err != nil {
			logHelper.Errorf("failed to open sql.DB for migrations: %v", err)
			return
		}
		migrations.Migrate(sqlDB)
		sqlDB.Close()

		registryRepository = data.NewRegistryRepository(pool, logger)
		metadataRepository = data.NewMetadataRepository(pool, logger)
		userRepository = data.NewUserRepository(pool, logger)
		aclRepository = data.NewACLRepository(pool, logger)
		driftRepository = data.NewDriftRepository(pool, logger)
	}

	registryServer := server.NewRegistryServer(registryRepository, metadataRepository, logger)
	metadataServer := server.NewMetadataServer(registryRepository, metadataRepository, logger)
	usersServer := server.NewUsersServer(userRepository, aclRepository, logger)
	driftServer := server.NewDriftServer(driftRepository, logger)

	app := kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			server.NewGRPCServer(&config.Cfg.Server, registryServer, metadataServer, usersServer, driftServer, userRepository, aclRepository, logger),
			server.NewHTTPServer(&config.Cfg.Server, registryServer, metadataServer, usersServer, driftServer, userRepository, aclRepository, logger),
			server.NewDebugServer(&config.Cfg.Server, logger),
		),
	)

	debugApp := kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			server.NewDebugServer(&config.Cfg.Server, logger),
		),
	)

	launcher := &Launcher{
		config: config.Cfg,

		mainApp:  app,
		debugApp: debugApp,

		compactionDaemon:   background.NewCompactionDaemon(registryRepository, logger),
		protoParsingDaemon: background.NewProtoParsingDaemon(metadataRepository, driftRepository, logger),
	}

	err := CreateRootCommand(launcher).Execute()
	if err != nil {
		logHelper.Errorf("failed to run application: %v", err)
	}
}
