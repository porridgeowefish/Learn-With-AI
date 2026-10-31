// Package desktop owns the native application's private in-process HTTP engine.
package desktop

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/app/bootstrap"
	"github.com/xmz14/lll/backend-go/internal/platform/config"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

type Options struct {
	Workspace, Config string
	Port              int
	// Version is the running release version, used for update checks.
	Version string
}
type Engine struct {
	URL       string
	Workspace string
	Done      chan struct{}
	server    *http.Server
	app       *bootstrap.App
	instance  *os.File
	once      sync.Once
}

func Start(options Options) (*Engine, error) {
	root := options.Workspace
	if root == "" {
		root = os.Getenv("LLL_WORKSPACE_ROOT")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root, err = paths.DefaultUserWorkspace("windows", os.Getenv, home)
		if err != nil {
			return nil, err
		}
	}
	cfg, _, err := config.Load(config.Options{WorkspaceRoot: root, Path: options.Config, Port: options.Port})
	if err != nil {
		return nil, err
	}
	if err = paths.ConfigureWorkspace(cfg.WorkspaceRootResolved()); err != nil {
		return nil, err
	}
	instance, err := paths.AcquireWorkspaceInstance(paths.WORKSPACE)
	if err != nil {
		return nil, &BusyError{Workspace: paths.WORKSPACE, Err: err}
	}
	// A desktop owns a private IPv4 listener, even when a maintainer's server is running.
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(options.Port)))
	if err != nil {
		instance.Close()
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	os.Setenv("WORKSPACE", paths.WORKSPACE)
	os.Setenv("LEARN_WITH_AI_SERVER_PORT", strconv.Itoa(port))
	app := bootstrap.Build()
	engine := &Engine{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Workspace: paths.WORKSPACE, Done: make(chan struct{}), app: app, instance: instance}
	// Desktop-only update check routes sit in front of the
	// shared application server.
	updates := newUpdater(options.Version).handler()
	mux := http.NewServeMux()
	mux.Handle("/api/system/update", updates)
	mux.Handle("/api/system/update/", updates)
	mux.Handle("/", app.Server.Handler())
	engine.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	app.Server.SetShutdownFunc(func() { time.Sleep(200 * time.Millisecond); engine.Close() })
	go func() { _ = engine.server.Serve(listener) }()
	return engine, nil
}

// BusyError reports that another process already owns the resolved workspace.
// Workspace is the exact path the owner registered, so a second launch can
// find and focus the running window instead of failing.
type BusyError struct {
	Workspace string
	Err       error
}

func (e *BusyError) Error() string { return e.Err.Error() }
func (e *BusyError) Unwrap() error { return e.Err }

// windowRecordName is written by the running native window: "<hwnd> <pid>".
const windowRecordName = ".learn-with-ai.window"

func (engine *Engine) Close() {
	engine.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// SSE streams can remain open; bounded shutdown then closes owned connections.
		_ = engine.server.Shutdown(ctx)
		_ = engine.server.Close()
		engine.app.Server.Close()
		_ = engine.instance.Close()
		close(engine.Done)
	})
}

// AllowedExternalURL restricts the only shell bridge to public HTTP(S) links.
// Workspace files remain on the owned server's /files routes, never shell paths.
func AllowedExternalURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return false
	}
	return true
}

// DefaultWorkspace is stable across desktop launch directories and never
// defaults to a maintainer repository's workspace.
func DefaultWorkspace() string {
	if root := os.Getenv("LLL_WORKSPACE_ROOT"); root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	root, _ := paths.DefaultUserWorkspace("windows", os.Getenv, home)
	return root
}
