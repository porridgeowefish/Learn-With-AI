// Command archcheck enforces the LLL modular-monolith dependency policy
// (ADR-0015). It exits non-zero and prints a human-readable violation plus
// writes a machine-readable report when an import violates the approved graph.
//
// Rules:
//
//	R1 transport must not import a module-private package (modules/*/internal/**)
//	R2 a module must not import another module's private internal/** package
//	R3 platform must not import modules, transport, or app
//	R4 (frontend) a feature may not import another feature's non-index file
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	repoRoot := flag.String("repo", ".", "repository root")
	reportPath := flag.String("report", "trash/verification/architecture/dependency-report.json", "machine-readable report output")
	allowFile := flag.String("allow", "", "optional file listing temporarily-allowed imports (one 'importer -> imported' per line, '#' comments)")
	skipFrontend := flag.Bool("skip-frontend", false, "skip the frontend feature-boundary rule")
	flag.Parse()

	violations, stats, err := check(*repoRoot, *allowFile, !*skipFrontend)
	if err != nil {
		fmt.Fprintln(os.Stderr, "archcheck:", err)
		os.Exit(2)
	}

	if err := os.MkdirAll(dirOf(*reportPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "archcheck: create report dir:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(*reportPath, mustJSON(violations), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "archcheck: write report:", err)
		os.Exit(2)
	}

	if len(violations) > 0 {
		fmt.Printf("archcheck: %d violation(s)\n", len(violations))
		for _, v := range violations {
			fmt.Printf("  [%s] %s imports %s %s\n", v.Rule, v.Importer, v.Imported, v.Detail)
		}
		fmt.Printf("report: %s\n", *reportPath)
		os.Exit(1)
	}
	fmt.Printf("archcheck: OK (%d packages, %d edges)\n", stats.Packages, stats.Edges)
}
