package store

import (
	"context"
	"database/sql"
	"errors"
)

type UpstreamCredential struct {
	ID, Name, Host string
	Encrypted      []byte
}

func (s *Store) migrateUpstreamCredentials(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 23 {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS upstream_credential (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, encrypted BLOB NOT NULL
		);
		PRAGMA user_version=23;`)
		return err
	})
}

func (s *Store) ListUpstreamCredentials(ctx context.Context) ([]UpstreamCredential, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,host FROM upstream_credential ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []UpstreamCredential{}
	for rows.Next() {
		var entry UpstreamCredential
		if err := rows.Scan(&entry.ID, &entry.Name, &entry.Host); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *Store) UpstreamCredential(ctx context.Context, id string) (UpstreamCredential, error) {
	entry := UpstreamCredential{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT name,host,encrypted FROM upstream_credential WHERE id=?", id).
		Scan(&entry.Name, &entry.Host, &entry.Encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return UpstreamCredential{}, ErrNotFound
	}
	return entry, err
}

func (s *Store) AddUpstreamCredential(ctx context.Context, name, host string, encrypted []byte, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM upstream_credential").Scan(&count); err != nil {
			return err
		}
		if count >= 256 {
			return ErrConflict
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO upstream_credential(id,name,host,encrypted) VALUES(?,?,?,?)", id, name, host, encrypted)
		return err
	})
}

func (s *Store) RemoveUpstreamCredential(ctx context.Context, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var inUse bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM
			json_each((SELECT content FROM model_config_version ORDER BY version DESC LIMIT 1), '$.Connections')
			WHERE json_extract(value,'$.CredentialID')=?)`, id).Scan(&inUse)
		if err != nil {
			return err
		}
		if inUse {
			return ErrConflict
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM upstream_credential WHERE id=?", id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n == 0 {
			return ErrNotFound
		}
		return err
	})
}
