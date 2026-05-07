package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	v1 "github.com/pbufio/pbuf-registry/gen/pbuf-registry/v1"
	"github.com/pbufio/pbuf-registry/internal/data"
)

type registryRepo struct {
	repo
}

// NewRegistryRepository creates a new SQLite-backed RegistryRepository.
func NewRegistryRepository(db *sql.DB, logger log.Logger) data.RegistryRepository {
	return &registryRepo{
		repo: repo{
			db:     db,
			logger: log.NewHelper(log.With(logger, "module", "data/sqlite/RegistryRepository")),
		},
	}
}

func (r *registryRepo) RegisterModule(ctx context.Context, moduleName string) error {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO modules (id, name) VALUES (?, ?) ON CONFLICT (name) DO NOTHING",
		id, moduleName)
	if err != nil {
		return fmt.Errorf("could not insert module into database: %w", err)
	}
	return nil
}

func (r *registryRepo) GetModule(ctx context.Context, name string) (*v1.Module, error) {
	var module v1.Module
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name FROM modules WHERE name = ?",
		name).Scan(&module.Id, &module.Name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not select module from database: %w", err)
	}

	// fetch tags
	tags, err := r.db.QueryContext(ctx,
		"SELECT tag FROM tags WHERE module_id = ? ORDER BY updated_at DESC LIMIT 10",
		module.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			r.logger.Infof("no tags found for module %s", name)
			return &module, nil
		}
		return nil, fmt.Errorf("could not select tags from database: %w", err)
	}
	defer tags.Close()

	for tags.Next() {
		var tag string
		if err := tags.Scan(&tag); err != nil {
			return nil, fmt.Errorf("could not scan tag: %w", err)
		}
		module.Tags = append(module.Tags, tag)
	}

	// fetch draft tags
	draftTags, err := r.db.QueryContext(ctx,
		"SELECT tag FROM draft_tags WHERE module_id = ? ORDER BY updated_at DESC LIMIT 10",
		module.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			r.logger.Infof("no draft tags found for module %s", name)
			return &module, nil
		}
		return nil, fmt.Errorf("could not select draft tags from database: %w", err)
	}
	defer draftTags.Close()

	for draftTags.Next() {
		var tag string
		if err := draftTags.Scan(&tag); err != nil {
			return nil, fmt.Errorf("could not scan draft tag: %w", err)
		}
		module.DraftTags = append(module.DraftTags, tag)
	}

	return &module, nil
}

// ListModules returns a list of modules with paging support.
// Token is the base64 encoded module name.
func (r *registryRepo) ListModules(ctx context.Context, pageSize int, token string) ([]*v1.Module, string, error) {
	var modules []*v1.Module

	query := "SELECT id, name FROM modules"
	if token != "" {
		decoded, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			return nil, "", fmt.Errorf("could not decode token: %w", err)
		}
		query += fmt.Sprintf(" WHERE name >= '%s'", decoded)
	}

	query += fmt.Sprintf(" ORDER BY name ASC LIMIT %d", pageSize+1)

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, "", fmt.Errorf("could not select modules from database: %w", err)
	}
	defer rows.Close()

	var rowsCount int
	var nextPageToken string

	for rows.Next() {
		module := &v1.Module{}
		if err := rows.Scan(&module.Id, &module.Name); err != nil {
			return nil, "", fmt.Errorf("could not scan module: %w", err)
		}

		if rowsCount < pageSize {
			modules = append(modules, module)
		} else {
			nextPageToken = base64.StdEncoding.EncodeToString([]byte(module.Name))
		}

		rowsCount++
	}

	return modules, nextPageToken, nil
}

