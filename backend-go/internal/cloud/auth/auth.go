// Package auth implements verified email and password sign-in for LLL accounts.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

const (
	PurposeLogin   = "login"
	PurposeReset   = "reset"
	sessionTTL     = 30 * 24 * time.Hour
	passwordRounds = 600_000
	passwordMin    = 6
	passwordMax    = 256
)

var (
	ErrInvalid     = errors.New("invalid account input")
	ErrCredentials = errors.New("invalid username or password")
	ErrDelivery    = errors.New("email delivery failed")
	usernameFormat = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)
)

type SendCodeFunc func(context.Context, string, string) error

type Service struct {
	store *store.Store
	key   []byte
	send  SendCodeFunc
	now   func() time.Time
}

func New(db *store.Store, codeKey []byte, send SendCodeFunc) (*Service, error) {
	if db == nil || len(codeKey) < 32 || send == nil {
		return nil, ErrInvalid
	}
	return &Service{store: db, key: append([]byte(nil), codeKey...), send: send, now: time.Now}, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 {
		return "", ErrInvalid
	}
	return value, nil
}

func validPurpose(purpose string) bool {
	return purpose == PurposeLogin || purpose == PurposeReset
}

func (s *Service) codeDigest(email, purpose, code string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(email + "\x00" + purpose + "\x00" + code))
	return mac.Sum(nil)
}

func (s *Service) SendCode(ctx context.Context, email, purpose string) error {
	email, err := normalizeEmail(email)
	if err != nil || !validPurpose(purpose) {
		return ErrInvalid
	}
	return s.sendCode(ctx, email, purpose)
}

func (s *Service) sendCode(ctx context.Context, email, purpose string) error {
	return s.sendScopedCode(ctx, email, purpose, purpose)
}

func (s *Service) sendScopedCode(ctx context.Context, email, purpose, scope string) error {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	digest := s.codeDigest(email, scope, code)
	now := s.now()
	if err := s.store.PutCode(ctx, email, purpose, digest, now, now.Add(10*time.Minute)); err != nil {
		return err
	}
	if err := s.send(ctx, email, code); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		return errors.Join(ErrDelivery, s.store.ForgetCode(cleanup, email, purpose, digest))
	}
	return nil
}

func (s *Service) SendBindingEmail(ctx context.Context, token, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	account, err := s.Account(ctx, token)
	if err != nil {
		return err
	}
	if account.Name == "" || account.Email != "" {
		return store.ErrConflict
	}
	return s.sendCode(ctx, email, "bind:"+account.ID)
}

func (s *Service) BindEmail(ctx context.Context, token, email, code, password string) (store.Account, error) {
	email, err := normalizeEmail(email)
	if err != nil || !validCode(code) || len(password) > passwordMax {
		return store.Account{}, ErrInvalid
	}
	account, err := s.Account(ctx, token)
	if err != nil {
		return store.Account{}, err
	}
	if account.Name == "" || account.Email != "" {
		return store.Account{}, store.ErrConflict
	}
	verified, hash, err := s.store.PasswordAccount(ctx, account.Name)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Account{}, err
	}
	if !passwordMatches(hash, password) || verified.ID != account.ID {
		return store.Account{}, ErrCredentials
	}
	purpose := "bind:" + account.ID
	return s.store.BindEmail(ctx, store.EmailBinding{
		AccountID: account.ID, Email: email, Purpose: purpose, PasswordHash: hash,
		CodeDigest: s.codeDigest(email, purpose, code), SessionDigest: sessionDigest(token), Now: s.now(),
	})
}

func validCode(code string) bool {
	return len(code) == 6 && strings.Trim(code, "0123456789") == ""
}

