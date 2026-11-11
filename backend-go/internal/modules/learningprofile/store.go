// Package learningprofile owns opt-in learner memory and teaching controls.
// It never writes the learner-owned preferences.md or exports personal memory.
package learningprofile

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/platform/workspacegate"
)

type SearchSettings struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	ProviderID string `json:"providerId"`
	APIKey     string `json:"apiKey,omitempty"`
	HasKey     bool   `json:"hasKey"`
}
type Settings struct {
	Length        string         `json:"length"`
	Effort        string         `json:"effort"`
	ShowReasoning bool           `json:"showReasoning"`
	MemoryEnabled bool           `json:"memoryEnabled"`
	Search        SearchSettings `json:"search"`
}
type TurnOptions struct {
	Effort    string `json:"effort"`
	WebSearch bool   `json:"webSearch"`
}
type Memory struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Text          string `json:"text"`
	Project       string `json:"project"`
	SourceProject string `json:"sourceProject"`
	MessageID     string `json:"messageId"`
	Evidence      string `json:"evidence"`
	UpdatedAt     string `json:"updatedAt"`
	Edited        bool   `json:"edited"`
}
type State struct {
	MemoryEpoch   uint64          `json:"memoryEpoch"`
	SchemaVersion int             `json:"schemaVersion"`
	Revision      uint64          `json:"revision"`
	Settings      Settings        `json:"settings"`
	Memories      []Memory        `json:"memories"`
	Forgotten     map[string]bool `json:"forgotten"`
	Processed     map[string]bool `json:"processed"`
	LastError     string          `json:"lastError,omitempty"`
}
type Candidate struct {
	Key         string `json:"key"`
	Text        string `json:"text"`
	Evidence    string `json:"evidence"`
	ProjectOnly bool   `json:"projectOnly"`
}

var mu sync.Mutex
var extraction sync.Mutex
var slots = make(chan struct{}, 8)
var activeCancel context.CancelFunc

func read() (State, error) {
	st := State{SchemaVersion: 1, Settings: Settings{Length: "medium", Effort: "medium", ShowReasoning: true}, Memories: []Memory{}, Forgotten: map[string]bool{}, Processed: map[string]bool{}}
	data, e := os.ReadFile(filepath.Join(paths.WORKSPACE, "learning-profile.json"))
	if errors.Is(e, os.ErrNotExist) {
		return st, nil
	}
	if e != nil {
		return st, e
	}
	if len(data) > 4<<20 {
		return st, errors.New("学习记忆文件超出容量")
	}
	e = json.Unmarshal(data, &st)
	if e == nil && st.SchemaVersion != 1 {
		e = errors.New("不支持的学习记忆格式")
	}
	if st.Forgotten == nil {
		st.Forgotten = map[string]bool{}
	}
	if st.Processed == nil {
		st.Processed = map[string]bool{}
	}
	return st, e
}
func Read() (State, error) {
	mu.Lock()
	defer mu.Unlock()
	st, err := read()
	if err == nil && st.Settings.Search.Provider == "" {
		if p := PlanProvider(""); p != nil {
			st.Settings.Search = SearchSettings{Enabled: true, Provider: "zhipu-plan", ProviderID: p.ID, HasKey: true}
		}
	}
	return st, err
}

