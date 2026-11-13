// Command lll-cloud runs the Learn-With-AI account and inference API.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/auth"
	"github.com/xmz14/lll/backend-go/internal/cloud/httpapi"
	"github.com/xmz14/lll/backend-go/internal/cloud/maildelivery"
	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/platform/envname"
)

type config struct {
	httpapi.ModelConfig
	Budget            httpapi.Budget
	UpstreamHosts     []string
	TrustedProxyCIDRs []string
	Operators         map[string]struct{ Role, TOTPEnv string }
	GitHub            struct {
		ClientID, SecretEnv, Callback, TokenProxyURL string
		FallbackProxyURLs                            []string
	}
	Google struct {
		ClientID, SecretEnv, ProxyURL string
		FallbackProxyURLs             []string
	}
	Prompts struct {
		Root, Version, KeyEnv string
	}
	SMTP struct {
		Address, From, Username, PasswordEnv string
		ImplicitTLS                          bool
	}
}

func loadConfig(path string) (config, error) {
	var cfg config
	if path == "" {
		return cfg, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("invalid trailing cloud configuration")
	}
	return cfg, nil
}

func promptSigningKey(cfg config) (ed25519.PrivateKey, error) {
	if cfg.Prompts.Root == "" {
		return nil, errors.New("prompt publishing is not configured")
	}
	seed, err := base64.StdEncoding.DecodeString(envname.Value(cfg.Prompts.KeyEnv))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("prompt signing key must encode 32 random seed bytes")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func printPromptPublicKey(path string, out io.Writer) error {
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	key, err := promptSigningKey(cfg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
	return err
}

func checkConfig(ctx context.Context, path, snapshot string) error {
	scratch, err := os.MkdirTemp("", "lwa-cloud-config-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	database := filepath.Join(scratch, "check.db")
	if snapshot != "" {
		if err := copyConfigSnapshot(snapshot, database); err != nil {
			return fmt.Errorf("read SQLite preflight snapshot: %w", err)
		}
	}
	return run(ctx, "", path, database, true)
}

func copyConfigSnapshot(snapshot, database string) error {
	info, err := os.Stat(snapshot)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("expected a nonempty SQLite snapshot file")
	}
	source, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(database, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(destination, source)
	return errors.Join(err, destination.Close())
}

func run(ctx context.Context, address, path, database string, checkOnly bool) error {
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(envname.Value("LWA_CLOUD_OTP_KEY"))
	if err != nil || len(key) < 32 {
		return errors.New("LWA_CLOUD_OTP_KEY must encode at least 32 random bytes")
	}
	db, err := store.Open(database)
	if err != nil {
		return err
	}
	defer db.Close()
	relay := maildelivery.SMTP{
		Address: cfg.SMTP.Address, From: cfg.SMTP.From, Username: cfg.SMTP.Username,
		Password: envname.Value(cfg.SMTP.PasswordEnv), ImplicitTLS: cfg.SMTP.ImplicitTLS,
	}
	if cfg.SMTP.Address != "" || cfg.SMTP.From != "" || cfg.SMTP.Username != "" ||
		cfg.SMTP.PasswordEnv != "" || cfg.SMTP.ImplicitTLS {
		if err := relay.Validate(); err != nil {
			return err
		}
	}
	accounts, err := auth.New(db, key, relay.SendCode)
	if err != nil {
		return err
	}
	hosts := append([]string{"open.bigmodel.cn", "api.deepseek.com"}, cfg.UpstreamHosts...)
	builder := httpapi.NewModelBuilder(hosts, envname.Value)
	active, err := db.ActiveModelConfig(ctx)
	if err == nil {
		if err := json.Unmarshal(active.Config, &cfg.ModelConfig); err != nil {
			return err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	gateway, err := builder(cfg.ModelConfig)
	if err != nil {
		return err
	}
	handler, err := httpapi.New(db, accounts, gateway, cfg.Models)
	if err != nil {
		return err
	}
	if err := handler.TrustProxies(cfg.TrustedProxyCIDRs); err != nil {
		return err
	}
	if cfg.GitHub.ClientID != "" || cfg.GitHub.SecretEnv != "" || cfg.GitHub.Callback != "" || cfg.GitHub.TokenProxyURL != "" || len(cfg.GitHub.FallbackProxyURLs) > 0 {
		github, err := auth.NewGitHub(db, cfg.GitHub.ClientID, envname.Value(cfg.GitHub.SecretEnv), cfg.GitHub.Callback)
		if err != nil {
			return err
		}
		if cfg.GitHub.TokenProxyURL != "" || len(cfg.GitHub.FallbackProxyURLs) > 0 {
			if err := github.SetTokenProxy(cfg.GitHub.TokenProxyURL, cfg.GitHub.FallbackProxyURLs...); err != nil {
				return err
			}
		}
		handler.EnableGitHub(github)
	}
	if cfg.Google.ClientID != "" || cfg.Google.SecretEnv != "" || cfg.Google.ProxyURL != "" || len(cfg.Google.FallbackProxyURLs) > 0 {
		google, err := auth.NewGoogle(db, cfg.Google.ClientID, envname.Value(cfg.Google.SecretEnv), cfg.Google.ProxyURL, cfg.Google.FallbackProxyURLs...)
		if err != nil {
			return err
		}
		handler.EnableGoogle(google)
	}
	var promptKey ed25519.PrivateKey
	if cfg.Prompts.Root != "" {
		promptKey, err = promptSigningKey(cfg)
		if err != nil {
			return err
		}
		active, err := db.ActivePromptConfig(ctx)
		var draft promptcatalog.Draft
		switch {
		case err == nil:
			err = json.Unmarshal(active.Config, &draft)
			if err == nil && draft.Resources != nil && !promptcatalog.HasReviewHTMLContract(draft.Resources["review"]) {
				var review string
				review, err = promptcatalog.ReadResource(cfg.Prompts.Root, "review")
				if err == nil {
					if !promptcatalog.HasReviewHTMLContract(review) {
						return errors.New("cloud review prompt bundle must contain the HTML review contract")
					}
					draft.Resources["review"] = review
					var body []byte
					body, err = json.Marshal(draft)
					if err == nil {
						active.Version, err = db.PublishPromptConfig(ctx, active.Version, "system-review-html", body, time.Now())
					}
				}
			}
		case errors.Is(err, store.ErrNotFound):
			draft, err = promptcatalog.LoadDraft(cfg.Prompts.Root, cfg.Prompts.Version)
		}
		if err != nil {
			return err
		}
		base := draft.Resources
		if draft.Comparison != nil && active.Version > 0 {
			initial, err := db.InitialExperimentConfig(ctx, draft.Comparison.ID)
			if err != nil {
				return err
			}
			var original promptcatalog.Draft
			if err := json.Unmarshal(initial.Config, &original); err != nil {
				return err
			}
			base = original.Resources
			if base == nil {
				return errors.New("experiment template baseline missing")
			}
			if !promptcatalog.HasReviewHTMLContract(base["review"]) {
				base["review"] = draft.Resources["review"]
			}
		}
		catalog, err := promptcatalog.BuildWithExperimentBase(draft, base, promptKey)
		if err != nil {
			return err
		}
		if active.Version == 0 && len(cfg.Operators) > 0 {
			body, err := json.Marshal(draft)
			if err != nil {
				return err
			}
			if _, err := db.PublishPromptConfig(ctx, 0, "bootstrap", body, time.Now()); err != nil {
				return err
			}
		}
		handler.EnablePrompts(catalog, key)
	}
	if len(cfg.Operators) > 0 {
		operators := make(map[string]httpapi.Operator, len(cfg.Operators))
		for principal, operator := range cfg.Operators {
			entry := httpapi.Operator{Role: operator.Role}
			if operator.TOTPEnv != "" {
				secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(envname.Value(operator.TOTPEnv))
				if err != nil || len(secret) < 20 {
					return errors.New("invalid operator authentication configuration")
				}
				entry.TOTPKey = secret
			}
			operators[principal] = entry
		}
		if err := handler.EnableAdmin(operators); err != nil {
			return err
		}
		if active.Version == 0 {
			body, err := json.Marshal(cfg.ModelConfig)
			if err != nil {
				return err
			}
			active.Version, err = db.PublishModelConfig(ctx, 0, "bootstrap", body, time.Now())
			if err != nil {
				return err
			}
		}
		handler.EnableModelAdmin(builder)
		if len(promptKey) != 0 {
			handler.EnablePromptAdmin(promptKey)
		}
	}
	if err := handler.SetStartupModelRevision(cfg.ModelConfig, active.Version); err != nil {
		return err
	}
	if err := handler.EnableBudget(cfg.Budget); err != nil {
		return err
	}
	if checkOnly {
		return nil
	}
	if err := db.ExpireInferenceHolds(ctx, time.Now()); err != nil {
		return fmt.Errorf("expire stale inference holds: %w", err)
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				if err := db.ExpireInferenceHolds(ctx, now); err != nil && ctx.Err() == nil {
					log.Printf("expire stale inference holds: %v", err)
				}
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	log.Printf("Learn-With-AI cloud listening on %s", address)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	address := flag.String("listen", "127.0.0.1:8789", "HTTP listen address")
	path := flag.String("config", "", "cloud configuration JSON path")
	database := flag.String("database", "cloud.db", "SQLite database path")
	publicKey := flag.Bool("print-prompt-public-key", false, "print the configured prompt signing public key and exit")
	check := flag.Bool("check-config", false, "validate startup configuration without opening the service database or listening")
	snapshot := flag.String("check-snapshot", "", "optional consistent SQLite snapshot to copy for -check-config")
	flag.Parse()
	if *check && *publicKey {
		log.Fatal("choose either -check-config or -print-prompt-public-key")
	}
	if *snapshot != "" && !*check {
		log.Fatal("-check-snapshot requires -check-config")
	}
	if *publicKey {
		if err := printPromptPublicKey(*path, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *check {
		if err := checkConfig(ctx, *path, *snapshot); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Cloud startup configuration validated.")
		return
	}
	if err := run(ctx, *address, *path, *database, false); err != nil {
		log.Fatal(err)
	}
}