func (r *registryRepo) DeleteModule(ctx context.Context, name string) error {
	// delete all protofiles
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM protofiles WHERE tag_id IN (SELECT id FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?))",
		name)
	if err != nil {
		return fmt.Errorf("could not delete protofiles from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d protofiles for module %s", n, name)
	}

	// delete all module dependencies
	res, err = r.db.ExecContext(ctx,
		"DELETE FROM dependencies WHERE tag_id IN (SELECT id FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?))",
		name)
	if err != nil {
		return fmt.Errorf("could not delete module dependencies from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d dependencies for module %s", n, name)
	}

	// delete all module tags
	res, err = r.db.ExecContext(ctx,
		"DELETE FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?)",
		name)
	if err != nil {
		return fmt.Errorf("could not delete module tags from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d tags for module %s", n, name)
	}

	// delete all module draft tags
	res, err = r.db.ExecContext(ctx,
		"DELETE FROM draft_tags WHERE module_id = (SELECT id FROM modules WHERE name = ?)",
		name)
	if err != nil {
		return fmt.Errorf("could not delete module draft tags from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d draft tags for module %s", n, name)
	}

	// delete module
	res, err = r.db.ExecContext(ctx,
		"DELETE FROM modules WHERE name = ?",
		name)
	if err != nil {
		return fmt.Errorf("could not delete module from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted module %s", name)
	} else {
		return errors.New("module not found")
	}

	return nil
}

func (r *registryRepo) PushModule(ctx context.Context, name string, tag string, protofiles []*v1.ProtoFile) (*v1.Module, error) {
	module, err := r.GetModule(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("could not get module: %w", err)
	}
	if module == nil {
		return nil, errors.New("module not found")
	}

	for _, t := range module.Tags {
		if t == tag {
			return nil, errors.New("tag already exists")
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("could not begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			r.logger.Errorf("could not rollback transaction: %v", err)
		}
	}()

	tagID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO tags (id, module_id, tag) VALUES (?, ?, ?)",
		tagID, module.Id, tag)
	if err != nil {
		return nil, fmt.Errorf("could not insert tag into database: %w", err)
	}

	for _, protofile := range protofiles {
		pfID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO protofiles (id, tag_id, filename, content) VALUES (?, ?, ?, ?)",
			pfID, tagID, protofile.Filename, protofile.Content)
		if err != nil {
			return nil, fmt.Errorf("could not insert protofile into database: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		r.logger.Errorf("could not commit transaction: %v", err)
		return nil, fmt.Errorf("could not push the module. internal error")
	}

	module.Tags = append(module.Tags, tag)
	return module, nil
}

func (r *registryRepo) PullModule(ctx context.Context, name string, tag string) (*v1.Module, []*v1.ProtoFile, error) {
	module, err := r.GetModule(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get module: %w", err)
	}
	if module == nil {
		return nil, nil, errors.New("module not found")
	}

	tagId, err := r.GetModuleTagId(ctx, name, tag)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get tag id: %w", err)
	}
	if tagId == "" {
		return nil, nil, data.ErrTagNotFound
	}

	protofilesRows, err := r.db.QueryContext(ctx,
		"SELECT filename, content FROM protofiles WHERE tag_id = ?",
		tagId)
	if err != nil {
		return nil, nil, fmt.Errorf("could not select protofiles from database: %w", err)
	}
	defer protofilesRows.Close()

	var protofiles []*v1.ProtoFile
	for protofilesRows.Next() {
		protofile := &v1.ProtoFile{}
		if err := protofilesRows.Scan(&protofile.Filename, &protofile.Content); err != nil {
			return nil, nil, fmt.Errorf("could not scan protofile: %w", err)
		}
		protofiles = append(protofiles, protofile)
	}

	return module, protofiles, nil
}

func (r *registryRepo) PullDraftModule(ctx context.Context, name string, tag string) (*v1.Module, []*v1.ProtoFile, error) {
	module, err := r.GetModule(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get module: %w", err)
	}
	if module == nil {
		return nil, nil, errors.New("module not found")
	}

	var protofilesJson string
	err = r.db.QueryRowContext(ctx,
		"SELECT proto_files FROM draft_tags WHERE module_id = ? AND tag = ?",
		module.Id, tag).Scan(&protofilesJson)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errors.New("draft tag not found")
		}
		return nil, nil, fmt.Errorf("could not select draft tag from database: %w", err)
	}

	var protofiles []*v1.ProtoFile
	if err := json.Unmarshal([]byte(protofilesJson), &protofiles); err != nil {
		return nil, nil, fmt.Errorf("could not unmarshal protofiles: %w", err)
	}

	return module, protofiles, nil
}