func (s *Service) UnlinkIdentity(ctx context.Context, token, provider, password string) (store.Account, error) {
	account, err := s.Account(ctx, token)
	if err != nil {
		return store.Account{}, err
	}
	if account.Name == "" {
		return store.Account{}, store.ErrConflict
	}
	if provider == "email" {
		return s.store.UnlinkIdentity(ctx, account, provider, "", sessionDigest(token), s.now())
	}
	if provider != "github" || password == "" || len(password) > passwordMax {
		return store.Account{}, ErrInvalid
	}
	verified, hash, err := s.store.PasswordAccount(ctx, account.Name)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Account{}, err
	}
	if !passwordMatches(hash, password) || verified.ID != account.ID {
		return store.Account{}, ErrCredentials
	}
	return s.store.UnlinkIdentity(ctx, account, provider, hash, sessionDigest(token), s.now())
}

type EmailInput struct {
	Email, Purpose, Code, Password string
}

type Session struct {
	Account   store.Account
	Token     string
	ExpiresAt time.Time
}

func (s *Service) SetLoginCredentials(ctx context.Context, token, username, password, confirmation string) (store.Account, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameFormat.MatchString(username) || password != confirmation || len(password) < passwordMin || len(password) > passwordMax {
		return store.Account{}, ErrInvalid
	}
	hash, err := hashPassword(password)
	if err != nil {
		return store.Account{}, err
	}
	return s.store.SetLoginCredentials(ctx, username, hash, sessionDigest(token), s.now())
}

func (s *Service) VerifyEmail(ctx context.Context, in EmailInput) (Session, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil || !validPurpose(in.Purpose) || !validCode(in.Code) {
		return Session{}, ErrInvalid
	}
	var hash string
	if in.Purpose == PurposeReset {
		if len(in.Password) < passwordMin || len(in.Password) > passwordMax {
			return Session{}, ErrInvalid
		}
		hash, err = hashPassword(in.Password)
		if err != nil {
			return Session{}, err
		}
	}
	now := s.now()
	token := rand.Text()
	expires := now.Add(sessionTTL)
	account, err := s.store.VerifyEmail(ctx, store.EmailVerification{
		Email: email, Purpose: in.Purpose, PasswordHash: hash,
		CodeDigest: s.codeDigest(email, in.Purpose, in.Code), SessionDigest: sessionDigest(token),
		Now: now, SessionExpires: expires,
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Account: account, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) LoginPassword(ctx context.Context, username, password string) (Session, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameFormat.MatchString(username) || len(password) > passwordMax {
		return Session{}, ErrCredentials
	}
	account, hash, err := s.store.PasswordAccount(ctx, username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Session{}, err
	}
	if !passwordMatches(hash, password) || err != nil {
		return Session{}, ErrCredentials
	}
	token := rand.Text()
	now := s.now()
	expires := now.Add(sessionTTL)
	if err := s.store.PutPasswordSession(ctx, account.ID, hash, sessionDigest(token), now, expires); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Session{}, ErrCredentials
		}
		return Session{}, err
	}
	return Session{Account: account, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) Account(ctx context.Context, token string) (store.Account, error) {
	return s.store.SessionAccount(ctx, sessionDigest(token), s.now())
}

func (s *Service) Refresh(ctx context.Context, token string) (Session, error) {
	now := s.now()
	next := rand.Text()
	expires := now.Add(sessionTTL)
	account, err := s.store.RotateSession(ctx, sessionDigest(token), sessionDigest(next), now, expires)
	if err != nil {
		return Session{}, err
	}
	return Session{Account: account, Token: next, ExpiresAt: expires}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.RevokeSession(ctx, sessionDigest(token))
}

func sessionDigest(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	digest, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-v1:" + hex.EncodeToString(append(salt, digest...)), nil
}

func passwordMatches(encoded, password string) bool {
	raw, err := hex.DecodeString(strings.TrimPrefix(encoded, "pbkdf2-v1:"))
	valid := strings.HasPrefix(encoded, "pbkdf2-v1:") && err == nil && len(raw) == 48
	if !valid {
		raw = make([]byte, 48)
	}
	digest, err := pbkdf2.Key(sha256.New, password, raw[:16], passwordRounds, 32)
	return err == nil && subtle.ConstantTimeCompare(digest, raw[16:]) == 1 && valid
}
