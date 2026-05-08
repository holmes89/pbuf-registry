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

type aclRepo struct {
	repo
}

// NewACLRepository creates a new SQLite-backed ACLRepository.
func NewACLRepository(db *sql.DB, logger log.Logger) data.ACLRepository {
	return &aclRepo{
		repo: repo{
			db:     db,
			logger: log.NewHelper(log.With(logger, "module", "data/sqlite/ACLRepository")),
		},
	}
}

func (r *aclRepo) GrantPermission(ctx context.Context, entry *model.ACLEntry) error {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO acl (id, user_id, module_name, permission, created_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT (user_id, module_name) DO UPDATE SET permission = excluded.permission`,
		id, entry.UserID.String(), entry.ModuleName, entry.Permission,
	)
	if err != nil {
		return fmt.Errorf("could not grant permission in database: %w", err)
	}

	// SELECT back to get the actual id and created_at (which may differ if conflict updated)
	var idStr, createdAtStr string
	err = r.db.QueryRowContext(ctx,
		`SELECT id, created_at FROM acl WHERE user_id = ? AND module_name = ?`,
		entry.UserID.String(), entry.ModuleName,
	).Scan(&idStr, &createdAtStr)
	if err != nil {
		return fmt.Errorf("could not fetch acl entry after grant: %w", err)
	}

	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("could not parse acl id: %w", err)
	}
	entry.ID = parsedID
	entry.CreatedAt = parseTime(createdAtStr)
	return nil
}

func (r *aclRepo) RevokePermission(ctx context.Context, userID uuid.UUID, moduleName string) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM acl WHERE user_id = ? AND module_name = ?`,
		userID.String(), moduleName,
	)
	if err != nil {
		return fmt.Errorf("could not revoke permission from database: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if n == 0 {
		return data.ErrPermissionNotFound
	}
	return nil
}

func (r *aclRepo) ListUserPermissions(ctx context.Context, userID uuid.UUID) ([]*model.ACLEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, module_name, permission, created_at
		FROM acl WHERE user_id = ? ORDER BY created_at DESC`,
		userID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("could not select permissions from database: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*model.ACLEntry
	for rows.Next() {
		var entry model.ACLEntry
		var idStr, userIDStr, createdAtStr string

		if err := rows.Scan(&idStr, &userIDStr, &entry.ModuleName, &entry.Permission, &createdAtStr); err != nil {
			return nil, fmt.Errorf("could not scan permission: %w", err)
		}

		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("could not parse acl id: %w", err)
		}
		parsedUserID, err := uuid.Parse(userIDStr)
		if err != nil {
			return nil, fmt.Errorf("could not parse user id: %w", err)
		}

		entry.ID = parsedID
		entry.UserID = parsedUserID
		entry.CreatedAt = parseTime(createdAtStr)
		entries = append(entries, &entry)
	}

	return entries, nil
}

func (r *aclRepo) CheckPermission(ctx context.Context, userID uuid.UUID, moduleName string, permission model.Permission) (bool, error) {
	var foundPermission model.Permission
	err := r.db.QueryRowContext(ctx,
		`SELECT permission FROM acl WHERE user_id = ? AND module_name = ?`,
		userID.String(), moduleName,
	).Scan(&foundPermission)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("could not check permission in database: %w", err)
	}
	if err == nil {
		return hasRequiredPermission(foundPermission, permission), nil
	}

	// Check for wildcard permission
	err = r.db.QueryRowContext(ctx,
		`SELECT permission FROM acl WHERE user_id = ? AND module_name = '*'`,
		userID.String(),
	).Scan(&foundPermission)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("could not check wildcard permission in database: %w", err)
	}

	return hasRequiredPermission(foundPermission, permission), nil
}

func (r *aclRepo) DeleteUserPermissions(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM acl WHERE user_id = ?`,
		userID.String(),
	)
	if err != nil {
		return fmt.Errorf("could not delete user permissions from database: %w", err)
	}
	return nil
}

// hasRequiredPermission checks if the granted permission meets the required permission level.
// admin > write > read
func hasRequiredPermission(granted, required model.Permission) bool {
	permissionLevel := map[model.Permission]int{
		model.PermissionRead:  1,
		model.PermissionWrite: 2,
		model.PermissionAdmin: 3,
	}
	return permissionLevel[granted] >= permissionLevel[required]
}
