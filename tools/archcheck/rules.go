package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	repoModule      = "github.com/xmz14/lll"
	transportPrefix = repoModule + "/backend-go/internal/transport"
	platformPrefix  = repoModule + "/backend-go/internal/platform"
	modulesPrefix   = repoModule + "/backend-go/internal/modules"
	appPrefix       = repoModule + "/backend-go/internal/app"
)

var moduleInternalRe = regexp.MustCompile(`^` + modulesPrefix + `/([a-z]+)/internal/`)

// Violation is one rule breach.
type Violation struct {
	Rule     string `json:"rule"`
	Importer string `json:"importer"`
	Imported string `json:"imported"`
	Detail   string `json:"detail,omitempty"`
}

// stats summarizes what was checked.
type stats struct {
	Packages int `json:"packagesChecked"`
	Edges    int `json:"edgesChecked"`
}

// check runs every rule and returns violations plus counting stats.
func check(repoRoot, allowFile string, withFrontend bool) ([]Violation, stats, error) {
	var st stats
	allowed := map[string]bool{}
	if allowFile != "" {
		data, err := os.ReadFile(allowFile)
		if err != nil {
			return nil, st, fmt.Errorf("read allowlist: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			allowed[line] = true
		}
	}

	var violations []Violation
	for _, retired := range []string{"learning-agents", "backend-go/internal/modules/assistant", "backend-go/internal/compatibility", "backend-go/internal/modules/learning", "backend-go/internal/app/integration", "frontend/src/features/legacy-zones", "frontend/src/features/agents"} {
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(retired))); err == nil {
			violations = append(violations, Violation{Rule: "R6-retired-surface", Importer: retired, Detail: "retired runtime or UI must not return"})
		} else if !os.IsNotExist(err) {
			return nil, st, err
		}
	}

	pkgs, err := listGoPackages(repoRoot, "./backend-go/...")
	if err != nil {
		return nil, st, err
	}
	st.Packages = len(pkgs)
	for importer, deps := range pkgs {
		for _, dep := range deps {
			st.Edges++
			if v := checkGoEdge(importer, dep); v != nil && !allowed[v.Importer+" -> "+v.Imported] {
				violations = append(violations, *v)
			}
		}
	}

	if withFrontend {
		fe, err := checkFrontend(filepath.Join(repoRoot, "frontend", "src", "features"))
		if err != nil {
			return nil, st, err
		}
		for _, v := range fe {
			if !allowed[v.Importer+" -> "+v.Imported] {
				violations = append(violations, v)
			}
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Importer != violations[j].Importer {
			return violations[i].Importer < violations[j].Importer
		}
		return violations[i].Imported < violations[j].Imported
	})
	return violations, st, nil
}

// checkGoEdge applies R1–R3 to one import edge.
func checkGoEdge(importer, imported string) *Violation {
	if moduleOf(importer) == "ai" && strings.HasPrefix(imported, modulesPrefix+"/") && moduleOf(imported) != "ai" {
		return &Violation{Rule: "R5-ai-imports-business", Importer: importer, Imported: imported}
	}
	if strings.HasPrefix(importer, transportPrefix) {
		if m := moduleInternalRe.FindStringSubmatch(imported); m != nil {
			return &Violation{Rule: "R1-transport-imports-module-private", Importer: importer, Imported: imported, Detail: fmt.Sprintf("module %q internals are private", m[1])}
		}
	}
	if strings.HasPrefix(importer, platformPrefix) {
		if strings.HasPrefix(imported, modulesPrefix) || strings.HasPrefix(imported, transportPrefix) || strings.HasPrefix(imported, appPrefix) {
			return &Violation{Rule: "R3-platform-imports-business", Importer: importer, Imported: imported}
		}
	}
	if m := moduleInternalRe.FindStringSubmatch(imported); m != nil {
		if importerModule := moduleOf(importer); importerModule != "" && importerModule != m[1] {
			return &Violation{Rule: "R2-cross-module-private", Importer: importer, Imported: imported, Detail: fmt.Sprintf("%q cannot reach %q internals", importerModule, m[1])}
		}
	}
	return nil
}

