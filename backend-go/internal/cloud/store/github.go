package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrWaiting       = errors.New("authorization is pending")
	ErrGitHub        = errors.New("github authorization failed")
	ErrGitHubSession = errors.New("github binding requires the original session")
)

const githubSchema = `
CREATE TABLE github_identity (
  provider_id TEXT PRIMARY KEY,
  account_id TEXT UNIQUE NOT NULL REFERENCES account(id)
);
CREATE TABLE github_flow (
  id TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  verifier TEXT NOT NULL,
  state_digest BLOB UNIQUE NOT NULL,
  desktop_challenge BLOB NOT NULL,
  expires_at INTEGER NOT NULL,
  state TEXT NOT NULL DEFAULT 'waiting',
  account_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX github_flow_expiry ON github_flow(expires_at);
PRAGMA user_version = 3;
`

type GitHubFlow struct {
	ID, Owner, Verifier           string
	StateDigest, DesktopChallenge []byte
	OwnerSession                  []byte
	Expires                       time.Time
}

func (s *Store) migrateGitHubFlows(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 12 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
ALTER TABLE github_flow ADD COLUMN owner_session BLOB;
ALTER TABLE github_flow ADD COLUMN link_provider_id TEXT NOT NULL DEFAULT '';
DELETE FROM github_flow WHERE owner <> '';
PRAGMA user_version = 12;`)
		return err
	})
}

func (s *Store) StartGitHub(ctx context.Context, f GitHubFlow) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM github_flow WHERE expires_at < ?", f.Expires.Add(-10*time.Minute).Unix()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO github_flow(id,owner,verifier,state_digest,desktop_challenge,expires_at,owner_session)
			VALUES(?,?,?,?,?,?,?)`, f.ID, f.Owner, f.Verifier, f.StateDigest, f.DesktopChallenge, f.Expires.Unix(), f.OwnerSession)
		return err
	})
}

func (s *Store) ClaimGitHubCallback(ctx context.Context, digest []byte, now time.Time) (GitHubFlow, error) {
	var flow GitHubFlow
	err := s.db.QueryRowContext(ctx, `UPDATE github_flow SET state = 'exchanging' WHERE state_digest = ?
		AND state = 'waiting' AND expires_at > ? RETURNING id,owner,verifier`, digest, now.Unix()).
		Scan(&flow.ID, &flow.Owner, &flow.Verifier)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return flow, err
}

func (s *Store) CompleteGitHub(ctx context.Context, flowID, githubID, email string, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var owner string
		var ownerSession []byte
		err := tx.QueryRowContext(ctx, "SELECT owner,owner_session FROM github_flow WHERE id = ? AND state = 'exchanging' AND expires_at > ?",
			flowID, now.Unix()).Scan(&owner, &ownerSession)
		if err != nil {
			return err
		}
		if owner != "" {
			// Wait for the desktop that started the binding to complete it.
			account, err := sessionAccount(ctx, tx, ownerSession, now)
			if errors.Is(err, ErrNotFound) || (err == nil && account.ID != owner) {
				return ErrGitHubSession
			}
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "UPDATE github_flow SET state = 'ready', link_provider_id = ?, verifier = '' WHERE id = ?", githubID, flowID)
			return err
		}
		var accountID string
		err = tx.QueryRowContext(ctx, "SELECT account_id FROM github_identity WHERE provider_id = ?", githubID).Scan(&accountID)
		switch {
		case err == nil:
		case !errors.Is(err, sql.ErrNoRows):
			return err
		default:
			if email == "" {
				return ErrCode
			}
			var exists bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM account WHERE email = ?)", email).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrConflict
			}
			account, err := accountForEmail(ctx, tx, email, now)
			if err != nil {
				return err
			}
			accountID = account.ID
			if err := linkGitHubIdentity(ctx, tx, githubID, accountID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE github_flow SET state = 'ready', account_id = ?, verifier = '' WHERE id = ?", accountID, flowID)
		return err
	})
}

func linkGitHubIdentity(ctx context.Context, tx *sql.Tx, providerID, accountID string) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO github_identity VALUES(?,?) ON CONFLICT DO NOTHING", providerID, accountID); err != nil {
		return err
	}
	var linked bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM github_identity WHERE provider_id=? AND account_id=?)", providerID, accountID).Scan(&linked); err != nil {
		return err
	}
	if !linked {
		return ErrConflict
	}
	return nil
}

func (s *Store) FailGitHub(ctx context.Context, flowID, reason string) error {
	if reason != "email_conflict" && reason != "email_required" {
		reason = "failed"
	}
	_, err := s.db.ExecContext(ctx, "UPDATE github_flow SET state = ?, verifier = '' WHERE id = ? AND state = 'exchanging'", reason, flowID)
	return err
}

func (s *Store) RedeemGitHub(ctx context.Context, flowID string, proof, ownerSession, sessionDigest []byte, now, expires time.Time) (Account, time.Time, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var state, accountID, owner, linkProvider string
		var originalSession []byte
		err := tx.QueryRowContext(ctx, `SELECT state,account_id,owner,owner_session,link_provider_id
			FROM github_flow WHERE id = ? AND desktop_challenge = ? AND expires_at > ?`,
			flowID, proof, now.Unix()).Scan(&state, &accountID, &owner, &originalSession, &linkProvider)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if owner != "" {
			if len(originalSession) == 0 || subtle.ConstantTimeCompare(originalSession, ownerSession) != 1 {
				return ErrGitHubSession
			}
			account, err = sessionAccount(ctx, tx, ownerSession, now)
			if errors.Is(err, ErrNotFound) || (err == nil && account.ID != owner) {
				return ErrGitHubSession
			}
			if err != nil {
				return err
			}
		} else if len(ownerSession) > 0 {
			return ErrGitHubSession
		}
		switch state {
		case "waiting", "exchanging":
			return ErrWaiting
		case "email_conflict":
			return ErrConflict
		case "email_required":
			return ErrCode
		case "failed":
			return ErrGitHub
		case "ready":
		default:
			return ErrNotFound
		}
		if owner != "" {
			if linkProvider == "" {
				return ErrGitHubSession
			}
			if err := linkGitHubIdentity(ctx, tx, linkProvider, account.ID); err != nil {
				return err
			}
			var originalExpiry int64
			if err := tx.QueryRowContext(ctx, "SELECT expires_at FROM account_session WHERE digest = ?", ownerSession).Scan(&originalExpiry); err != nil {
				return err
			}
			expires = time.Unix(originalExpiry, 0)
		} else {
			if err := tx.QueryRowContext(ctx, "SELECT id,COALESCE(email,''),balance,COALESCE(username,'') FROM account WHERE id = ?", accountID).
				Scan(&account.ID, &account.Email, &account.Balance, &account.Name); err != nil {
				return err
			}
			if err := issueSignedInSession(ctx, tx, account.ID, sessionDigest, now, expires); err != nil {
				return err
			}
		}
		account.GitHubLinked = true
		_, err = tx.ExecContext(ctx, "DELETE FROM github_flow WHERE id = ?", flowID)
		return err
	})
	return account, expires, err
}
