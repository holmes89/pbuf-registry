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

func TestSQLite_CreateUser(t *testing.T) {
	r := suite.users
	ctx := context.Background()

	user := &model.User{
		Name:     "sqlite-create-user",
		Token:    "pbuf_user_sqlite-create-token",
		Type:     model.UserTypeUser,
		IsActive: true,
	}
	require.NoError(t, r.CreateUser(ctx, user))
	assert.NotEqual(t, uuid.Nil, user.ID)
	assert.False(t, user.CreatedAt.IsZero())

	dup := &model.User{Name: user.Name, Token: "pbuf_user_other-token", Type: model.UserTypeUser, IsActive: true}
	assert.Error(t, r.CreateUser(ctx, dup))
}

func TestSQLite_GetUser(t *testing.T) {
	r := suite.users
	ctx := context.Background()

	user := &model.User{Name: "sqlite-get-user", Token: "pbuf_user_sqlite-get-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, r.CreateUser(ctx, user))

	got, err := r.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, user.Name, got.Name)

	_, err = r.GetUser(ctx, uuid.New())
	assert.ErrorIs(t, err, data.ErrUserNotFound)
}

func TestSQLite_GetUserByToken(t *testing.T) {
	r := suite.users
	ctx := context.Background()
	token := "pbuf_user_sqlite-token-lookup"

	user := &model.User{Name: "sqlite-token-user", Token: token, Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, r.CreateUser(ctx, user))

	got, err := r.GetUserByToken(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)

	_, err = r.GetUserByToken(ctx, "wrong-token")
	assert.ErrorIs(t, err, data.ErrUserNotFound)

	// inactive user should not be returned
	require.NoError(t, r.SetUserActive(ctx, user.ID, false))
	_, err = r.GetUserByToken(ctx, token)
	assert.ErrorIs(t, err, data.ErrUserNotFound)
}

func TestSQLite_UpdateUser(t *testing.T) {
	r := suite.users
	ctx := context.Background()

	user := &model.User{Name: "sqlite-update-user", Token: "pbuf_user_sqlite-update-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, r.CreateUser(ctx, user))

	user.Name = "sqlite-update-user-renamed"
	require.NoError(t, r.UpdateUser(ctx, user))

	got, err := r.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "sqlite-update-user-renamed", got.Name)
}

func TestSQLite_DeleteUser(t *testing.T) {
	r := suite.users
	ctx := context.Background()

	user := &model.User{Name: "sqlite-delete-user", Token: "pbuf_user_sqlite-delete-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, r.CreateUser(ctx, user))

	require.NoError(t, r.DeleteUser(ctx, user.ID))
	assert.ErrorIs(t, r.DeleteUser(ctx, user.ID), data.ErrUserNotFound)
}

func TestSQLite_SetUserActive(t *testing.T) {
	r := suite.users
	ctx := context.Background()

	user := &model.User{Name: "sqlite-active-user", Token: "pbuf_user_sqlite-active-token", Type: model.UserTypeUser, IsActive: true}
	require.NoError(t, r.CreateUser(ctx, user))

	require.NoError(t, r.SetUserActive(ctx, user.ID, false))
	got, err := r.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, got.IsActive)

	require.NoError(t, r.SetUserActive(ctx, user.ID, true))
	got, err = r.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, got.IsActive)

	assert.ErrorIs(t, r.SetUserActive(ctx, uuid.New(), true), data.ErrUserNotFound)
}
