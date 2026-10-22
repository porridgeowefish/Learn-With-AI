package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	learningscope "github.com/xmz14/lll/backend-go/internal/modules/projects/internal/learningscope"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ProjectType distinguishes broad orientation maps from focused learning work.
type ProjectType string

const (
	ProjectTypeDisciplineMap  ProjectType = "discipline-map"
	ProjectTypeSystemLearning ProjectType = "system-learning"
)

// validSlugPattern is the strict slug validator for URL/path parameters.
// Allows lowercase ASCII letters/digits/hyphens AND CJK / other lowercase
// Unicode letters (so Chinese project slugs like "场论-笔记" work).
// Rejects uppercase ASCII to keep URLs predictable.
var validSlugPattern = regexp.MustCompile(`^[a-z0-9\p{Ll}\p{Lo}][a-z0-9\p{Ll}\p{Lo}\-]*$`)

// ProjectState is the persistent per-project state.
type ProjectState struct {
	LayoutVersion int         `json:"layoutVersion,omitempty"`
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	Slug          string      `json:"slug"`
	ProjectType   ProjectType `json:"projectType"`
	Status        string      `json:"status"`
	CreatedAt     time.Time   `json:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`
}

// ProjectMeta is the lightweight summary used in the tree listing.
type ProjectMeta struct {
	ID                string      `json:"id"`
	Slug              string      `json:"slug"`
	Title             string      `json:"title"`
	ProjectType       ProjectType `json:"projectType"`
	OverviewAvailable bool        `json:"overviewAvailable"`
	LayoutStatus      string      `json:"layoutStatus,omitempty"`
}

// NormalizeProjectType preserves legacy projects and clients by treating an
// absent project type as the historical system-learning shape.
func NormalizeProjectType(t ProjectType) ProjectType {
	if t == "" {
		return ProjectTypeSystemLearning
	}
	return t
}

func ValidateProjectType(t ProjectType) bool {
	t = NormalizeProjectType(t)
	return t == ProjectTypeDisciplineMap || t == ProjectTypeSystemLearning
}

// ValidateSlug returns true if s is a valid project slug.
// Used for URL/path parameter validation to prevent path traversal.
func ValidateSlug(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	return validSlugPattern.MatchString(s)
}