// PlanProvider only recognizes official domestic Coding Plan endpoints.
func PlanProvider(id string) *ai.ProviderConfig {
	cfg, err := ai.LoadConfig()
	if err != nil || cfg == nil {
		return nil
	}
	for _, p := range cfg.Providers {
		u, e := url.Parse(p.BaseURL)
		if e == nil && u.Scheme == "https" && u.Host == "open.bigmodel.cn" && (strings.HasPrefix(u.Path, "/api/coding/") || strings.TrimRight(u.Path, "/") == "/api/anthropic") && p.APIKey != "" && (id == "" || id == p.ID) {
			return &p
		}
	}
	return nil
}
func write(st State) error {
	st.Revision++
	data, e := json.MarshalIndent(st, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(paths.WORKSPACE, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(paths.WORKSPACE, ".learning-profile-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(paths.WORKSPACE, "learning-profile.json"))
}
func SaveSettings(cfg Settings) error {
	if cfg.Length != "short" && cfg.Length != "medium" && cfg.Length != "long" {
		return errors.New("请选择回复长度")
	}
	if cfg.Effort != "low" && cfg.Effort != "medium" && cfg.Effort != "high" && cfg.Effort != "xhigh" {
		return errors.New("请选择有效思考强度")
	}
	mu.Lock()
	defer mu.Unlock()
	st, e := read()
	if e != nil {
		return e
	}
	if cfg.Search.APIKey == "" && cfg.Search.Provider == st.Settings.Search.Provider {
		cfg.Search.APIKey = st.Settings.Search.APIKey
	}
	if cfg.Search.Enabled && !((cfg.Search.Provider == "chatgpt" && cfg.Search.ProviderID != "") || ((cfg.Search.Provider == "zhipu" || cfg.Search.Provider == "deepseek" || cfg.Search.Provider == "yuanbao") && strings.TrimSpace(cfg.Search.APIKey) != "") || (cfg.Search.Provider == "zhipu-plan" && PlanProvider(cfg.Search.ProviderID) != nil)) {
		return errors.New("请先完成联网搜索配置")
	}
	cfg.Search.HasKey = cfg.Search.APIKey != ""
	if st.Settings.MemoryEnabled != cfg.MemoryEnabled {
		st.MemoryEpoch++
	}
	if !cfg.MemoryEnabled && activeCancel != nil {
		activeCancel()
	}
	st.Settings = cfg
	return write(st)
}
func DeleteMemory(id string) error {
	mu.Lock()
	defer mu.Unlock()
	st, e := read()
	if e != nil {
		return e
	}
	out := []Memory{}
	found := id == "all"
	for _, m := range st.Memories {
		if id == "all" || m.ID == id {
			st.Forgotten[m.Project+"/"+m.Key] = true
			found = true
		} else {
			out = append(out, m)
		}
	}
	if !found {
		return os.ErrNotExist
	}
	st.MemoryEpoch++
	st.Memories = out
	return write(st)
}
func EditMemory(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 500 {
		return errors.New("记忆内容需要 1–500 个字")
	}
	mu.Lock()
	defer mu.Unlock()
	st, e := read()
	if e != nil {
		return e
	}
	for i := range st.Memories {
		if st.Memories[i].ID == id {
			st.MemoryEpoch++
			st.Memories[i].Text = text
			st.Memories[i].Edited = true
			st.Memories[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return write(st)
		}
	}
	return os.ErrNotExist
}
func ApplyMemories(revision uint64, slug, message string, items []Candidate) error {
	mu.Lock()
	defer mu.Unlock()
	st, e := read()
	if e != nil {
		return e
	}
	if !st.Settings.MemoryEnabled || st.Revision != revision || st.Processed[slug+"/"+message] {
		return nil
	}
	for _, c := range items {
		c.Key = strings.TrimSpace(c.Key)
		c.Text = strings.TrimSpace(c.Text)
		if c.Key == "" || len(c.Key) > 100 || c.Text == "" || utf8.RuneCountInString(c.Text) > 500 {
			continue
		}
		if utf8.RuneCountInString(c.Evidence) > 500 {
			c.Evidence = string([]rune(c.Evidence)[:500])
		}
		project := ""
		if c.ProjectOnly {
			project = slug
		}
		key := project + "/" + c.Key
		if st.Forgotten[key] {
			continue
		}
		index := -1
		for i, m := range st.Memories {
			if m.Project == project && (m.Key == c.Key || m.Text == c.Text) {
				index = i
				break
			}
		}
		if index >= 0 && st.Memories[index].Edited {
			continue
		}
		m := Memory{ID: idgen.New("mem"), Key: c.Key, Text: c.Text, Project: project, SourceProject: slug, MessageID: message, Evidence: c.Evidence, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
		if index >= 0 {
			m.ID = st.Memories[index].ID
			st.Memories[index] = m
		} else if len(st.Memories) < 200 {
			st.Memories = append(st.Memories, m)
		}
	}
	st.Processed[slug+"/"+message] = true
	st.LastError = ""
	return write(st)
}
func Context(slug string) string {
	st, e := Read()
	if e != nil || !st.Settings.MemoryEnabled {
		return ""
	}
	var lines []string
	size := 0
	for i := len(st.Memories) - 1; i >= 0; i-- {
		m := st.Memories[i]
		if m.Project != "" && m.Project != slug {
			continue
		}
		if size+len(m.Text) > 6000 {
			continue
		}
		lines = append(lines, m.Text)
		size += len(m.Text)
	}
	if len(lines) == 0 {
		return ""
	}
	data, _ := json.Marshal(lines)
	text, err := ai.RenderPrompt("teaching/context/memory", map[string]string{"content": string(data)})
	if err != nil {
		panic(err)
	}
	return "\n\n" + text
}
func LengthPrompt(length string) string {
	if length != "short" && length != "medium" && length != "long" {
		length = "medium"
	}
	text, err := ai.RenderPrompt("teaching/response-length/"+length, nil)
	if err != nil {
		panic(err)
	}
	return "\n\n" + text
}

// Observe only receives the just-completed learner statement, never old history
// or the teacher's own claims. Opt-out/edit/delete invalidates in-flight results.
func Observe(slug, message, text, providerID string, gateway ai.Gateway) {
	st, e := Read()
	if e != nil || !st.Settings.MemoryEnabled || st.Processed[slug+"/"+message] || strings.TrimSpace(text) == "" {
		return
	}
	if providerID == "" || ai.IsManagedProvider(providerID) {
		// Memory extraction includes saved private memory and must use a
		// user-configured connection when the main turn uses a system model.
		cfg, err := ai.LoadConfig()
		if err != nil {
			return
		}
		provider := cfg.ResolveDirect("teacher")
		if provider == nil || !provider.HasConnection() {
			return
		}
		providerID = provider.ID
	}
	if len([]rune(text)) > 6000 {
		text = string([]rune(text)[:6000])
	}
	select {
	case slots <- struct{}{}:
	default:
		return
	}
	leave := workspacegate.Continue()
	go func() {
		defer leave()
		defer func() { <-slots }()
		extraction.Lock()
		defer extraction.Unlock()
		current, e := Read()
		if e != nil || !current.Settings.MemoryEnabled || current.MemoryEpoch != st.MemoryEpoch {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		mu.Lock()
		latest, checkErr := read()
		if checkErr != nil || !latest.Settings.MemoryEnabled || latest.MemoryEpoch != current.MemoryEpoch {
			mu.Unlock()
			return
		}
		activeCancel = cancel
		mu.Unlock()
		defer func() { mu.Lock(); activeCancel = nil; mu.Unlock() }()
		known := []map[string]string{}
		knownSize := 0
		for _, m := range current.Memories {
			if m.Project != "" && m.Project != slug {
				continue
			}
			if knownSize+len(m.Text) > 12000 {
				break
			}
			known = append(known, map[string]string{"key": m.Key, "text": m.Text, "project": m.Project})
			knownSize += len(m.Text)
		}
		existing, _ := json.Marshal(known)
		prompt, promptErr := ai.RenderPrompt("auxiliary/memory-extraction", map[string]string{"existing": string(existing)})
		if promptErr != nil {
			return
		}
		var output strings.Builder
		err := gateway.Stream(ctx, ai.GatewayRequest{ProviderID: providerID, System: prompt, Messages: []ai.GatewayMessage{{Role: "user", Content: text}}}, func(ev ai.GatewayEvent) {
			if ev.Type == "text-delta" && output.Len() < 32000 {
				output.WriteString(ev.Delta)
			}
		})
		if err == nil {
			raw := strings.TrimSpace(output.String())
			raw = strings.TrimPrefix(raw, "```json")
			raw = strings.TrimPrefix(raw, "```")
			raw = strings.TrimSuffix(raw, "```")
			var items []Candidate
			err = json.Unmarshal([]byte(raw), &items)
			if err == nil {
				valid := []Candidate{}
				for _, c := range items {
					if c.Evidence != "" && strings.Contains(text, c.Evidence) && len(valid) < 5 {
						valid = append(valid, c)
					}
				}
				err = ApplyMemories(current.Revision, slug, message, valid)
			}
		}
		if err != nil {
			mu.Lock()
			defer mu.Unlock()
			latest, e := read()
			if e == nil && latest.Settings.MemoryEnabled && latest.Revision == current.Revision {
				latest.LastError = "上次记忆整理未完成，后续新对话会继续整理。"
				_ = write(latest)
			}
		}
	}()
}
