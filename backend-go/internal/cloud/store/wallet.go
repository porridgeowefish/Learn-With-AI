package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Account struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	Balance      int64  `json:"balance"`
	GitHubLinked bool   `json:"githubLinked,omitempty"`
}

// AccountForEmail is for verified addresses only.
func (s *Store) AccountForEmail(ctx context.Context, email string, now time.Time) (Account, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		account, err = accountForEmail(ctx, tx, email, now)
		return err
	})
	return account, err
}

func accountForEmail(ctx context.Context, tx *sql.Tx, email string, now time.Time) (Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return Account{}, errors.New("verified email is required")
	}
	account := Account{ID: randomID(), Email: email, Balance: WelcomeCredits}
	result, err := tx.ExecContext(ctx, "INSERT INTO account(id,email,created_at,balance) VALUES(?,?,?,?) ON CONFLICT(email) DO NOTHING",
		account.ID, account.Email, now.Unix(), account.Balance)
	if err != nil {
		return Account{}, err
	}
	created, err := result.RowsAffected()
	if err != nil {
		return Account{}, err
	}
	if created == 0 {
		err = tx.QueryRowContext(ctx, `SELECT a.id,a.balance,COALESCE(a.username,''),
			EXISTS(SELECT 1 FROM github_identity g WHERE g.account_id=a.id) FROM account a WHERE a.email=?`, email).
			Scan(&account.ID, &account.Balance, &account.Name, &account.GitHubLinked)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO credit_movement VALUES(?,?,?,?,?)", account.ID, "welcome", "registration", WelcomeCredits, now.Unix())
	}
	return account, err
}

type Wallet struct {
	Balance int64 `json:"balance"`
	Held    int64 `json:"held"`
}

type Claim struct {
	Wallet
	Granted bool `json:"granted"`
}

type DailyClaimEntry struct {
	Day   string `json:"day"`
	Units int64  `json:"units"`
}

type DailyClaimHistory struct {
	Today   string            `json:"today"`
	Entries []DailyClaimEntry `json:"entries"`
}

func (s *Store) DailyClaimHistory(ctx context.Context, accountID string, now time.Time) (DailyClaimHistory, error) {
	day := now.In(shanghai)
	history := DailyClaimHistory{Today: day.Format(time.DateOnly), Entries: []DailyClaimEntry{}}
	rows, err := s.db.QueryContext(ctx, `SELECT reference,amount FROM credit_movement
		WHERE account_id=? AND kind='daily' AND reference BETWEEN ? AND ? ORDER BY reference DESC`,
		accountID, day.AddDate(0, 0, -29).Format(time.DateOnly), history.Today)
	if err != nil {
		return DailyClaimHistory{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry DailyClaimEntry
		if err := rows.Scan(&entry.Day, &entry.Units); err != nil {
			return DailyClaimHistory{}, err
		}
		history.Entries = append(history.Entries, entry)
	}
	return history, rows.Err()
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func wallet(ctx context.Context, db queryer, accountID string) (Wallet, error) {
	var w Wallet
	err := db.QueryRowContext(ctx, `SELECT balance, COALESCE((SELECT SUM(held) FROM inference_request
		WHERE account_id = account.id AND state IN ('reserved','pending')),0) FROM account WHERE id = ?`, accountID).
		Scan(&w.Balance, &w.Held)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return w, err
}

func (s *Store) Wallet(ctx context.Context, accountID string) (Wallet, error) {
	return wallet(ctx, s.db, accountID)
}

func (s *Store) SettledCredits(ctx context.Context, accountID string, requestIDs []string) (map[string]int64, error) {
	encoded, err := json.Marshal(requestIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_id,charged FROM inference_request
		WHERE account_id=? AND state='settled' AND request_id IN (SELECT value FROM json_each(?))`, accountID, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settled := make(map[string]int64)
	for rows.Next() {
		var request string
		var units int64
		if err := rows.Scan(&request, &units); err != nil {
			return nil, err
		}
		settled[request] = units
	}
	return settled, rows.Err()
}

func (s *Store) DailyClaimed(ctx context.Context, accountID string, now time.Time) (bool, error) {
	var claimed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credit_movement WHERE account_id=?
		AND kind='daily' AND reference=?)`, accountID, now.In(shanghai).Format(time.DateOnly)).Scan(&claimed)
	return claimed, err
}

func (s *Store) ClaimDaily(ctx context.Context, accountID string, now time.Time) (Claim, error) {
	var claim Claim
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		claim.Wallet, err = wallet(ctx, tx, accountID)
		if err != nil {
			return err
		}
		day := now.In(shanghai).Format(time.DateOnly)
		result, err := tx.ExecContext(ctx, "INSERT INTO credit_movement VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING",
			accountID, "daily", day, DailyCredits, now.Unix())
		if err != nil {
			return err
		}
		added, err := result.RowsAffected()
		if err != nil || added == 0 {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE account SET balance = balance + ? WHERE id = ?", DailyCredits, accountID)
		if err == nil {
			claim.Balance += DailyCredits
			claim.Granted = true
		}
		return err
	})
	return claim, err
}

type Reservation struct {
	Created bool
	State   string
}

func (s *Store) Reserve(ctx context.Context, accountID, requestID, digest string, units int64, now time.Time) (Reservation, error) {
	return s.ReserveBudgeted(ctx, accountID, requestID, digest, units, now, BudgetHold{})
}

func (s *Store) ReserveBudgeted(ctx context.Context, accountID, requestID, digest string, units int64, now time.Time, budget BudgetHold) (Reservation, error) {
	if requestID == "" || digest == "" || units <= 0 {
		return Reservation{}, errors.New("invalid inference reservation")
	}
	var hold Reservation
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, "SELECT digest,state FROM inference_request WHERE account_id = ? AND request_id = ?",
			accountID, requestID).Scan(&existing, &hold.State)
		if err == nil {
			if existing != digest {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		w, err := wallet(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if units > w.Balance-w.Held {
			return ErrBalance
		}
		if err := checkDailyCreditLimit(ctx, tx, accountID, units, now); err != nil {
			return err
		}
		hold = Reservation{Created: true, State: "reserved"}
		_, err = tx.ExecContext(ctx, `INSERT INTO inference_request(account_id,request_id,digest,held,state,created_at,updated_at)
			VALUES(?,?,?,?,'reserved',?,?)`, accountID, requestID, digest, units, now.Unix(), now.Unix())
		if err != nil || budget.Limit == 0 {
			return err
		}
		return reserveCost(ctx, tx, accountID, requestID, now, budget)
	})
	return hold, err
}

// MarkPending retains the hold when upstream usage cannot be established.
func (s *Store) MarkPending(ctx context.Context, accountID, requestID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE inference_request SET state = 'pending', updated_at = ? WHERE account_id = ? AND request_id = ? AND state = 'reserved'",
		now.Unix(), accountID, requestID)
	return err
}

