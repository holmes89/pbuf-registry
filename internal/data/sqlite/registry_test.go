package sqlite

import (
	"context"
	"testing"

	v1 "github.com/pbufio/pbuf-registry/gen/pbuf-registry/v1"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_RegisterModule(t *testing.T) {
	r := suite.registry
	ctx := context.Background()

	require.NoError(t, r.RegisterModule(ctx, "test/register-idempotent"))
	require.NoError(t, r.RegisterModule(ctx, "test/register-idempotent")) // idempotent

	module, err := r.GetModule(ctx, "test/register-idempotent")
	require.NoError(t, err)
	require.NotNil(t, module)
	assert.Equal(t, "test/register-idempotent", module.Name)
}

func TestSQLite_GetModuleNotFound(t *testing.T) {
	module, err := suite.registry.GetModule(context.Background(), "test/does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, module)
}

func TestSQLite_PushAndPullModule(t *testing.T) {
	r := suite.registry
	ctx := context.Background()
	name := "test/push-pull"

	require.NoError(t, r.RegisterModule(ctx, name))

	protofiles := []*v1.ProtoFile{
		{Filename: "service.proto", Content: `syntax = "proto3"; package test;`},
	}

	module, err := r.PushModule(ctx, name, "v1.0.0", protofiles)
	require.NoError(t, err)
	assert.Contains(t, module.Tags, "v1.0.0")

	// duplicate tag is an error
	_, err = r.PushModule(ctx, name, "v1.0.0", protofiles)
	assert.Error(t, err)

	_, pulled, err := r.PullModule(ctx, name, "v1.0.0")
	require.NoError(t, err)
	require.Len(t, pulled, 1)
	assert.Equal(t, "service.proto", pulled[0].Filename)
}

func TestSQLite_DeleteModuleTag(t *testing.T) {
	r := suite.registry
	ctx := context.Background()
	name := "test/delete-tag"

	require.NoError(t, r.RegisterModule(ctx, name))
	_, err := r.PushModule(ctx, name, "v1.0.0", []*v1.ProtoFile{{Filename: "a.proto", Content: "syntax=\"proto3\";"}})
	require.NoError(t, err)

	require.NoError(t, r.DeleteModuleTag(ctx, name, "v1.0.0"))
	assert.ErrorIs(t, r.DeleteModuleTag(ctx, name, "v1.0.0"), data.ErrTagNotFound)
}

func TestSQLite_DeleteModule(t *testing.T) {
	r := suite.registry
	ctx := context.Background()
	name := "test/delete-module"

	require.NoError(t, r.RegisterModule(ctx, name))
	require.NoError(t, r.DeleteModule(ctx, name))

	module, err := r.GetModule(ctx, name)
	require.NoError(t, err)
	assert.Nil(t, module)
}

func TestSQLite_ListModules(t *testing.T) {
	r := suite.registry
	ctx := context.Background()

	for _, n := range []string{"test/list-a", "test/list-b", "test/list-c"} {
		require.NoError(t, r.RegisterModule(ctx, n))
	}

	modules, _, err := r.ListModules(ctx, 100, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(modules), 3)
}

func TestSQLite_PushDraftAndPull(t *testing.T) {
	r := suite.registry
	ctx := context.Background()
	name := "test/draft"

	require.NoError(t, r.RegisterModule(ctx, name))

	protofiles := []*v1.ProtoFile{{Filename: "draft.proto", Content: "syntax=\"proto3\";"}}
	module, err := r.PushDraftModule(ctx, name, "draft/feature", protofiles, nil)
	require.NoError(t, err)
	assert.Contains(t, module.DraftTags, "draft/feature")

	// upsert is idempotent
	_, err = r.PushDraftModule(ctx, name, "draft/feature", protofiles, nil)
	require.NoError(t, err)

	_, pulled, err := r.PullDraftModule(ctx, name, "draft/feature")
	require.NoError(t, err)
	require.Len(t, pulled, 1)
	assert.Equal(t, "draft.proto", pulled[0].Filename)
}
