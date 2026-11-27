package projects

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

type LearningTask struct {
	ID            string `json:"id"`
	TopicID       string `json:"topicId,omitempty"`
	TopicTitle    string `json:"topicTitle"`
	Goal          string `json:"goal,omitempty"`
	ReviewTask    string `json:"reviewTask,omitempty"`
	Status        string `json:"status"`
	AddedAt       string `json:"addedAt"`
	StartedAt     string `json:"startedAt,omitempty"`
	CompletedAt   string `json:"completedAt,omitempty"`
	ScheduledDate string `json:"scheduledDate,omitempty"`
}

type LearningPlan struct {
	SchemaVersion int            `json:"schemaVersion"`
	Items         []LearningTask `json:"items"`
	UpdatedAt     string         `json:"updatedAt"`
}

type LearningSchedule struct {
	StartDate string `json:"startDate"`
	Weekdays  []int  `json:"weekdays"`
}

var learningPlanLocks sync.Map

func LearningPlanLock(root string) *sync.Mutex {
	value, _ := learningPlanLocks.LoadOrStore(root, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func ReadLearningPlan(path string) (LearningPlan, error) {
	plan := LearningPlan{SchemaVersion: 1, Items: []LearningTask{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		return plan, err
	}
	if plan.Items == nil {
		plan.Items = []LearningTask{}
	}
	return plan, nil
}

func (schedule LearningSchedule) Valid() bool {
	if _, err := time.Parse(time.DateOnly, schedule.StartDate); err != nil {
		return false
	}
	if len(schedule.Weekdays) < 1 || len(schedule.Weekdays) > 7 {
		return false
	}
	for _, day := range schedule.Weekdays {
		if day < 0 || day > 6 {
			return false
		}
	}
	return true
}

func EnsureScheduledLearningPlan(root string, topics []Topic, schedule LearningSchedule) error {
	if !schedule.Valid() {
		return errors.New("invalid learning schedule")
	}
	lock := LearningPlanLock(root)
	lock.Lock()
	defer lock.Unlock()
	path := filepath.Join(root, "learning-plan.json")
	plan, err := ReadLearningPlan(path)
	if err != nil || len(plan.Items) != 0 {
		return err
	}
	cursor, _ := time.Parse(time.DateOnly, schedule.StartDate)
	var days [7]bool
	for _, day := range schedule.Weekdays {
		days[day] = true
	}
	now := time.Now().UTC().Format(time.RFC3339)
	usedTitles := make(map[string]bool)
	for _, topic := range topics {
		if usedTitles[topic.Title] {
			continue
		}
		usedTitles[topic.Title] = true
		for !days[cursor.Weekday()] {
			cursor = cursor.AddDate(0, 0, 1)
		}
		plan.Items = append(plan.Items, LearningTask{
			ID: topic.ID, TopicID: topic.ID, TopicTitle: topic.Title, Status: "planned", AddedAt: now, ScheduledDate: cursor.Format(time.DateOnly),
		})
		cursor = cursor.AddDate(0, 0, 1)
		if len(plan.Items) == 200 {
			break
		}
	}
	plan.UpdatedAt = now
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(path, raw, 0o644)
}
