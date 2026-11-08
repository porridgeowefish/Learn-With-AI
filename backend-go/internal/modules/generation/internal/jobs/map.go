package generation

import (
	"encoding/json"
	"errors"
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
)

type MapProjection struct {
	MapVersionID     string           `json:"mapVersionId"`
	Format           string           `json:"format"`
	OverviewMarkdown string           `json:"overviewMarkdown"`
	Topics           projects.Catalog `json:"topics"`
	Coverage         Coverage         `json:"coverage"`
}
type mapManifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	VersionID     string    `json:"versionId"`
	JobID         string    `json:"jobId,omitempty"`
	Format        string    `json:"format"`
	OverviewHash  string    `json:"overviewHash"`
	TopicsHash    string    `json:"topicsHash"`
	Coverage      Coverage  `json:"coverage"`
	CreatedAt     time.Time `json:"createdAt"`
}
type mapPointer struct {
	SchemaVersion int    `json:"schemaVersion"`
	VersionID     string `json:"versionId"`
}

func mapLock(root string) *sync.Mutex { return projects.MapLock(root) }
func ReadMap(slug, version string) (MapProjection, error) {
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return MapProjection{}, e
	}
	state, e := projects.ReadProjectState(slug)
	if e != nil {
		return MapProjection{}, e
	}
	if state.ProjectType != projects.ProjectTypeDisciplineMap {
		return MapProjection{}, problem("map-project-type", 400)
	}
	mu := mapLock(root)
	mu.Lock()
	defer mu.Unlock()
	if version != "" && !safeID(version) {
		return MapProjection{}, problem("invalid-map-version", 400)
	}
	return readMapLocked(root, version)
}
func readMapLocked(root, version string) (MapProjection, error) {
	if version == "" {
		var pointer mapPointer
		e := readJSON(filepath.Join(root, "map", "current.json"), &pointer)
		if errors.Is(e, os.ErrNotExist) {
			return MapProjection{Format: "structured", Topics: projects.EmptyCatalog()}, nil
		}
		if e != nil || pointer.SchemaVersion != 1 || !safeID(pointer.VersionID) {
			return MapProjection{}, problem("map-recovery-required", 409)
		}
		version = pointer.VersionID
	}
	manifest, overview, topics, e := projects.ReadMapVersion(root, version)
	if e != nil {
		return MapProjection{}, problem("map-recovery-required", 409)
	}
	var coverage Coverage
	if len(manifest.Coverage) > 0 && json.Unmarshal(manifest.Coverage, &coverage) != nil {
		return MapProjection{}, problem("map-recovery-required", 409)
	}
	var catalog projects.Catalog
	if json.Unmarshal(topics, &catalog) != nil {
		catalog = projects.EmptyCatalog()
	}
	return MapProjection{MapVersionID: version, Format: manifest.Format, OverviewMarkdown: string(overview), Topics: catalog, Coverage: coverage}, nil
}
func writeMapVersion(root, id, job, format string, overview, topics []byte, coverage Coverage) error {
	b, e := json.Marshal(coverage)
	if e != nil {
		return e
	}
	return projects.WriteMapVersion(root, id, job, format, overview, topics, b)
}
func publishMapLocked(slug, job, base string, out MapProjection) (string, error) {
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return "", e
	}
	if e = validateNewMap(&out); e != nil {
		return "", e
	}
	id := idgen.New("mapv")
	out.Topics.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	topics, e := json.Marshal(out.Topics)
	if e != nil {
		return "", e
	}
	coverage, e := json.Marshal(out.Coverage)
	if e != nil {
		return "", e
	}
	if e = projects.PublishCourseMap(root, base, id, job, "structured", []byte(out.OverviewMarkdown), topics, coverage); e != nil {
		if e.Error() == "map-version-conflict" {
			return "", problem("map-version-conflict", 409)
		}
		return "", e
	}
	return id, nil
}
func mapResultForJob(slug, job string) ([]string, error) {
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return nil, e
	}
	mu := mapLock(root)
	mu.Lock()
	defer mu.Unlock()
	var ptr mapPointer
	if e = readJSON(filepath.Join(root, "map", "current.json"), &ptr); errors.Is(e, os.ErrNotExist) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	dir := filepath.Join(root, "map", "versions", ptr.VersionID)
	var m mapManifest
	if e = readJSON(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return nil, e
	}
	if m.JobID == job {
		if _, e = readMapLocked(root, ptr.VersionID); e != nil {
			return nil, e
		}
		return []string{ptr.VersionID}, nil
	}
	return nil, nil
}
func validateNewMap(out *MapProjection) error {
	if inspectNewMap(out) != nil {
		return problem("invalid-map-output", 409)
	}
	return nil
}
func inspectNewMap(out *MapProjection) *MapValidationIssue {
	if err := projects.ValidateCatalog(&out.Topics); err != nil {
		messages := map[string]string{
			"unsupported_discipline_topics_schema": "主题目录的版本字段不正确。",
			"discipline_topics_too_large":          "主题目录超过 500 个小节。",
			"invalid_topic_priority":               "小节优先级不是 I、II 或 III。",
			"invalid_discipline_topic":             "小节的必填字段缺失或长度不符合要求。",
			"duplicate_discipline_topic":           "主题目录包含重复的小节名称或标识。",
			"duplicate_owned_concept":              "不同小节重复声明了同一个核心概念的归属。",
		}
		message := messages[err.Error()]
		if message == "" {
			message = "主题目录字段未通过校验。"
		}
		return mapIssue(err.Error(), message)
	}
	required := []string{"这门学科研究什么", "学科边界", "主要研究领域与知识架构", "领域之间如何关联", "常用研究方法与证据", "典型应用"}
	headings := []string{}
	chapter := ""
	chapters := map[string]int{}
	topicIndex := 0
	inside := false
	for _, line := range structuralLines(out.OverviewMarkdown) {
		if strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			headings = append(headings, title)
			inside = title == "主要研究领域与知识架构"
		}
		if strings.HasPrefix(line, "### ") {
			if !inside {
				return mapIssue("chapter-position", "章节标题出现在知识架构区域之外。")
			}
			chapter = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if _, ok := chapters[chapter]; ok {
				return mapIssue("duplicate-chapter", "地图包含重复的章节标题。")
			}
			chapters[chapter] = 0
		}
		if strings.HasPrefix(line, "#### ") {
			if !inside || chapter == "" {
				return mapIssue("topic-position", "小节标题没有位于知识架构的章节下。")
			}
			if topicIndex >= len(out.Topics.Topics) {
				return mapIssue("topic-count", "正文小节数量多于主题目录。")
			}
			topic := out.Topics.Topics[topicIndex]
			if topic.Priority == "" {
				return mapIssue("missing-priority", "主题目录有小节缺少优先级。")
			}
			if topic.TeachingOutline == "" {
				return mapIssue("missing-teaching-outline", "主题目录有小节缺少教学次序。")
			}
			if topic.ChapterTitle != chapter {
				return mapIssue("chapter-mismatch", "正文与主题目录的小节章节归属不一致。")
			}
			if topic.Title != strings.TrimSpace(strings.TrimPrefix(line, "#### ")) {
				return mapIssue("topic-title-mismatch", "正文小节的名称或顺序与主题目录不一致。")
			}
			chapters[chapter]++
			topicIndex++
		}
	}
	if len(headings) != len(required) {
		return mapIssue("section-count", "地图的六个总览部分不完整或包含额外部分。")
	}
	for i, title := range required {
		if headings[i] != title {
			return mapIssue("section-order", "地图总览部分的名称或顺序不符合要求。")
		}
	}
	if len(chapters) < 4 || len(chapters) > 8 {
		return mapIssue("chapter-count", "地图章节数量不符合要求（应为 4–8 章）。")
	}
	if topicIndex != len(out.Topics.Topics) {
		return mapIssue("topic-count", "正文小节数量少于主题目录。")
	}
	for _, n := range chapters {
		if n < 2 || n > 6 {
			return mapIssue("chapter-topic-count", "有章节的小节数量不符合要求（每章应为 2–6 节）。")
		}
	}
	return nil
}