// projectRoot returns the absolute path to a project's folder.
// Refuses slugs that traverse outside PROJECTS_ROOT.
func projectRoot(slug string) (string, error) {
	if !ValidateSlug(slug) {
		return "", fmt.Errorf("invalid slug: %q", slug)
	}
	root := filepath.Join(activeProjectsRoot(), slug)
	if _, err := os.Stat(filepath.Join(root, "state.json")); err == nil {
		return filepath.Abs(root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	projectsAbs, err := filepath.Abs(activeProjectsRoot())
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(projectsAbs, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("slug escapes projects root: %q", slug)
	}
	return abs, nil
}

// activeProjectsRoot returns the override (test) or the real PROJECTS_ROOT.
func activeProjectsRoot() string {
	if projectsRootOverride != "" {
		return projectsRootOverride
	}
	return filesystem.PROJECTS_ROOT
}

// ProjectExists reports whether a project state file is on disk.
func ProjectExists(slug string) (bool, error) {
	root, err := projectRoot(slug)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(root, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// DeleteProject removes a learning project and every artifact stored below its
// canonical project root. Callers must separately clean workspace-global
// references (for example folders) and reject deletion while a run is active.
func DeleteProject(slug string) error {
	if !ValidateSlug(slug) {
		return errors.New("invalid project slug")
	}
	root, err := ProjectRootForSlug(slug)
	if err != nil {
		return err
	}
	exists, err := ProjectExists(slug)
	if err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}
	return os.RemoveAll(root)
}

// CreateProjectSkeleton writes the canonical folder tree plus initial
// state.json and project.md. Returns os.ErrExist-equivalent
// (via *SlugConflictError) if the project already exists.
// CreateProjectSkeleton creates a project with empty background fields.
// parentSlug is ignored; it remains only for source compatibility. All new
// project directories are top-level and have no parent relationship.
func CreateProjectSkeleton(slug, title string, parentSlug string) error {
	return CreateProjectSkeletonWithInput(slug, title, parentSlug, ProjectInput{})
}

// CreateProjectSkeletonWithInput is the rich-form creator: writes project.md
// seeded with the user's why / current / target / standard fields.
func CreateProjectSkeletonWithInput(slug, title string, parentSlug string, in ProjectInput) error {
	root, err := projectRoot(slug)
	if err != nil {
		return err
	}

	// Check for collision.
	if exists, err := os.Stat(filepath.Join(root, "state.json")); err == nil && !exists.IsDir() {
		return &SlugConflictError{Slug: slug}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	projectType := NormalizeProjectType(in.ProjectType)
	if !ValidateProjectType(projectType) {
		return fmt.Errorf("invalid project type: %q", in.ProjectType)
	}
	if len(in.SystemPrompt) > 32*1024 {
		return errors.New("conversation instructions exceed 32768 UTF-8 bytes")
	}

	// Folder tree. Discipline maps deliberately do not own learning zones.
	dirs := []string{"", "assets"}
	if projectType == ProjectTypeSystemLearning {
		dirs = append(dirs, "conversation", "progress")
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	parsed, _ := time.Parse(time.RFC3339, now)

	state := &ProjectState{
		LayoutVersion: 1,
		ID:            slug,
		Title:         title,
		Slug:          slug,
		ProjectType:   projectType,
		Status:        "active",
		CreatedAt:     parsed,
		UpdatedAt:     parsed,
	}

	// state.json (atomic)
	if err := WriteProjectState(slug, state, ""); err != nil {
		return err
	}

	// project.md
	projectMd := fmt.Sprintf("# %s\n\n%s", title, defaultProjectMdBodyForInput(in))
	if err := filesystem.AtomicWriteFile(filepath.Join(root, "project.md"), []byte(projectMd), 0o644); err != nil {
		return err
	}

	if projectType == ProjectTypeSystemLearning {
		if err := filesystem.AtomicWriteFile(filepath.Join(root, "conversation", "system-prompt.txt"), []byte(in.SystemPrompt), 0o644); err != nil {
			return err
		}
		var scope any = learningscope.NewDraft(title, parsed)
		if in.LearningScope != nil {
			scope = in.LearningScope
		}
		scopeBytes, err := json.MarshalIndent(scope, "", "  ")
		if err != nil {
			return fmt.Errorf("encode learning scope: %w", err)
		}
		if err := filesystem.AtomicWriteFile(filepath.Join(root, "learning-scope.json"), scopeBytes, 0o644); err != nil {
			return err
		}
	} else {
		plan := []byte("{\n  \"schemaVersion\": 1,\n  \"items\": [],\n  \"updatedAt\": \"\"\n}\n")
		if err := filesystem.AtomicWriteFile(filepath.Join(root, "learning-plan.json"), plan, 0o644); err != nil {
			return err
		}
	}

	return nil
}

// ReadProjectState reads and decodes one top-level project's state.json.
func ReadProjectState(slug string) (*ProjectState, error) {
	root, err := projectRoot(slug)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		return nil, err
	}
	var s ProjectState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%w: decode state.json", ErrInvalidLayout)
	}
	s.ProjectType = NormalizeProjectType(s.ProjectType)
	if err := checkLayout(root, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func nonEmptyFile(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && len(strings.TrimSpace(string(data))) > 0
}

// WriteProjectState persists state.json atomically in the flat project root.
func WriteProjectState(slug string, state *ProjectState, parentSlug string) error {
	root, err := projectRoot(slug)
	if err != nil {
		return err
	}
	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(filepath.Join(root, "state.json"), data, 0o644)
}

// IndexAll returns top-level peer projects.
func IndexAll() ([]ProjectMeta, error) {
	var out []ProjectMeta
	if err := walkProjects(activeProjectsRoot(), &out); err != nil {
		return nil, err
	}
	// Stable sort by slug for deterministic listing.
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func walkProjects(dir string, out *[]ProjectMeta) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "subprojects" || name == "runs" || name == "assets" || name == "memory" ||
			name == "intro" || name == "explain" || name == "practice" || name == "extend" || name == "summary" {
			continue
		}
		childDir := filepath.Join(dir, name)
		statePath := filepath.Join(childDir, "state.json")
		if _, err := os.Stat(statePath); err != nil {
			continue
		}
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}
		var s ProjectState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		s.ProjectType = NormalizeProjectType(s.ProjectType)
		*out = append(*out, ProjectMeta{
			ID:                s.ID,
			Slug:              s.Slug,
			Title:             s.Title,
			ProjectType:       s.ProjectType,
			OverviewAvailable: s.ProjectType == ProjectTypeDisciplineMap && nonEmptyFile(filepath.Join(childDir, "map", "current.json")),
			LayoutStatus:      layoutStatus(childDir, &s),
		})
	}
	return nil
}

// SlugConflictError indicates a project with the same slug already exists.
type SlugConflictError struct {
	Slug string
}

func (e *SlugConflictError) Error() string {
	return "slug already exists: " + e.Slug
}

// IsSlugConflict reports whether err is a *SlugConflictError.
func IsSlugConflict(err error) bool {
	var sc *SlugConflictError
	return errors.As(err, &sc)
}

func defaultProjectMdBody() string {
	return defaultProjectMdBodyForInput(ProjectInput{})
}

// ProjectInput captures creation-time context. Why/Current/Target/Standard seed
// only system-learning project briefs; discipline-map briefs ignore them.
type ProjectInput struct {
	SystemPrompt  string // learner-owned conversation instructions
	ProjectType   ProjectType
	Why           string // motivation: why this topic matters
	Current       string // current ability self-assessment
	Target        string // target ability self-assessment
	Standard      string // completion criteria the learner commits to
	LearningScope *learningscope.Scope
}

func defaultProjectMdBodyForInput(in ProjectInput) string {
	if NormalizeProjectType(in.ProjectType) == ProjectTypeDisciplineMap {
		return `## 项目形态

学科地图

## 总览目标

建立这门学科的统一入口，了解学科边界、主要研究领域、领域关系、研究方法、典型应用与可选学习路线。

## 范围备注

_尚未填写：特别关注或暂不覆盖的方向。_

## 备注

_由学习者维护；智能体未经确认不得覆盖本文件。_`
	}

	why := strings.TrimSpace(in.Why)
	if why == "" && strings.TrimSpace(in.Current) == "" && strings.TrimSpace(in.Target) == "" && strings.TrimSpace(in.Standard) == "" {
		return "## 学习主题\n\n围绕项目标题学习。特殊要求可在教师页的附加信息中填写、修改或清空；可以不填。\n\n## 备注\n\n_由学习者维护；智能体未经确认不得覆盖本文件。_"
	}
	if why == "" {
		why = "_尚未填写：为什么这个主题值得学。_"
	}
	current := strings.TrimSpace(in.Current)
	if current == "" {
		current = "_尚未评估：当前在哪个水平。_"
	}
	target := strings.TrimSpace(in.Target)
	if target == "" {
		target = "_尚未设定：希望达到什么水平。_"
	}
	standard := strings.TrimSpace(in.Standard)
	if standard == "" {
		standard = "_尚未约定：怎样的成果算「完成」。_"
	}
	return fmt.Sprintf(`## Why this topic matters

%s

## Current ability

%s

## Target ability

%s

## Completion standard

%s

## Notes

_Learner-owned. Agents will not overwrite this file without confirmation._`, why, current, target, standard)
}

// FindProjectBySlug scans the flat project index.
func FindProjectBySlug(slug string) (ProjectMeta, string, bool) {
	all, err := IndexAll()
	if err != nil {
		return ProjectMeta{}, "", false
	}
	for _, p := range all {
		if p.Slug == slug {
			return p, "", true
		}
	}
	return ProjectMeta{}, "", false
}

// EnsureProjectsRoot creates PROJECTS_ROOT if missing.
func EnsureProjectsRoot() error {
	return os.MkdirAll(activeProjectsRoot(), 0o755)
}

// ProjectsRoot returns the effective projects root, honoring the test
// override. Every file-backed store under projects/ must resolve through it
// so tests can never touch the real workspace.
func ProjectsRoot() string { return activeProjectsRoot() }

// ProjectRootForSlug returns the absolute path to a project's folder for
// external callers (stores, launchers, etc.). Validates the slug.
func ProjectRootForSlug(slug string) (string, error) {
	return projectRoot(slug)
}

// projectsRootOverride is set by tests to redirect PROJECTS_ROOT to a temp
// directory. Production code never sets it (activeProjectsRoot falls back
// to filesystem.PROJECTS_ROOT when empty).
var projectsRootOverride string

// ProjectsRootForTest returns the current override (test helper).
func ProjectsRootForTest() string { return projectsRootOverride }

// SetProjectsRootForTest sets the override (test helper).
func SetProjectsRootForTest(dir string) { projectsRootOverride = dir }
