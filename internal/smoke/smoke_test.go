// Package smoke provides end-to-end integration tests that wire the real SQLite
// repositories directly to the real gRPC server handlers.  No mocks are used.
// Three flows are verified:
//
//  1. Push / pull a proto module
//  2. Auth round-trip: create user → token persisted → ACL grant / check
//  3. Drift detection: push two versions → inject drift events → query → acknowledge
package smoke

import (
	"context"
	"database/sql"
	"io"
	stdlog "log"
	"net"
	"os"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	v1 "github.com/pbufio/pbuf-registry/gen/pbuf-registry/v1"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/data/sqlite"
	"github.com/pbufio/pbuf-registry/internal/model"
	"github.com/pbufio/pbuf-registry/internal/server"
	"github.com/pbufio/pbuf-registry/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// smokeEnv holds all repos and gRPC clients for the test suite.
type smokeEnv struct {
	db       *sql.DB
	registry data.RegistryRepository
	users    data.UserRepository
	acl      data.ACLRepository
	drift    data.DriftRepository
	metadata data.MetadataRepository

	regClient   v1.RegistryClient
	userClient  v1.UserServiceClient
	driftClient v1.DriftServiceClient

	closer func()
}

var env smokeEnv

func TestMain(m *testing.M) {
	f, err := os.CreateTemp("", "pbuf-smoke-*.db")
	if err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	dbPath := f.Name()

	db, err := sqlite.Open(dbPath)
	if err != nil {
		panic(err)
	}
	migrations.MigrateSQLite(db)

	logger := log.NewStdLogger(io.Discard)

	reg := sqlite.NewRegistryRepository(db, log.DefaultLogger)
	users := sqlite.NewUserRepository(db, log.DefaultLogger)
	acl := sqlite.NewACLRepository(db, log.DefaultLogger)
	drift := sqlite.NewDriftRepository(db, log.DefaultLogger)
	meta := sqlite.NewMetadataRepository(db, log.DefaultLogger)

	env = smokeEnv{
		db:       db,
		registry: reg,
		users:    users,
		acl:      acl,
		drift:    drift,
		metadata: meta,
	}

	// Build a bufconn gRPC server wired to the real handlers.
	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)
	grpcSrv := grpc.NewServer()

	v1.RegisterRegistryServer(grpcSrv, server.NewRegistryServer(reg, meta, logger))
	v1.RegisterUserServiceServer(grpcSrv, server.NewUsersServer(users, acl, logger))
	v1.RegisterDriftServiceServer(grpcSrv, server.NewDriftServer(drift, logger))
	v1.RegisterMetadataServiceServer(grpcSrv, server.NewMetadataServer(reg, meta, logger))

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			stdlog.Printf("smoke grpc serve: %v", err)
		}
	}()

	dial := func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dial),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}

	env.regClient = v1.NewRegistryClient(conn)
	env.userClient = v1.NewUserServiceClient(conn)
	env.driftClient = v1.NewDriftServiceClient(conn)
	env.closer = func() {
		_ = conn.Close()
		_ = lis.Close()
		grpcSrv.Stop()
		_ = db.Close()
		_ = os.Remove(dbPath)
	}

	code := m.Run()
	env.closer()
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// 1. Push / pull module
// ---------------------------------------------------------------------------

func TestSmoke_PushPullModule(t *testing.T) {
	ctx := context.Background()
	name := "smoke/push-pull"

	// Register is idempotent.
	mod, err := env.regClient.RegisterModule(ctx, &v1.RegisterModuleRequest{Name: name})
	require.NoError(t, err)
	assert.Equal(t, name, mod.GetName())

	protofiles := []*v1.ProtoFile{
		{Filename: "hello.proto", Content: `syntax = "proto3"; package smoke; message Hello { string msg = 1; }`},
		{Filename: "world.proto", Content: `syntax = "proto3"; package smoke; message World { int32 id = 1; }`},
	}

	pushed, err := env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "v1.0.0",
		Protofiles: protofiles,
	})
	require.NoError(t, err)
	assert.Contains(t, pushed.GetTags(), "v1.0.0")

	// Duplicate tag must fail.
	_, err = env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "v1.0.0",
		Protofiles: protofiles,
	})
	require.Error(t, err, "pushing duplicate tag must return an error")

	pulled, err := env.regClient.PullModule(ctx, &v1.PullModuleRequest{Name: name, Tag: "v1.0.0"})
	require.NoError(t, err)
	require.Len(t, pulled.GetProtofiles(), 2)

	byName := map[string]string{}
	for _, pf := range pulled.GetProtofiles() {
		byName[pf.GetFilename()] = pf.GetContent()
	}
	assert.Contains(t, byName["hello.proto"], "message Hello")
	assert.Contains(t, byName["world.proto"], "message World")
}

