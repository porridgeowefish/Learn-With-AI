// Package store owns durable cloud account, session, and credit transactions.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	UnitsPerCredit int64 = 1000
	WelcomeCredits       = 500 * UnitsPerCredit
	DailyCredits         = 100 * UnitsPerCredit
)

var (
	ErrConflict = errors.New("request already exists with different data")
	ErrBalance  = errors.New("insufficient available credits")
	ErrNotFound = errors.New("record not found")
	shanghai    = time.FixedZone("Asia/Shanghai", 8*60*60)
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("cloud database path is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	fileURI := filepath.ToSlash(path)
	if !filepath.IsAbs(fileURI) || fileURI[0] != '/' {
		fileURI = "/" + fileURI
	}
	dsn := url.URL{Scheme: "file", Path: fileURI}
	dsn.RawQuery = url.Values{"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)"}}.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	// One service replica owns the inference pool. Serialize local writes so
	// competing HTTP requests cannot interleave a balance read and a reservation.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err := s.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) initialize(ctx context.Context) error {
	if err := s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version > 18 {
			return fmt.Errorf("unsupported cloud schema version %d", version)
		}
		if version == 0 {
			if _, err := tx.ExecContext(ctx, schema); err != nil {
				return err
			}
		}
		if version < 2 {
			if _, err := tx.ExecContext(ctx, authSchema); err != nil {
				return err
			}
		}
		if version < 3 {
			if _, err := tx.ExecContext(ctx, githubSchema); err != nil {
				return err
			}
		}
		if version < 4 {
			if _, err := tx.ExecContext(ctx, metricsSchema); err != nil {
				return err
			}
		}
		if version < 5 {
			if _, err := tx.ExecContext(ctx, adminSchema); err != nil {
				return err
			}
		}
		if version < 6 {
			if _, err := tx.ExecContext(ctx, experimentSchema); err != nil {
				return err
			}
		}
		if version < 7 {
			if _, err := tx.ExecContext(ctx, modelConfigSchema); err != nil {
				return err
			}
		}
		if version < 8 {
			if _, err := tx.ExecContext(ctx, budgetSchema); err != nil {
				return err
			}
		}
		if version < 9 {
			if _, err := tx.ExecContext(ctx, promptConfigSchema); err != nil {
				return err
			}
		}
		if version < 10 {
			_, err := tx.ExecContext(ctx, reconcileSchema)
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if err := s.migratePasswordAccounts(ctx); err != nil {
		return err
	}
	if err := s.migrateGitHubFlows(ctx); err != nil {
		return err
	}
	if err := s.migrateExperimentRequests(ctx); err != nil {
		return err
	}
	if err := s.migrateInferenceRoutes(ctx); err != nil {
		return err
	}
	if err := s.migrateAccountLimits(ctx); err != nil {
		return err
	}
	if err := s.migrateLoginMetrics(ctx); err != nil {
		return err
	}
	if err := s.migrateTeachingRatings(ctx); err != nil {
		return err
	}
	return s.migrateGoogleIdentity(ctx)
}

func (s *Store) transaction(ctx context.Context, run func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func randomID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

const schema = `
CREATE TABLE account (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  username TEXT UNIQUE,
  password_hash TEXT,
  created_at INTEGER NOT NULL,
  balance INTEGER NOT NULL CHECK(balance >= 0)
);
CREATE TABLE credit_movement (
  account_id TEXT NOT NULL REFERENCES account(id),
  kind TEXT NOT NULL,
  reference TEXT NOT NULL,
  amount INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(account_id, kind, reference)
);
CREATE TABLE inference_request (
  account_id TEXT NOT NULL REFERENCES account(id),
  request_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  held INTEGER NOT NULL CHECK(held >= 0),
  charged INTEGER NOT NULL DEFAULT 0 CHECK(charged >= 0),
  state TEXT NOT NULL CHECK(state IN ('reserved', 'pending', 'settled')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(account_id, request_id)
);
CREATE INDEX inference_active_holds ON inference_request(account_id) WHERE state IN ('reserved', 'pending');
PRAGMA user_version = 1;
`
