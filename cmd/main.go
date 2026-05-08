package main

import (
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/pbufio/pbuf-registry/internal/background"
	"github.com/pbufio/pbuf-registry/internal/config"
	"github.com/pbufio/pbuf-registry/internal/data"
	_ "github.com/pbufio/pbuf-registry/internal/data/sqlite" // register sqlite driver
	"github.com/pbufio/pbuf-registry/internal/server"
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

	repos, closeDB, err := data.Open(config.Cfg.Data.Database.Driver, config.Cfg.Data.Database.DSN, logger)
	if err != nil {
		logHelper.Errorf("failed to open database: %v", err)
		return
	}
	defer closeDB()

	registryServer := server.NewRegistryServer(repos.Registry, repos.Metadata, logger)
	metadataServer := server.NewMetadataServer(repos.Registry, repos.Metadata, logger)
	usersServer := server.NewUsersServer(repos.Users, repos.ACL, logger)
	driftServer := server.NewDriftServer(repos.Drift, logger)

	app := kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(
			server.NewGRPCServer(&config.Cfg.Server, registryServer, metadataServer, usersServer, driftServer, repos.Users, repos.ACL, logger),
			server.NewHTTPServer(&config.Cfg.Server, registryServer, metadataServer, usersServer, driftServer, repos.Users, repos.ACL, logger),
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

		compactionDaemon:   background.NewCompactionDaemon(repos.Registry, logger),
		protoParsingDaemon: background.NewProtoParsingDaemon(repos.Metadata, repos.Drift, logger),
	}

	err = CreateRootCommand(launcher).Execute()
	if err != nil {
		logHelper.Errorf("failed to run application: %v", err)
	}
}