// Settle deducts confirmed usage. A zero charge releases an unused hold.
func (s *Store) Settle(ctx context.Context, accountID, requestID string, units int64, now time.Time) error {
	return s.SettleBudgeted(ctx, accountID, requestID, units, now, nil)
}

func (s *Store) SettleBudgeted(ctx context.Context, accountID, requestID string, units int64, now time.Time, cost *int64) error {
	if units < 0 || (cost != nil && *cost < 0) {
		return errors.New("invalid credit charge")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		return settleInference(ctx, tx, accountID, requestID, units, now, cost)
	})
}

func settleInference(ctx context.Context, tx *sql.Tx, accountID, requestID string, units int64, now time.Time, cost *int64) error {
	var state string
	var held, charged int64
	err := tx.QueryRowContext(ctx, "SELECT state,held,charged FROM inference_request WHERE account_id = ? AND request_id = ?",
		accountID, requestID).Scan(&state, &held, &charged)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state == "settled" {
		if charged != units {
			return ErrConflict
		}
		return settleCost(ctx, tx, accountID, requestID, cost)
	}
	w, err := wallet(ctx, tx, accountID)
	if err != nil {
		return err
	}
	if units > w.Balance-w.Held+held {
		return ErrBalance
	}
	if _, err = tx.ExecContext(ctx, "UPDATE account SET balance = balance - ? WHERE id = ?", units, accountID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO credit_movement VALUES(?,?,?,?,?)", accountID, "inference", requestID, -units, now.Unix()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE inference_request SET held = 0, charged = ?, state = 'settled', updated_at = ? WHERE account_id = ? AND request_id = ?",
		units, now.Unix(), accountID, requestID)
	if err != nil {
		return err
	}
	return settleCost(ctx, tx, accountID, requestID, cost)
}

func settleCost(ctx context.Context, tx *sql.Tx, accountID, requestID string, cost *int64) error {
	if cost == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, "UPDATE cost_budget SET settled = ? WHERE account_id = ? AND request_id = ? AND settled IS NULL", *cost, accountID, requestID)
	return err
}
