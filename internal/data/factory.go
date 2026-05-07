package data

import (
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
)

// RepositorySet bundles all repository implementations for a database backend.
type RepositorySet struct {
	Registry RegistryRepository
	Metadata MetadataRepository
	Users    UserRepository
	ACL      ACLRepository
	Drift    DriftRepository
}

type driverFactory func(dsn string, logger log.Logger) (RepositorySet, func(), error)

var registeredDrivers = map[string]driverFactory{}

// RegisterDriver registers a backend factory under the given driver name.
// Backend packages call this from their init() function.
func RegisterDriver(name string, f driverFactory) {
	registeredDrivers[name] = f
}

// Open returns a RepositorySet and close function for the given driver and DSN.
// The caller must invoke the returned func when done to release database resources.
// Defaults to "postgres" when driver is empty.
func Open(driver, dsn string, logger log.Logger) (RepositorySet, func(), error) {
	if driver == "" {
		driver = "postgres"
	}
	f, ok := registeredDrivers[driver]
	if !ok {
		return RepositorySet{}, nil, fmt.Errorf("unknown database driver %q — import the driver package to register it", driver)
	}
	return f(dsn, logger)
}