func TestSmoke_PushPullDraftModule(t *testing.T) {
	ctx := context.Background()
	name := "smoke/draft"

	_, err := env.regClient.RegisterModule(ctx, &v1.RegisterModuleRequest{Name: name})
	require.NoError(t, err)

	pf := []*v1.ProtoFile{{Filename: "draft.proto", Content: `syntax = "proto3"; package smoke;`}}

	pushed, err := env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "draft/feature-x",
		Protofiles: pf,
		IsDraft:    true,
	})
	require.NoError(t, err)
	assert.Contains(t, pushed.GetDraftTags(), "draft/feature-x")

	// upsert: pushing the same draft tag again must succeed
	_, err = env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "draft/feature-x",
		Protofiles: pf,
		IsDraft:    true,
	})
	require.NoError(t, err)

	pulled, err := env.regClient.PullModule(ctx, &v1.PullModuleRequest{Name: name, Tag: "draft/feature-x"})
	require.NoError(t, err)
	require.Len(t, pulled.GetProtofiles(), 1)
	assert.Equal(t, "draft.proto", pulled.GetProtofiles()[0].GetFilename())
}

// ---------------------------------------------------------------------------
// 2. Auth round-trip
// ---------------------------------------------------------------------------

// TestSmoke_AuthRoundTrip verifies that:
//   - CreateUser via the gRPC handler generates a token and persists it to SQLite
//   - The raw token returned is retrievable via GetUserByToken (confirming DB storage)
//   - ACL grant / check / revoke cycle works end-to-end through the real repos
func TestSmoke_AuthRoundTrip(t *testing.T) {
	ctx := context.Background()

	// --- Create user ---
	resp, err := env.userClient.CreateUser(ctx, &v1.CreateUserRequest{
		Name: "smoke-bot",
		Type: v1.UserType_USER_TYPE_BOT,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetToken(), "token must be returned on creation")
	assert.Equal(t, "smoke-bot", resp.GetUser().GetName())

	userID, err := uuid.Parse(resp.GetUser().GetId())
	require.NoError(t, err)

	// --- Token is persisted and retrievable ---
	token := resp.GetToken()
	found, err := env.users.GetUserByToken(ctx, token)
	require.NoError(t, err, "token returned by CreateUser must be resolvable in SQLite")
	assert.Equal(t, userID, found.ID)
	assert.Equal(t, "smoke-bot", found.Name)

	// --- GetUser via gRPC ---
	got, err := env.userClient.GetUser(ctx, &v1.GetUserRequest{Id: userID.String()})
	require.NoError(t, err)
	assert.Equal(t, "smoke-bot", got.GetName())
	assert.True(t, got.GetIsActive())

	// --- Grant write permission on a module ---
	grantResp, err := env.userClient.GrantPermission(ctx, &v1.GrantPermissionRequest{
		UserId:     userID.String(),
		ModuleName: "smoke/auth-module",
		Permission: v1.Permission_PERMISSION_WRITE,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, grantResp.GetEntry().GetId())

	// --- Confirm write implies read via the repo ---
	ok, err := env.acl.CheckPermission(ctx, userID, "smoke/auth-module", model.PermissionRead)
	require.NoError(t, err)
	assert.True(t, ok, "write permission must satisfy read check")

	ok, err = env.acl.CheckPermission(ctx, userID, "smoke/auth-module", model.PermissionAdmin)
	require.NoError(t, err)
	assert.False(t, ok, "write permission must not satisfy admin check")

	// Revoke and confirm no longer accessible.
	_, err = env.userClient.RevokePermission(ctx, &v1.RevokePermissionRequest{
		UserId:     userID.String(),
		ModuleName: "smoke/auth-module",
	})
	require.NoError(t, err)

	ok, err = env.acl.CheckPermission(ctx, userID, "smoke/auth-module", model.PermissionRead)
	require.NoError(t, err)
	assert.False(t, ok, "permission must be gone after revoke")

	// --- Token regeneration ---
	regenResp, err := env.userClient.RegenerateToken(ctx, &v1.RegenerateTokenRequest{Id: userID.String()})
	require.NoError(t, err)
	newToken := regenResp.GetToken()
	assert.NotEmpty(t, newToken)
	assert.NotEqual(t, token, newToken, "regenerated token must differ from the original")

	// Old token must no longer work.
	_, err = env.users.GetUserByToken(ctx, token)
	assert.ErrorIs(t, err, data.ErrUserNotFound, "old token must be invalidated after regeneration")

	// New token must work.
	found2, err := env.users.GetUserByToken(ctx, newToken)
	require.NoError(t, err)
	assert.Equal(t, userID, found2.ID)
}

// ---------------------------------------------------------------------------
// 3. Drift detection
// ---------------------------------------------------------------------------

// TestSmoke_DriftDetection verifies that:
//   - Two versions of a module can be pushed
//   - Hash computation records file hashes in SQLite
//   - Drift events can be saved and queried via the DriftServer gRPC handler
//   - Events can be acknowledged via gRPC
func TestSmoke_DriftDetection(t *testing.T) {
	ctx := context.Background()
	name := "smoke/drift"

	// Push v1.
	_, err := env.regClient.RegisterModule(ctx, &v1.RegisterModuleRequest{Name: name})
	require.NoError(t, err)

	v1Files := []*v1.ProtoFile{
		{Filename: "svc.proto", Content: `syntax = "proto3"; package drift; service Svc { rpc Ping(Req) returns (Resp); } message Req {} message Resp {}`},
	}
	_, err = env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "v1.0.0",
		Protofiles: v1Files,
	})
	require.NoError(t, err)

	// Push v2 — modified content for svc.proto.
	v2Files := []*v1.ProtoFile{
		{Filename: "svc.proto", Content: `syntax = "proto3"; package drift; service Svc { rpc Ping(Req) returns (Resp); rpc Pong(Req) returns (Resp); } message Req {} message Resp {}`},
	}
	_, err = env.regClient.PushModule(ctx, &v1.PushModuleRequest{
		ModuleName: name,
		Tag:        "v2.0.0",
		Protofiles: v2Files,
	})
	require.NoError(t, err)

	// Compute and store hashes for both tags (normally done by the background daemon).
	tagIDv1, err := env.registry.GetModuleTagId(ctx, name, "v1.0.0")
	require.NoError(t, err)
	tagIDv2, err := env.registry.GetModuleTagId(ctx, name, "v2.0.0")
	require.NoError(t, err)

	require.NoError(t, env.drift.ComputeAndStoreHashes(ctx, tagIDv1))
	require.NoError(t, env.drift.ComputeAndStoreHashes(ctx, tagIDv2))

	hashesV1, err := env.drift.GetFileHashesForTag(ctx, tagIDv1)
	require.NoError(t, err)
	hashesV2, err := env.drift.GetFileHashesForTag(ctx, tagIDv2)
	require.NoError(t, err)
	assert.NotEqual(t, hashesV1["svc.proto"], hashesV2["svc.proto"], "modified file must produce different hashes")

	// Inject a drift event representing the modification (normally done by the daemon).
	module, err := env.registry.GetModule(ctx, name)
	require.NoError(t, err)
	events := []model.DriftEvent{
		{
			ModuleID:     module.Id,
			ModuleName:   name,
			TagID:        tagIDv2,
			TagName:      "v2.0.0",
			Filename:     "svc.proto",
			EventType:    model.DriftEventTypeModified,
			PreviousHash: hashesV1["svc.proto"],
			CurrentHash:  hashesV2["svc.proto"],
			Severity:     model.DriftSeverityWarning,
		},
	}
	require.NoError(t, env.drift.SaveDriftEvents(ctx, events))

	// Query via gRPC server.
	tagName := "v2.0.0"
	modEventsResp, err := env.driftClient.GetModuleDriftEvents(ctx, &v1.GetModuleDriftEventsRequest{
		ModuleName: name,
		TagName:    &tagName,
	})
	require.NoError(t, err)
	require.NotEmpty(t, modEventsResp.GetEvents(), "must find at least one drift event")

	var targetEvent *v1.DriftEvent
	for _, e := range modEventsResp.GetEvents() {
		if e.GetFilename() == "svc.proto" {
			targetEvent = e
			break
		}
	}
	require.NotNil(t, targetEvent, "drift event for svc.proto must be present")
	assert.Equal(t, v1.DriftEventType_DRIFT_EVENT_TYPE_MODIFIED, targetEvent.GetEventType())
	assert.Equal(t, v1.DriftSeverity_DRIFT_SEVERITY_WARNING, targetEvent.GetSeverity())
	assert.False(t, targetEvent.GetAcknowledged())

	// ListDriftEvents (global unacknowledged view) must also include it.
	listResp, err := env.driftClient.ListDriftEvents(ctx, &v1.ListDriftEventsRequest{})
	require.NoError(t, err)
	found := false
	for _, e := range listResp.GetEvents() {
		if e.GetId() == targetEvent.GetId() {
			found = true
			break
		}
	}
	assert.True(t, found, "event must appear in global unacknowledged list")

	// Acknowledge it.
	ackResp, err := env.driftClient.AcknowledgeDriftEvent(ctx, &v1.AcknowledgeDriftEventRequest{
		EventId:        targetEvent.GetId(),
		AcknowledgedBy: "smoke-test",
	})
	require.NoError(t, err)
	assert.True(t, ackResp.GetEvent().GetAcknowledged())
	assert.Equal(t, "smoke-test", ackResp.GetEvent().GetAcknowledgedBy())

	// Must no longer appear in the unacknowledged list.
	listResp2, err := env.driftClient.ListDriftEvents(ctx, &v1.ListDriftEventsRequest{})
	require.NoError(t, err)
	for _, e := range listResp2.GetEvents() {
		assert.NotEqual(t, targetEvent.GetId(), e.GetId(), "acknowledged event must not appear in unacknowledged list")
	}

	// GetModuleDriftEvents still includes it (acknowledged = true).
	modEventsResp2, err := env.driftClient.GetModuleDriftEvents(ctx, &v1.GetModuleDriftEventsRequest{
		ModuleName: name,
		TagName:    &tagName,
	})
	require.NoError(t, err)
	var acked *v1.DriftEvent
	for _, e := range modEventsResp2.GetEvents() {
		if e.GetId() == targetEvent.GetId() {
			acked = e
			break
		}
	}
	require.NotNil(t, acked, "event must still appear in module-scoped query after acknowledgment")
	assert.True(t, acked.GetAcknowledged())
}
