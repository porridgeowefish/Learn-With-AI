package generation

import (
	"encoding/json"
	"errors"
	"fmt"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/modules/projects"
)

type MapValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
}
type MapCandidate struct {
	Content          string `json:"content"`
	OverviewMarkdown string `json:"overviewMarkdown,omitempty"`
}

func mapIssue(code, message string) *MapValidationIssue {
	return &MapValidationIssue{Code: code, Message: message}
}

func decodeMapOutput(text string) (MapProjection, *MapValidationIssue) {
	var out MapProjection
	clean := cleanOutput(text)
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		issue := mapIssue("json-format", "模型返回的地图不是有效的 JSON 格式。")
		var fieldType *json.UnmarshalTypeError
		if errors.As(err, &fieldType) {
			issue = mapIssue("json-field-type", "地图 JSON 的字段类型不正确（例如数组字段返回了文本）。")
		}
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			prefix := clean[:min(len(clean), max(0, int(syntax.Offset)-1))]
			issue.Line = strings.Count(prefix, "\n") + 1
			last := strings.LastIndex(prefix, "\n")
			issue.Column = len([]rune(prefix[last+1:])) + 1
			issue.Message = fmt.Sprintf("模型返回的地图 JSON 格式有误（第 %d 行，第 %d 列）。", issue.Line, issue.Column)
		}
		return out, issue
	}
	return out, inspectNewMap(&out)
}

func candidatePath(root, id string) string {
	return filepath.Join(root, "generation", "candidates", id, "output.txt")
}

// Preserve only the completed visible response, never provider reasoning or
// prompt data. Sharing the generation lock prevents late failure writes after
// cancellation or project deletion. No additional model request is made.
func (s *Service) failMap(slug string, j Job, text string, issue *MapValidationIssue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := getJob(slug, j.ID)
	if err != nil || current.Status != "running" || s.deleted[slug] {
		return
	}
	root, err := projects.ProjectRootForSlug(slug)
	if err != nil {
		return
	}
	if _, err = projects.ReadProjectState(slug); err != nil {
		return
	}
	j.Validation = issue
	file := candidatePath(root, j.ID)
	if len(text) <= 4<<20 && os.MkdirAll(filepath.Dir(file), 0700) == nil && filesystem.AtomicWriteFile(file, []byte(text), 0600) == nil {
		j.CandidateAvailable = true
	} else {
		copy := *issue
		copy.Message += " 失败草稿保存未完成，请检查本地存储。"
		j.Validation = &copy
	}
	s.failLocked(slug, root, j, "invalid-map-output")
}

func (s *Service) Candidate(slug, id string) (MapCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := getJob(slug, id)
	if err != nil {
		return MapCandidate{}, err
	}
	if j.Kind != "discipline-map" || j.Status != "failed" || !j.CandidateAvailable || s.deleted[slug] {
		return MapCandidate{}, problem("candidate-unavailable", 404)
	}
	root, err := projects.ProjectRootForSlug(slug)
	if err != nil {
		return MapCandidate{}, err
	}
	f, err := os.Open(candidatePath(root, id))
	if err != nil {
		return MapCandidate{}, problem("candidate-unavailable", 404)
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return MapCandidate{}, problem("candidate-unavailable", 404)
	}
	candidate := MapCandidate{Content: string(body)}
	var out MapProjection
	if json.Unmarshal([]byte(cleanOutput(candidate.Content)), &out) == nil {
		candidate.OverviewMarkdown = out.OverviewMarkdown
	}
	return candidate, nil
}
