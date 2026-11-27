package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

type studyUnit struct {
	Title      string `json:"title"`
	Goal       string `json:"goal"`
	ReviewTask string `json:"reviewTask"`
	Date       string `json:"date"`
}

type studyCourse struct {
	Title     string      `json:"title"`
	StartDate string      `json:"startDate"`
	Weekdays  []int       `json:"weekdays"`
	Units     []studyUnit `json:"units"`
}

type studyEvent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	Course    string `json:"course,omitempty"`
	Slug      string `json:"slug,omitempty"`
	TopicID   string `json:"topicId,omitempty"`
	Completed bool   `json:"completed"`
}

func studyLocation(name string) *time.Location {
	if name != "" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
	}
	return time.Local
}

func (c *studyCourse) prepare(today time.Time) error {
	c.Title = strings.TrimSpace(strings.Split(strings.ReplaceAll(c.Title, "：", ":"), ":")[0])
	if !validStudyTitle(c.Title) || len(c.Units) == 0 || len(c.Units) > 40 {
		return errors.New("学习方案需要一个主题和 1 至 40 个单元。")
	}
	cursor, err := time.Parse(time.DateOnly, c.StartDate)
	if err != nil || len(c.Weekdays) == 0 || len(c.Weekdays) > 7 {
		return errors.New("请选择有效的开始日期和学习日。")
	}
	if c.StartDate > today.AddDate(2, 0, 0).Format(time.DateOnly) {
		return errors.New("学习开始日期超出两年。")
	}
	for _, weekday := range c.Weekdays {
		if weekday < 0 || weekday > 6 {
			return errors.New("请选择有效的学习日。")
		}
	}
	titles := map[string]bool{}
	for i := range c.Units {
		u := &c.Units[i]
		u.Title, u.Goal, u.ReviewTask = strings.TrimSpace(u.Title), strings.TrimSpace(u.Goal), strings.TrimSpace(u.ReviewTask)
		if !validStudyTitle(u.Title) || titles[u.Title] || len(u.Goal) == 0 || len(u.Goal) > 600 ||
			len(u.ReviewTask) == 0 || len(u.ReviewTask) > 600 {
			return errors.New("学习方案需要不同的单元标题、具体目标和复习内容。")
		}
		titles[u.Title] = true
		for !slices.Contains(c.Weekdays, int(cursor.Weekday())) {
			cursor = cursor.AddDate(0, 0, 1)
		}
		u.Date = cursor.Format(time.DateOnly)
		cursor = cursor.AddDate(0, 0, 1)
	}
	return nil
}

func validStudyTitle(title string) bool {
	return title != "" && utf8.RuneCountInString(title) <= 60 && !strings.ContainsAny(title, "\r\n#*_\x60<>")
}