func (r *registryRepo) DeleteModuleTag(ctx context.Context, name string, tag string) error {
	tagId, err := r.GetModuleTagId(ctx, name, tag)
	if err != nil {
		return fmt.Errorf("could not get tag id: %w", err)
	}
	if tagId == "" {
		return data.ErrTagNotFound
	}

	res, err := r.db.ExecContext(ctx,
		"DELETE FROM protofiles WHERE tag_id = ?",
		tagId)
	if err != nil {
		return fmt.Errorf("could not delete protofiles from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d protofiles for tag %s", n, tag)
	}

	res, err = r.db.ExecContext(ctx,
		"DELETE FROM dependencies WHERE tag_id = ? OR dependency_tag_id = ?",
		tagId, tagId)
	if err != nil {
		return fmt.Errorf("could not delete dependencies from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d dependencies for tag %s", n, tag)
	}

	res, err = r.db.ExecContext(ctx,
		"DELETE FROM tags WHERE id = ?",
		tagId)
	if err != nil {
		return fmt.Errorf("could not delete tag from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted tag %s", tag)
	} else {
		return data.ErrTagNotFound
	}

	return nil
}

func (r *registryRepo) AddModuleDependencies(ctx context.Context, name string, tag string, dependencies []*v1.Dependency) error {
	tagId, err := r.GetModuleTagId(ctx, name, tag)
	if err != nil {
		return fmt.Errorf("could not get tag id: %w", err)
	}
	if tagId == "" {
		return data.ErrTagNotFound
	}

	for _, dependency := range dependencies {
		var dependencyTagId string
		err := r.db.QueryRowContext(ctx,
			"SELECT id FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?) AND tag = ?",
			dependency.Name, dependency.Tag).Scan(&dependencyTagId)
		if err != nil {
			return fmt.Errorf("could not find tag %s for module %s: %w", dependency.Tag, dependency.Name, err)
		}

		depID := uuid.New().String()
		_, err = r.db.ExecContext(ctx,
			"INSERT INTO dependencies (id, tag_id, dependency_tag_id) VALUES (?, ?, ?)",
			depID, tagId, dependencyTagId)
		if err != nil {
			return fmt.Errorf("could not insert dependency into database: %w", err)
		}
	}

	return nil
}

func (r *registryRepo) GetModuleDependencies(ctx context.Context, name string, tag string) ([]*v1.Dependency, error) {
	var dependencies []*v1.Dependency

	if tag == "" {
		err := r.db.QueryRowContext(ctx,
			"SELECT tag FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?) ORDER BY updated_at DESC LIMIT 1",
			name).Scan(&tag)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return dependencies, nil
			}
			return nil, fmt.Errorf("could not select tag from database: %w", err)
		}
	}

	tagId, err := r.GetModuleTagId(ctx, name, tag)
	if err != nil {
		return nil, fmt.Errorf("could not get tag id: %w", err)
	}
	if tagId == "" {
		return nil, data.ErrTagNotFound
	}

	rows, err := r.db.QueryContext(ctx,
		"SELECT dependency_tag_id FROM dependencies WHERE tag_id = ?",
		tagId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []*v1.Dependency{}, nil
		}
		return nil, fmt.Errorf("could not select dependencies from database: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dependencyTagId string
		if err := rows.Scan(&dependencyTagId); err != nil {
			return nil, fmt.Errorf("could not scan dependency: %w", err)
		}

		var dependencyName, dependencyTag string
		err = r.db.QueryRowContext(ctx,
			"SELECT modules.name, tags.tag FROM modules JOIN tags ON modules.id = tags.module_id WHERE tags.id = ?",
			dependencyTagId).Scan(&dependencyName, &dependencyTag)
		if err != nil {
			return nil, fmt.Errorf("could not find module and tag for dependency: %w", err)
		}

		dependencies = append(dependencies, &v1.Dependency{
			Name: dependencyName,
			Tag:  dependencyTag,
		})
	}

	return dependencies, nil
}

