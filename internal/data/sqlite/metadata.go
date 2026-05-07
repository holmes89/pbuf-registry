package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	v1 "github.com/pbufio/pbuf-registry/gen/pbuf-registry/v1"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/model"
	"github.com/pbufio/pbuf-registry/internal/utils"
	"github.com/yoheimuta/go-protoparser/v4/interpret/unordered"
)

type metadataRepo struct {
	repo
}

// NewMetadataRepository creates a new SQLite-backed MetadataRepository.
func NewMetadataRepository(db *sql.DB, logger log.Logger) data.MetadataRepository {
	return &metadataRepo{
		repo: repo{
			db:     db,
			logger: log.NewHelper(log.With(logger, "module", "data/sqlite/MetadataRepository")),
		},
	}
}

func (m *metadataRepo) GetUnprocessedTagIds(ctx context.Context) ([]string, error) {
	var tagIds []string

	rows, err := m.db.QueryContext(ctx, "SELECT id FROM tags WHERE is_processed = 0")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tagIds, nil
		}
		m.logger.Errorf("error getting unprocessed tag ids: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tagId string
		if err = rows.Scan(&tagId); err != nil {
			m.logger.Errorf("error scanning tag id: %v", err)
			return nil, err
		}
		tagIds = append(tagIds, tagId)
	}

	return tagIds, nil
}

func (m *metadataRepo) GetProtoFilesForTagId(ctx context.Context, tagId string) ([]*v1.ProtoFile, error) {
	var protoFiles []*v1.ProtoFile

	rows, err := m.db.QueryContext(ctx, "SELECT filename, content FROM protofiles WHERE tag_id = ?", tagId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protoFiles, nil
		}
		m.logger.Errorf("error getting proto files for tag id %s: %v", tagId, err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var filename, content string
		if err = rows.Scan(&filename, &content); err != nil {
			m.logger.Errorf("error scanning proto file: %v", err)
			return nil, err
		}
		protoFiles = append(protoFiles, &v1.ProtoFile{
			Filename: filename,
			Content:  content,
		})
	}

	return protoFiles, nil
}

func (m *metadataRepo) SaveParsedProtoFiles(ctx context.Context, tagId string, files []*model.ParsedProtoFile) error {
	meta, err := utils.RetrieveMeta(files)
	if err != nil {
		m.logger.Errorf("error retrieving tag meta: %v", err)
		return err
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		m.logger.Errorf("error starting transaction: %v", err)
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			m.logger.Errorf("error rolling back transaction: %v", err)
		}
	}()

	for _, file := range files {
		pfID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO proto_parsed_data (id, tag_id, filename, json) VALUES (?, ?, ?, ?) ON CONFLICT (tag_id, filename) DO UPDATE SET json = excluded.json",
			pfID, tagId, file.Filename, file.ProtoJson)
		if err != nil {
			m.logger.Errorf("error inserting parsed proto file: %v", err)
			return err
		}
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		m.logger.Errorf("error marshaling tag meta: %v", err)
		return err
	}

	tmID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO tag_meta (id, tag_id, meta) VALUES (?, ?, ?) ON CONFLICT (tag_id) DO UPDATE SET meta = excluded.meta",
		tmID, tagId, string(metaJSON))
	if err != nil {
		m.logger.Errorf("error inserting tag meta: %v", err)
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE tags SET is_processed = 1 WHERE id = ?", tagId)
	if err != nil {
		m.logger.Errorf("error updating tag to processed: %v", err)
		return err
	}

	if err = tx.Commit(); err != nil {
		m.logger.Errorf("error committing transaction: %v", err)
		return err
	}

	return nil
}

func (m *metadataRepo) GetTagMetaByTagId(ctx context.Context, tagId string) (*model.TagMeta, error) {
	var metaStr string
	err := m.db.QueryRowContext(ctx, "SELECT meta FROM tag_meta WHERE tag_id = ?", tagId).Scan(&metaStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		m.logger.Errorf("error getting tag meta for tag id %s: %v", tagId, err)
		return nil, err
	}

	var meta model.TagMeta
	if err := json.Unmarshal([]byte(metaStr), &meta); err != nil {
		m.logger.Errorf("error unmarshaling tag meta for tag id %s: %v", tagId, err)
		return nil, err
	}

	return &meta, nil
}

func (m *metadataRepo) GetParsedProtoFiles(ctx context.Context, tagId string) ([]*model.ParsedProtoFile, error) {
	var parsedProtoFiles []*model.ParsedProtoFile

	rows, err := m.db.QueryContext(ctx, "SELECT filename, json FROM proto_parsed_data WHERE tag_id = ?", tagId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return parsedProtoFiles, nil
		}
		m.logger.Errorf("error getting parsed proto files for tag id %s: %v", tagId, err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var filename, protoJson string
		var proto *unordered.Proto
		if err = rows.Scan(&filename, &protoJson); err != nil {
			m.logger.Errorf("error scanning parsed proto file: %v", err)
			return nil, err
		}

		if err = json.Unmarshal([]byte(protoJson), &proto); err != nil {
			m.logger.Errorf("error unmarshaling json: %v", err)
			return nil, err
		}

		parsedProtoFiles = append(parsedProtoFiles, &model.ParsedProtoFile{
			Filename: filename,
			Proto:    proto,
		})
	}

	return parsedProtoFiles, nil
}