var studyOperationID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func (s *Server) handleConfirmStudyCourse(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OperationID string      `json:"operationId"`
		TimeZone    string      `json:"timeZone"`
		Course      studyCourse `json:"course"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil || !studyOperationID.MatchString(in.OperationID) {
		httpx.Error(w, http.StatusBadRequest, "请提供待确认的学习方案。")
		return
	}
	now := time.Now().In(studyLocation(in.TimeZone))
	if err := in.Course.prepare(now); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, _ := json.Marshal(in.Course)
	digest := sha256.Sum256(payload)
	slug := "study-" + in.OperationID
	result := map[string]any{"slug": slug, "title": in.Course.Title, "planned": len(in.Course.Units)}
	s.projectTreeMu.Lock()
	defer s.projectTreeMu.Unlock()
	exists, err := projects.ProjectExists(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "暂时无法核对学习主题。")
		return
	}
	if exists {
		root, err := projects.ProjectRootForSlug(slug)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "暂时无法打开学习主题。")
			return
		}
		manifest, _, _, err := projects.ReadMapVersion(root, "confirmed")
		if err != nil || manifest.JobID != fmt.Sprintf("%x", digest[:]) {
			httpx.Error(w, http.StatusConflict, "这份确认记录已用于另一份学习方案。")
			return
		}
		s.cache.Invalidate(slug)
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	if err := publishStudyCourse(slug, in.Course, fmt.Sprintf("%x", digest[:]), now); err != nil {
		log.Printf("publish study course %s: %v", slug, err)
		httpx.Error(w, http.StatusInternalServerError, "学习主题和日历未能保存，请重试。")
		return
	}
	s.cache.Invalidate(slug)
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func publishStudyCourse(slug string, course studyCourse, digest string, now time.Time) error {
	catalog := projects.Catalog{SchemaVersion: 1, UpdatedAt: now.UTC().Format(time.RFC3339)}
	var overview strings.Builder
	fmt.Fprintf(&overview, "# %s\n\n## 课程内容\n\n### 学习单元\n", course.Title)
	for i, unit := range course.Units {
		topic := projects.Topic{
			ID: fmt.Sprintf("unit-%d", i+1), Title: unit.Title, ChapterTitle: "学习单元",
			Goal: unit.Goal, TeachingOutline: unit.Goal,
			InScope: []string{unit.Goal}, OwnedConcepts: []string{unit.Title},
		}
		fmt.Fprintf(&overview, "\n#### %s\n\n%s\n", unit.Title, strings.Join(strings.Fields(unit.Goal), " "))
		catalog.Topics = append(catalog.Topics, topic)
	}
	if err := projects.ValidateCatalog(&catalog); err != nil {
		return err
	}
	if err := projects.ValidateAgainstOverview(catalog, overview.String()); err != nil {
		return err
	}
	topics, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	if err := projects.EnsureProjectsRoot(); err != nil {
		return err
	}
	parent := projects.ProjectsRoot()
	stage, err := os.MkdirTemp(parent, ".study-course-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := projects.StageDisciplineProject(stage, slug, course.Title); err != nil {
		return err
	}
	plan := projects.LearningPlan{SchemaVersion: 1, UpdatedAt: catalog.UpdatedAt}
	for index, topic := range catalog.Topics {
		plan.Items = append(plan.Items, projects.LearningTask{
			ID: topic.ID, TopicID: topic.ID, TopicTitle: topic.Title, Status: "planned",
			AddedAt: catalog.UpdatedAt, ScheduledDate: course.Units[index].Date,
			Goal: topic.Goal, ReviewTask: course.Units[index].ReviewTask,
		})
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := filesystem.AtomicWriteFile(filepath.Join(stage, "learning-plan.json"), raw, 0600); err != nil {
		return err
	}
	if err := projects.PublishCourseMap(stage, "", "confirmed", digest, "structured", []byte(overview.String()), topics, nil); err != nil {
		return err
	}
	return filesystem.PublishDirectory(stage, filepath.Join(parent, slug))
}

func (s *Server) handleStudyCalendar(w http.ResponseWriter, r *http.Request) {
	now := time.Now().In(studyLocation(r.URL.Query().Get("timeZone")))
	items, err := s.studyCalendar(now)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "暂时无法读取学习日历。")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"today": now.Format(time.DateOnly), "items": items})
}

var reviewResultsLock sync.Mutex

func readReviewResults() (map[string]string, error) {
	results := make(map[string]string)
	raw, err := os.ReadFile(filepath.Join(filesystem.WORKSPACE, "learning-review-results.json"))
	if errors.Is(err, os.ErrNotExist) {
		return results, nil
	}
	if err != nil {
		return nil, err
	}
	return results, json.Unmarshal(raw, &results)
}

func saveCompletedReview(event studyEvent, when time.Time) error {
	reviewResultsLock.Lock()
	defer reviewResultsLock.Unlock()
	results, err := readReviewResults()
	if err != nil {
		return err
	}
	if results[event.ID] != "" {
		return nil
	}
	results[event.ID] = when.Format(time.DateOnly)
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(filepath.Join(filesystem.WORKSPACE, "learning-review-results.json"), raw, 0600)
}

func (s *Server) studyCalendar(now time.Time) ([]studyEvent, error) {
	all, err := projects.IndexAll()
	if err != nil {
		return nil, err
	}
	results, err := readReviewResults()
	if err != nil {
		return nil, err
	}
	completions := map[string]time.Time{}
	for _, p := range all {
		if p.ProjectType != projects.ProjectTypeSystemLearning {
			continue
		}
		source, err := projects.CourseSource(p.Slug)
		if err != nil || source.MapID == "" || source.BindingStatus == "unresolved" {
			continue
		}
		summaries, err := teacher.TeachingSummaries(p.Slug)
		if err != nil {
			continue
		}
		key := source.MapID + "/" + source.MapVersionID + "/" + source.TopicID
		for _, summary := range summaries {
			if summary.Status != "revoked" && summary.Status != "superseded" &&
				(completions[key].IsZero() || summary.CreatedAt.Before(completions[key])) {
				completions[key] = summary.CreatedAt
			}
		}
	}
	items := []studyEvent{}
	for _, p := range all {
		if p.ProjectType != projects.ProjectTypeDisciplineMap {
			continue
		}
		root, err := projects.ProjectRootForSlug(p.Slug)
		if err != nil {
			continue
		}
		plan, err := projects.ReadLearningPlan(filepath.Join(root, "learning-plan.json"))
		if err != nil {
			return nil, err
		}
		if len(plan.Items) == 0 {
			continue
		}
		projection, err := generation.ReadMap(p.Slug, "")
		if err != nil {
			continue
		}
		for _, unit := range plan.Items {
			key := p.ID + "/" + projection.MapVersionID + "/" + unit.TopicID
			completed := completions[key]
			if completed.IsZero() {
				completed, _ = time.Parse(time.RFC3339Nano, unit.CompletedAt)
			}
			event := studyEvent{ID: key + "/" + unit.ID, Kind: "study", Date: unit.ScheduledDate,
				Title: unit.TopicTitle, Detail: unit.Goal, Course: p.Title, Slug: p.Slug,
				TopicID: unit.TopicID, Completed: !completed.IsZero()}
			if event.Date != "" {
				items = append(items, event)
			}
			if completed.IsZero() {
				continue
			}
			task := unit.ReviewTask
			if task == "" {
				task = "先回忆「" + unit.TopicTitle + "」的关键判断，再用一个新场景解释它。"
			}
			for _, days := range []int{1, 3, 7} {
				review := event
				review.ID = fmt.Sprintf("%s/review/%d", event.ID, days)
				review.Kind = "review"
				review.Date = completed.In(now.Location()).AddDate(0, 0, days).Format(time.DateOnly)
				review.Detail = task
				review.Completed = results[review.ID] != ""
				items = append(items, review)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date < items[j].Date
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *Server) handleOpenStudyUnit(w http.ResponseWriter, r *http.Request) {
	s.projectTreeMu.Lock()
	defer s.projectTreeMu.Unlock()
	source := learningScopeSourceRequest{
		Type: projects.SourceDisciplineMap, MapSlug: r.PathValue("id"), TopicID: r.PathValue("topicId"),
	}
	scope, err := resolveMapLearningScope(source)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "找不到这个学习单元。")
		return
	}
	all, err := projects.IndexAll()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "暂时无法打开课堂。")
		return
	}
	for _, p := range all {
		if p.ProjectType != projects.ProjectTypeSystemLearning {
			continue
		}
		if existing, e := projects.CourseSource(p.Slug); e == nil &&
			existing.MapID == scope.Source.MapID && existing.MapVersionID == scope.Source.MapVersionID &&
			existing.TopicID == scope.Source.TopicID && existing.BindingStatus != "unresolved" {
			httpx.WriteJSON(w, http.StatusOK, map[string]string{"slug": p.Slug})
			return
		}
	}
	key := sha256.Sum256([]byte(scope.Source.MapID + "/" + scope.Source.MapVersionID + "/" + scope.Source.TopicID))
	slug := fmt.Sprintf("lesson-%x", key[:12])
	_, err = s.projectOperations().Create(slug, scope.Title, projects.ProjectInput{
		ProjectType: projects.ProjectTypeSystemLearning, LearningScope: &scope,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "暂时无法打开课堂。")
		return
	}
	s.cache.Invalidate(slug)
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"slug": slug})
}
