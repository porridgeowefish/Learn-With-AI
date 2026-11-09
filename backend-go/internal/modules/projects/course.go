package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Course is the immutable map version to which a learning project belongs.
type Course struct {
	ID               string  `json:"id"`
	MapSlug          string  `json:"mapSlug"`
	MapVersionID     string  `json:"mapVersionId"`
	OverviewMarkdown string  `json:"overviewMarkdown"`
	Topics           Catalog `json:"topics"`
}
type CourseMember struct {
	Slug         string
	Title        string
	TopicID      string
	MapID        string
	MapVersionID string
}

// ReadCourseMap verifies the persisted immutable version without applying newer
// generation-time schema restrictions to historical catalogs.
func ReadCourseMap(slug, version string) (Course, error) {
	if !validMapVersion(version) {
		return Course{}, errors.New("invalid-map-version")
	}
	root, err := ProjectRootForSlug(slug)
	if err != nil {
		return Course{}, err
	}
	state, err := ReadProjectState(slug)
	if err != nil {
		return Course{}, err
	}
	if state.ProjectType != ProjectTypeDisciplineMap {
		return Course{}, errors.New("map-project-type")
	}
	mu := MapLock(root)
	mu.Lock()
	defer mu.Unlock()
	_, overview, topics, err := ReadMapVersion(root, version)
	if err != nil {
		return Course{}, err
	}
	var catalog Catalog
	if err = json.Unmarshal(topics, &catalog); err != nil {
		return Course{}, err
	}
	return Course{state.ID, slug, version, string(overview), catalog}, nil
}

func projectCourseSource(slug string) (Source, error) {
	root, err := ProjectRootForSlug(slug)
	if err != nil {
		return Source{}, err
	}
	b, err := readCourseFile(filepath.Join(root, "learning-scope.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Source{}, nil
	}
	if err != nil {
		return Source{}, err
	}
	var scope Scope
	if err = json.Unmarshal(b, &scope); err != nil {
		return Source{}, err
	}
	return scope.Source, nil
}
func CourseForProject(slug string) (Course, error) {
	source, err := CourseSource(slug)
	if err != nil {
		return Course{}, err
	}
	// Unresolved legacy snapshots remain usable as standalone
	// teaching scopes, but cannot safely claim continuity with a recreated map.
	if source.Type != SourceDisciplineMap || source.BindingStatus == "unresolved" || source.MapVersionID == "" || source.MapID == "" {
		return Course{}, nil
	}
	course, err := ReadCourseMap(source.MapSlug, source.MapVersionID)
	if err != nil {
		return Course{}, err
	}
	if source.MapID != "" && source.MapID != course.ID {
		return Course{}, errors.New("course-map-identity-mismatch")
	}
	if _, ok := FindTopic(course.Topics, source.TopicID); !ok {
		return Course{}, errors.New("course-topic-unavailable")
	}
	return course, nil
}

// CourseMembers reads only project metadata and scope bindings, never chats.
func CourseMembers(slug string) ([]CourseMember, error) {
	course, err := CourseForProject(slug)
	if err != nil || course.ID == "" {
		return nil, err
	}
	all, err := indexedCourseProjects(slug)
	if err != nil {
		return nil, err
	}
	members := []CourseMember{}
	counts := map[string]int{}
	for _, p := range all {
		source, e := CourseSource(p.Slug)
		if e != nil {
			continue
		}
		if source.Type != SourceDisciplineMap || source.BindingStatus == "unresolved" || source.MapSlug != course.MapSlug || source.MapVersionID != course.MapVersionID || source.MapID != course.ID {
			continue
		}
		if _, ok := FindTopic(course.Topics, source.TopicID); !ok {
			continue
		}
		members = append(members, CourseMember{p.Slug, p.Title, source.TopicID, course.ID, course.MapVersionID})
		counts[source.TopicID]++
	}
	result := members[:0]
	for _, m := range members {
		if counts[m.TopicID] == 1 {
			result = append(result, m)
		}
	}
	return result, nil
}

// CourseSource reads the persisted binding. Compatibility belongs to migration.
func CourseSource(slug string) (Source, error) {
	source, err := projectCourseSource(slug)
	if source.BindingVersion > 1 {
		return Source{}, errors.New("unsupported course binding version")
	}
	return source, err
}