// moduleOf returns the capability name when importer lives under modules/.
func moduleOf(importPath string) string {
	if !strings.HasPrefix(importPath, modulesPrefix+"/") {
		return ""
	}
	rest := strings.TrimPrefix(importPath, modulesPrefix+"/")
	return strings.SplitN(rest, "/", 2)[0]
}

var feImportRe = regexp.MustCompile(`(?:from\s+|import\s*\(\s*|require\()\s*['"]([^'"]+)['"]`)

// checkFrontend applies R4: features/<a> may reach features/<b> only via
// features/<b>/index.ts(.). Files under a feature may freely import within
// it and anything outside features/.
func checkFrontend(featuresDir string) ([]Violation, error) {
	info, err := os.Stat(featuresDir)
	if err != nil || !info.IsDir() {
		// Features directory not created yet (early waves) — rule is vacuous.
		return nil, nil
	}
	var violations []Violation
	err = filepath.Walk(featuresDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil || fi.IsDir() {
			return walkErr
		}
		name := fi.Name()
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			return nil
		}
		if strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(featuresDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		feature := strings.SplitN(rel, "/", 2)[0]
		importer := "frontend/src/features/" + rel

		for _, m := range feImportRe.FindAllStringSubmatch(string(data), -1) {
			spec := m[1]
			var targetFeature, targetTail string
			switch {
			case strings.HasPrefix(spec, "@/features/"):
				targetFeature, targetTail = splitFeaturePath(strings.TrimPrefix(spec, "@/features/"))
			case strings.Contains(spec, "/features/"):
				idx := strings.LastIndex(spec, "/features/") + len("/features/")
				targetFeature, targetTail = splitFeaturePath(spec[idx:])
			case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
				// Resolve relative to the importing file inside features/.
				base := filepath.ToSlash(filepath.Dir("features/" + rel))
				resolved := filepath.ToSlash(filepath.Clean(base + "/" + spec))
				if !strings.HasPrefix(resolved, "features/") {
					continue // escapes features/ — not this rule's concern
				}
				targetFeature, targetTail = splitFeaturePath(strings.TrimPrefix(resolved, "features/"))
			default:
				continue
			}
			if targetFeature == "" || targetFeature == feature {
				continue
			}
			if targetTail == "" || targetTail == "index" || targetTail == "index.ts" || targetTail == "index.tsx" {
				continue
			}
			violations = append(violations, Violation{
				Rule:     "R4-frontend-cross-feature-internal",
				Importer: importer,
				Imported: spec,
				Detail:   fmt.Sprintf("feature %q internals are private; import its index.ts", targetFeature),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return violations, nil
}

func splitFeaturePath(rest string) (feature, tail string) {
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// goListPackage mirrors the `go list -json` fields we consume.
type goListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

// listGoPackages returns importer → direct repo-internal imports. dir is the
// module root the pattern resolves against.
func listGoPackages(dir, pattern string) (map[string][]string, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", pattern)
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	pkgs := map[string][]string{}
	dec := json.NewDecoder(&stdout)
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			break
		}
		if !strings.HasPrefix(pkg.ImportPath, repoModule+"/backend-go/internal/") {
			continue
		}
		var deps []string
		for _, dep := range pkg.Imports {
			if strings.HasPrefix(dep, repoModule+"/backend-go/internal/") && dep != pkg.ImportPath {
				deps = append(deps, dep)
			}
		}
		pkgs[pkg.ImportPath] = deps
	}
	return pkgs, nil
}

func mustJSON(v any) []byte {
	blob, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("[]")
	}
	return append(blob, '\n')
}

func dirOf(path string) string {
	dir := filepath.Dir(path)
	if dir == "" {
		return "."
	}
	return dir
}
