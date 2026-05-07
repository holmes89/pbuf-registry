package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/model"
)

type userRepo struct {
	repo
}

// NewUserRepository creates a new SQLite-backed UserRepository.
func NewUserRepository(db *sql.DB, logger log.Logger) data.UserRepository {
	return &userRepo{
		repo: repo{
			db:     db,
			logger: log.NewHelper(log.With(logger, "module", "data/sqlite/UserRepository")),
		},
	}
}

func (r *userRepo) CreateUser(ctx context.Context, user *model.User) error {
	id := uuid.New().String()
	hashedToken := sha256hex(user.Token)

	var idStr, createdAtStr, updatedAtStr string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO users (id, name, token, type, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		RETURNING id, created_at, updated_at`,
		id, user.Name, hashedToken, user.Type, boolToInt(user.IsActive),
	).Scan(&idStr, &createdAtStr, &updatedAtStr)
	if err != nil {
		return fmt.Errorf("could not insert user into database: %w", err)
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("could not parse user id: %w", err)
	}
	user.ID = parsedID
	user.CreatedAt = parseTime(createdAtStr)
	user.UpdatedAt = parseTime(updatedAtStr)
	return nil
}

func (r *userRepo) GetUser(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var user model.User
	var idStr, createdAtStr, updatedAtStr string
	var isActiveInt int

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, token, type, is_active, created_at, updated_at
		FROM users WHERE id = ?`,
		id.String(),
	).Scan(&idStr, &user.Name, &user.Token, &user.Type, &isActiveInt, &createdAtStr, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, data.ErrUserNotFound
		}
		return nil, fmt.Errorf("could not select user from database: %w", err)
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse user id: %w", err)
	}
	user.ID = parsedID
	user.IsActive = isActiveInt != 0
	user.CreatedAt = parseTime(createdAtStr)
	user.UpdatedAt = parseTime(updatedAtStr)
	return &user, nil
}

func (r *userRepo) GetUserByName(ctx context.Context, name string) (*model.User, error) {
	var user model.User
	var idStr, createdAtStr, updatedAtStr string
	var isActiveInt int

	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, token, type, is_active, created_at, updated_at
		FROM users WHERE name = ?`,
		name,
	).Scan(&idStr, &user.Name, &user.Token, &user.Type, &isActiveInt, &createdAtStr, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, data.ErrUserNotFound
		}
		return nil, fmt.Errorf("could not select user from database: %w", err)
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse user id: %w", err)
	}
	user.ID = parsedID
	user.IsActive = isActiveInt != 0
	user.CreatedAt = parseTime(createdAtStr)
	user.UpdatedAt = parseTime(updatedAtStr)
	return &user, nil
}

func (r *userRepo) GetUserByToken(ctx context.Context, token string) (*model.User, error) {
	var user model.User
	var idStr, createdAtStr, updatedAtStr string
	var isActiveInt int

	hashedToken := sha256hex(token)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, token, type, is_active, created_at, updated_at
		FROM users WHERE token = ? AND is_active = 1`,
		hashedToken,
	).Scan(&idStr, &user.Name, &user.Token, &user.Type, &isActiveInt, &createdAtStr, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, data.ErrUserNotFound
		}
		return nil, fmt.Errorf("could not select user from database: %w", err)
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse user id: %w", err)
	}
	user.ID = parsedID
	user.IsActive = isActiveInt != 0
	user.CreatedAt = parseTime(createdAtStr)
	user.UpdatedAt = parseTime(updatedAtStr)
	return &user, nil
}

func (r *userRepo) ListUsers(ctx context.Context, limit int, offset int) ([]*model.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, token, type, is_active, created_at, updated_at
		FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("could not select users from database: %w", err)
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		var user model.User
		var idStr, createdAtStr, updatedAtStr string
		var isActiveInt int

		if err := rows.Scan(&idStr, &user.Name, &user.Token, &user.Type, &isActiveInt, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("could not scan user: %w", err)
		}

		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("could not parse user id: %w", err)
		}
		user.ID = parsedID
		user.IsActive = isActiveInt != 0
		user.CreatedAt = parseTime(createdAtStr)
		user.UpdatedAt = parseTime(updatedAtStr)
		users = append(users, &user)
	}

	return users, nil
}

func (r *userRepo) UpdateUser(ctx context.Context, user *model.User) error {
	hashedToken := sha256hex(user.Token)
	result, err := r.db.ExecContext(ctx,
		`UPDATE users SET name = ?, token = ?, type = ?, is_active = ?, updated_at = datetime('now')
		WHERE id = ?`,
		user.Name, hashedToken, user.Type, boolToInt(user.IsActive), user.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("could not update user in database: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if n == 0 {
		return data.ErrUserNotFound
	}
	return nil
}

func (r *userRepo) DeleteUser(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM users WHERE id = ?`,
		id.String(),
	)
	if err != nil {
		return fmt.Errorf("could not delete user from database: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if n == 0 {
		return data.ErrUserNotFound
	}
	return nil
}

func (r *userRepo) SetUserActive(ctx context.Context, id uuid.UUID, isActive bool) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`,
		boolToInt(isActive), id.String(),
	)
	if err != nil {
		return fmt.Errorf("could not update user active status in database: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if n == 0 {
		return data.ErrUserNotFound
	}
	return nil
}
