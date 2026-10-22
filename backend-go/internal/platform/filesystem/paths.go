// Package filesystem exposes canonical filesystem roots for LLL.
// All other packages resolve project and workspace paths through here.
package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WorkspaceDirName is the directory (inside the repository) that holds all
// learner-owned runtime data by default: projects/, preferences.md,
// config.local.json, folders ledger, and workspace-local tmp/ diagnostics.
const WorkspaceDirName = "workspace"

// DefaultWorkspaceRoot returns the default learner workspace root for a given
// repository root. Configuration loaders and package init must agree on this
// value; keep a single derivation so the config-file lookup and the runtime
// path variables can never diverge (see LESSONS_LEARNED #16 and #19).
func DefaultWorkspaceRoot(projectRoot string) string {
	if BuildMode == "release" {
		home, err := os.UserHomeDir()
		if err != nil {
			panic("cannot resolve user home")
		}
		root, err := DefaultUserWorkspace(runtime.GOOS, os.Getenv, home)
		if err != nil {
			panic(err.Error())
		}
		return root
	}
	return filepath.Join(projectRoot, WorkspaceDirName)
}

// PROJECT_ROOT is the repository root (where go.mod lives).
// WORKSPACE is the user's learning workspace (defaults to
// DefaultWorkspaceRoot(PROJECT_ROOT); configurable via WORKSPACE env).
// PROJECTS_ROOT holds all learning project folders.
var (
	PROJECT_ROOT   string
	WORKSPACE      string
	PROJECTS_ROOT  string
	FRONTEND_ROOT  string
	FRONTEND_INDEX string
)

func init() {
	// Resolve PROJECT_ROOT by walking up from the working directory until we find go.mod.
	wd, err := os.Getwd()
	if err != nil {
		panic("paths: cannot get working directory: " + err.Error())
	}
	executable, err := os.Executable()
	if err != nil {
		panic("paths: cannot resolve executable")
	}
	PROJECT_ROOT, err = DiscoverResourceRoot(wd, executable, BuildMode)
	if err != nil {
		panic("paths: " + err.Error())
	}

	if env := os.Getenv("WORKSPACE"); env != "" {
		if err := ConfigureWorkspace(env); err != nil {
			panic("paths: configure workspace: " + err.Error())
		}
	} else if isTestBinary() {
		// Test binaries must never default to the developer's real workspace:
		// an unisolated test writing through paths.WORKSPACE would corrupt
		// learner data (observed 2026-09-11 as stray "topic"/"other" projects).
		// Tests that need a specific workspace still configure one explicitly.
		scratch, err := os.MkdirTemp("", "lll-test-workspace-")
		if err != nil {
			panic("paths: test workspace: " + err.Error())
		}
		TestScratchWorkspace = scratch
		if err := ConfigureWorkspace(scratch); err != nil {
			panic("paths: configure workspace: " + err.Error())
		}
	} else {
		if err := ConfigureWorkspace(DefaultWorkspaceRoot(PROJECT_ROOT)); err != nil {
			panic("paths: configure workspace: " + err.Error())
		}
	}
	// FRONTEND_ROOT points at the Vite build output (frontend/dist) so a
	// single Go binary can serve the React SPA in production. In dev the
	// frontend runs on Vite :5173 and proxies API calls here.
	FRONTEND_ROOT = filepath.Join(PROJECT_ROOT, "frontend", "dist")
	FRONTEND_INDEX = filepath.Join(FRONTEND_ROOT, "index.html")
}

// TestScratchWorkspace is the throwaway workspace a go test binary starts
// with. Other packages that derive data locations independently of WORKSPACE
// (the config file) use it so an unisolated test can never write into the
// developer's real <repo>/workspace. Empty in every non-test process.
var TestScratchWorkspace string

// isTestBinary reports whether the current process is a compiled go test
// binary, so package init can keep unisolated tests away from real data.
func isTestBinary() bool {
	a := os.Args[0]
	return strings.HasSuffix(a, ".test") || strings.HasSuffix(a, ".test.exe")
}

// ConfigureWorkspace applies one resolved workspace root to every mutable
// runtime data path. Call it after loading CLI/environment/file configuration;
// changing WORKSPACE in the process environment after package initialization
// does not update Go package variables by itself.
func ConfigureWorkspace(root string) error {
	if root == "" {
		return os.ErrInvalid
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	WORKSPACE = filepath.Clean(abs)
	// The workspace root holds learner-owned files that several stores write
	// directly (preferences.md, folders ledger, tmp diagnostics), so create it
	// eagerly instead of relying on every writer to MkdirAll its parent.
	if err := os.MkdirAll(WORKSPACE, 0o755); err != nil {
		return fmt.Errorf("create workspace root: %w", err)
	}
	PROJECTS_ROOT = filepath.Join(WORKSPACE, "projects")
	return nil
}
