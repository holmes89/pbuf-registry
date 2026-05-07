package sqlite

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_GrantAndCheckPermission(t *testing.T) {
	ctx := context.Background()

	user := &model.User{Name: "acl-grant-user", Token: "pbuf_user_acl-grant-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, suite.users.CreateUser(ctx, user))

	entry := &model.ACLEntry{UserID: user.ID, ModuleName: "test/acl-module", Permission: model.PermissionWrite}
	require.NoError(t, suite.acl.GrantPermission(ctx, entry))
	assert.NotEqual(t, uuid.Nil, entry.ID)

	ok, err := suite.acl.CheckPermission(ctx, user.ID, "test/acl-module", model.PermissionRead)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = suite.acl.CheckPermission(ctx, user.ID, "test/acl-module", model.PermissionAdmin)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = suite.acl.CheckPermission(ctx, user.ID, "test/no-such-module", model.PermissionRead)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSQLite_RevokePermission(t *testing.T) {
	ctx := context.Background()

	user := &model.User{Name: "acl-revoke-user", Token: "pbuf_user_acl-revoke-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, suite.users.CreateUser(ctx, user))

	entry := &model.ACLEntry{UserID: user.ID, ModuleName: "test/revoke-module", Permission: model.PermissionWrite}
	require.NoError(t, suite.acl.GrantPermission(ctx, entry))
	require.NoError(t, suite.acl.RevokePermission(ctx, user.ID, "test/revoke-module"))

	ok, err := suite.acl.CheckPermission(ctx, user.ID, "test/revoke-module", model.PermissionRead)
	require.NoError(t, err)
	assert.False(t, ok)

	assert.ErrorIs(t, suite.acl.RevokePermission(ctx, user.ID, "test/revoke-module"), data.ErrPermissionNotFound)
}

func TestSQLite_WildcardPermission(t *testing.T) {
	ctx := context.Background()

	user := &model.User{Name: "acl-wildcard-user", Token: "pbuf_user_acl-wildcard-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, suite.users.CreateUser(ctx, user))

	entry := &model.ACLEntry{UserID: user.ID, ModuleName: "*", Permission: model.PermissionAdmin}
	require.NoError(t, suite.acl.GrantPermission(ctx, entry))

	ok, err := suite.acl.CheckPermission(ctx, user.ID, "any/module/name", model.PermissionWrite)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSQLite_ListAndDeleteUserPermissions(t *testing.T) {
	ctx := context.Background()

	user := &model.User{Name: "acl-list-user", Token: "pbuf_user_acl-list-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, suite.users.CreateUser(ctx, user))

	for _, mod := range []string{"test/m1", "test/m2", "test/m3"} {
		e := &model.ACLEntry{UserID: user.ID, ModuleName: mod, Permission: model.PermissionRead}
		require.NoError(t, suite.acl.GrantPermission(ctx, e))
	}

	entries, err := suite.acl.ListUserPermissions(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	require.NoError(t, suite.acl.DeleteUserPermissions(ctx, user.ID))
	entries, err = suite.acl.ListUserPermissions(ctx, user.ID)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
