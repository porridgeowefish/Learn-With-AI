// Command lll is the Local Learning Lab HTTP server entry point.
package main

import (
	"flag"
	"fmt"
	browser "github.com/xmz14/lll/backend-go/internal/platform/browser"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/xmz14/lll/backend-go/internal/app/bootstrap"
	"github.com/xmz14/lll/backend-go/internal/app/desktop"
	"github.com/xmz14/lll/backend-go/internal/app/workspaceservice"
	"github.com/xmz14/lll/backend-go/internal/platform/config"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/transport/httpserver"
)

var Version = "0.3.0-dev"

func main() {
	var (
		versionOnly = flag.Bool("version", false, "print version")
		noBrowser   = flag.Bool("no-browser", false, "do not open the browser")
		port        = flag.Int("port", 0, "server port (overrides config and LLL_SERVER_PORT)")
		workspace   = flag.String("workspace", "", "workspace root (overrides config and LLL_WORKSPACE_ROOT)")
		configPath  = flag.String("config", "", "config file path (default: <workspace>/config.local.json)")
	)
	flag.Parse()
	if *versionOnly {
		fmt.Println("Learn-With-AI " + Version)
		return
	}

	cfg, instance, err := workspaceservice.OpenRuntime(config.Options{Path: *configPath, WorkspaceRoot: *workspace, Port: *port}, paths.DefaultWorkspaceRoot(paths.PROJECT_ROOT))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings {
		fmt.Println("config:", warning)
	}
	defer instance.Close()

	os.Setenv("WORKSPACE", paths.WORKSPACE)
	portEnv := strconv.Itoa(cfg.Server.Port)
	addr := net.JoinHostPort(cfg.Server.Host, portEnv)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		instance.Close()
		os.Exit(1)
	}
	defer listener.Close()
	os.Setenv("LEARN_WITH_AI_SERVER_PORT", strconv.Itoa(cfg.Server.Port))
	workspaceservice.ProducerVersion = Version
	srv := bootstrap.Build()
	defer srv.Server.Close()

	fmt.Printf("Learn-With-AI %s\n", Version)
	fmt.Printf("  Workspace: %s\n", paths.WORKSPACE)
	fmt.Printf("  Projects:  %s\n", paths.PROJECTS_ROOT)
	fmt.Printf("  Frontend:  %s\n", paths.FRONTEND_ROOT)
	fmt.Printf("Listening on http://localhost:%s\n", portEnv)

	httpServer := &http.Server{Addr: addr}
	updates, stopUpdates := desktop.UpdateHandler(Version, func() { _ = httpServer.Close() })
	defer stopUpdates()
	mux := http.NewServeMux()
	mux.Handle("/api/system/update", updates)
	mux.Handle("/api/system/update/", updates)
	mux.Handle("/", srv.Server.Handler())
	httpServer.Handler = mux
	srv.Server.SetShutdownFunc(func() {
		time.Sleep(200 * time.Millisecond)
		fmt.Println("\nShutting down from UI...")
		_ = httpServer.Close()
	})

	// Graceful shutdown on Ctrl+C.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nShutting down...")
		httpServer.Close()
	}()

	os.Setenv("LEARN_WITH_AI_SERVER_PORT", portEnv)
	browserHost := cfg.Server.Host
	if browserHost == "0.0.0.0" {
		browserHost = "127.0.0.1"
	}
	if browserHost == "::" {
		browserHost = "::1"
	}
	url := "http://" + net.JoinHostPort(browserHost, portEnv)
	if instance.IsDefault() {
		defer httpserver.PublishTransferEndpoint(url)()
	}
	if !*noBrowser {
		if err := browser.Open(url); err != nil {
			fmt.Println("Open browser manually:", url)
		}
	}
	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}
