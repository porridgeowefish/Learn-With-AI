package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"reflect"
)

// MigrateCourseBindings runs under the workspace lease, before services start,
// or against an unpublished import candidate. It creates no backup files.
func MigrateCourseBindings(root string) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("course project link rejected: %s", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		if !ValidateSlug(slug) {
			continue
		}
		if err := migrateCourseBinding(root, slug); err != nil {
			return fmt.Errorf("课程关联迁移失败 [%s]: %w", slug, err)
		}
	}
	return nil
}

// Check each path component so migration never writes through a project link.
func courseMigrationPath(root, relative string) (string, error) {
	if !filepath.IsLocal(relative) {
		return "", errors.New("invalid course path")
	}
	for p := relative; p != "."; p = filepath.Dir(p) {
		info, err := os.Lstat(filepath.Join(root, p))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("course link rejected")
		}
	}
	return filepath.Join(root, relative), nil
}
func readMigrationJSON(root, relative string, out any) error {
	p, err := courseMigrationPath(root, relative)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func migrateCourseBinding(root, slug string) error {
	var state ProjectState
	err := readMigrationJSON(root, filepath.Join(slug, "state.json"), &state)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.ProjectType != ProjectTypeSystemLearning {
		return nil
	}
	p, err := courseMigrationPath(root, filepath.Join(slug, "learning-scope.json"))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var copied Scope
	if err = json.Unmarshal(b, &copied); err != nil {
		return err
	}
	if copied.Source.Type != SourceDisciplineMap {
		return nil
	}
	if copied.SchemaVersion > SchemaVersion {
		return errors.New("unsupported learning scope version")
	}
	if copied.Source.BindingVersion > 1 {
		return errors.New("unsupported course binding version")
	}
	if copied.Source.BindingVersion == 1 {
		if copied.Source.BindingStatus != "linked" && copied.Source.BindingStatus != "unresolved" {
			return errors.New("invalid course binding status")
		}
		if copied.Source.BindingStatus == "linked" && (copied.Source.MapID == "" || copied.Source.MapVersionID == "" || copied.Source.TopicID == "") {
			return errors.New("incomplete linked course binding")
		}
		return nil
	}
	source, err := resolveLegacyCourseBinding(root, state, copied)
	if err != nil {
		return err
	}
	source.BindingVersion = 1
	source.BindingStatus = "unresolved"
	if source.MapID != "" && source.MapVersionID != "" {
		source.BindingStatus = "linked"
	}
	return storeCourseBinding(p, b, source)
}

func resolveLegacyCourseBinding(root string, member ProjectState, copied Scope) (Source, error) {
	source := copied.Source
	unresolved := source
	// Preserve legacy identifiers in the document unless a match is proven.
	// The caller must not infer success from those legacy identifiers.
	unresolved.MapID = ""
	if !ValidateSlug(source.MapSlug) {
		return unresolved, nil
	}
	var parent ProjectState
	err := readMigrationJSON(root, filepath.Join(source.MapSlug, "state.json"), &parent)
	if errors.Is(err, os.ErrNotExist) {
		return unresolved, nil
	}
	if err != nil {
		return Source{}, err
	}
	if parent.ProjectType != ProjectTypeDisciplineMap || parent.ID == "" || (source.MapID != "" && source.MapID != parent.ID) {
		return unresolved, nil
	}
	complete := source.MapID != "" && source.MapVersionID != ""
	if !complete && (parent.CreatedAt.IsZero() || member.CreatedAt.IsZero() || parent.CreatedAt.After(member.CreatedAt)) {
		return unresolved, nil
	}
	dir, err := courseMigrationPath(root, filepath.Join(source.MapSlug, "map", "versions"))
	if err != nil {
		return Source{}, err
	}
	versions, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return unresolved, nil
	}
	if err != nil {
		return Source{}, err
	}
	match := ""
	for _, version := range versions {
		if !version.IsDir() || !validMapVersion(version.Name()) || (source.MapVersionID != "" && source.MapVersionID != version.Name()) {
			continue
		}
		for _, name := range []string{"manifest.json", "overview.md", "topics.json"} {
			if _, err = courseMigrationPath(root, filepath.Join(source.MapSlug, "map", "versions", version.Name(), name)); err != nil {
				return Source{}, err
			}
		}
		_, _, data, err := ReadMapVersion(filepath.Join(root, source.MapSlug), version.Name())
		if err != nil {
			return Source{}, err
		}
		var catalog Catalog
		if err = json.Unmarshal(data, &catalog); err != nil {
			return Source{}, err
		}
		topic, ok := FindTopic(catalog, source.TopicID)
		if !ok {
			continue
		}
		if !complete {
			expected := SnapshotFromTopic(source.MapSlug, topic, member.CreatedAt)
			expected.Source, expected.UpdatedAt = copied.Source, copied.UpdatedAt
			if !reflect.DeepEqual(expected, copied) {
				continue
			}
		}
		if match != "" {
			return unresolved, nil
		}
		match = version.Name()
	}
	if match == "" {
		return unresolved, nil
	}
	source.MapID, source.MapVersionID = parent.ID, match
	return source, nil
}

// Patch only association keys, retaining all unknown and unrelated JSON fields.
func storeCourseBinding(path string, original []byte, source Source) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(original, &doc); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(doc["source"], &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("missing course source")
	}
	for k, v := range map[string]any{"bindingVersion": 1, "bindingStatus": source.BindingStatus} {
		fields[k], _ = json.Marshal(v)
	}
	if source.BindingStatus == "linked" {
		fields["mapId"], _ = json.Marshal(source.MapID)
		fields["mapVersionId"], _ = json.Marshal(source.MapVersionID)
	}
	doc["source"], _ = json.Marshal(fields)
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(path, append(b, '\n'), info.Mode().Perm())
}

// ConfirmCourseBinding is an explicit learner choice, not automatic guessing.
func ConfirmCourseBinding(slug, mapSlug, version, topicID string) error {
	root, err := ProjectRootForSlug(slug)
	if err != nil {
		return err
	}
	mu := MapLock(root)
	mu.Lock()
	defer mu.Unlock()
	source, err := CourseSource(slug)
	if err != nil {
		return err
	}
	if source.Type != SourceDisciplineMap || source.BindingStatus != "unresolved" || source.MapSlug != mapSlug || source.TopicID != topicID {
		return errors.New("course binding is not awaiting this association")
	}
	course, err := ReadCourseMap(mapSlug, version)
	if err != nil {
		return err
	}
	if _, ok := FindTopic(course.Topics, topicID); !ok {
		return errors.New("course topic unavailable")
	}
	path, err := courseMigrationPath(filepath.Dir(root), filepath.Join(slug, "learning-scope.json"))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	source.MapID, source.MapVersionID, source.BindingStatus = course.ID, version, "linked"
	return storeCourseBinding(path, b, source)
}