func (r *registryRepo) PushDraftModule(ctx context.Context, name string, tag string, protofiles []*v1.ProtoFile, dependencies []*v1.Dependency) (*v1.Module, error) {
	module, err := r.GetModule(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("could not get module: %w", err)
	}
	if module == nil {
		return nil, errors.New("module not found")
	}

	protofilesJson, err := json.Marshal(protofiles)
	if err != nil {
		return nil, fmt.Errorf("could not serialize protofiles: %w", err)
	}

	dependenciesJson, err := json.Marshal(dependencies)
	if err != nil {
		return nil, fmt.Errorf("could not serialize dependencies: %w", err)
	}

	id := uuid.New().String()
	_, err = r.db.ExecContext(ctx,
		"INSERT INTO draft_tags (id, module_id, tag, proto_files, dependencies) VALUES (?, ?, ?, ?, ?) ON CONFLICT (module_id, tag) DO UPDATE SET proto_files = excluded.proto_files, dependencies = excluded.dependencies, updated_at = datetime('now')",
		id, module.Id, tag, string(protofilesJson), string(dependenciesJson))
	if err != nil {
		return nil, fmt.Errorf("could not insert draft tag into database: %w", err)
	}

	var draftTagExists bool
	for _, t := range module.DraftTags {
		if t == tag {
			draftTagExists = true
			break
		}
	}
	if !draftTagExists {
		module.DraftTags = append(module.DraftTags, tag)
	}

	return module, nil
}

func (r *registryRepo) GetModuleTagId(ctx context.Context, moduleName string, tag string) (string, error) {
	var tagId string
	err := r.db.QueryRowContext(ctx,
		"SELECT id FROM tags WHERE module_id = (SELECT id FROM modules WHERE name = ?) AND tag = ?",
		moduleName, tag).Scan(&tagId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("could not select tag from database: %w", err)
	}
	return tagId, nil
}

// depKey is a struct used as a map key to track visited dependencies.
type depKey struct {
	name string
	tag  string
}

func (r *registryRepo) GetTransitiveDependencies(ctx context.Context, name string, tag string) ([]*v1.Dependency, error) {
	directDeps, err := r.GetModuleDependencies(ctx, name, tag)
	if err != nil {
		return nil, fmt.Errorf("could not get direct dependencies: %w", err)
	}

	result := make([]*v1.Dependency, 0, len(directDeps))
	for _, dep := range directDeps {
		result = append(result, &v1.Dependency{
			Name:           dep.Name,
			Tag:            dep.Tag,
			DependencyType: "direct",
		})
	}

	visited := make(map[depKey]bool)
	visited[depKey{name: name, tag: tag}] = true
	for _, dep := range directDeps {
		visited[depKey{name: dep.Name, tag: dep.Tag}] = true
	}

	queue := make([]*v1.Dependency, len(directDeps))
	copy(queue, directDeps)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		transitiveDeps, err := r.GetModuleDependencies(ctx, current.Name, current.Tag)
		if err != nil {
			r.logger.Warnf("could not get dependencies for %s:%s: %v", current.Name, current.Tag, err)
			continue
		}

		for _, dep := range transitiveDeps {
			key := depKey{name: dep.Name, tag: dep.Tag}
			if !visited[key] {
				visited[key] = true
				result = append(result, &v1.Dependency{
					Name:           dep.Name,
					Tag:            dep.Tag,
					DependencyType: "transitive",
				})
				queue = append(queue, dep)
			}
		}
	}

	return result, nil
}

// DeleteObsoleteDraftTags deletes all draft tags older than 7 days.
func (r *registryRepo) DeleteObsoleteDraftTags(ctx context.Context) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM draft_tags WHERE updated_at < datetime('now', '-7 days')")
	if err != nil {
		return fmt.Errorf("could not delete draft tags from database: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.logger.Infof("deleted %d draft tags", n)
	}
	return nil
}
