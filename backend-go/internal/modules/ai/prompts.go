package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type PromptSource struct {
	Role         string
	Read         func(context.Context) (string, error)
	Messages     func(context.Context) ([]GatewayMessage, error)
	ReadWithArgs func(context.Context, map[string]string) (string, error)
}
type ComposedPrompt struct {
	Messages        []GatewayMessage
	HistoryStart    int
	HistoryEnd      int
	Text            string
	Tools           []string
	Budgets         map[string]int
	Skipped         []string
	ResourceVersion string
	Fragments       []PromptFragment
}

// PromptFragment records local provenance. Unknown source versions and freshness
// are intentionally omitted rather than inferred from content.
type PromptFragment struct {
	Resource string
	SourceID string
	Role     string
	SHA256   string
}
type promptSlot struct {
	Resource string           `json:"resource"`
	Source   string           `json:"source"`
	Binding  string           `json:"contextBinding"`
	History  bool             `json:"history"`
	When     *promptCondition `json:"when,omitempty"`
}
type promptCondition struct {
	Optional bool              `json:"optional"`
	Fact     string            `json:"fact"`
	Equals   string            `json:"equals"`
	In       []string          `json:"in"`
	All      []promptCondition `json:"all"`
	Any      []promptCondition `json:"any"`
	Not      *promptCondition  `json:"not"`
}
type promptProfile struct {
	Facts           map[string]string        `json:"facts"`
	SchemaVersion   int                      `json:"schemaVersion"`
	SceneID         string                   `json:"sceneID"`
	ContextBindings map[string]promptBinding `json:"contextBindings"`
	Budgets         map[string]int           `json:"budgets"`
	Slots           []promptSlot             `json:"slots"`
	Tools           []string                 `json:"tools"`
}
type promptBinding struct {
	SourceID string            `json:"sourceID"`
	Role     string            `json:"role"`
	Required bool              `json:"required"`
	When     *promptCondition  `json:"when,omitempty"`
	Args     map[string]string `json:"args"`
	MaxBytes int               `json:"maxBytes"`
	Template string            `json:"template"`
}

var promptName = regexp.MustCompile(`^[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)*$`)
var promptVariable = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)
var promptCache struct {
	sync.Mutex
	key   string
	files map[string][]byte
}

func promptSnapshot() (map[string][]byte, error) {
	root := filepath.Join(filesystem.PROJECT_ROOT, "learning-prompts")
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Version   string `json:"version"`
		Resources []struct {
			Path      string   `json:"path"`
			SHA256    string   `json:"sha256"`
			Variables []string `json:"variables"`
		} `json:"resources"`
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		return nil, err
	}
	key := root + fmt.Sprintf("%x", sha256.Sum256(b))
	for _, r := range manifest.Resources {
		rel := strings.TrimPrefix(r.Path, "learning-prompts/")
		if !strings.HasPrefix(r.Path, "learning-prompts/") || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("invalid manifest path")
		}
		info, e := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if e != nil {
			return nil, e
		}
		key += fmt.Sprintf("|%s:%d:%d", rel, info.Size(), info.ModTime().UnixNano())
	}
	promptCache.Lock()
	defer promptCache.Unlock()
	if promptCache.key == key && promptCache.files != nil {
		return promptCache.files, nil
	}
	result := map[string][]byte{}
	result["_manifest_version"] = []byte(manifest.Version)
	for _, r := range manifest.Resources {
		if !strings.HasPrefix(r.Path, "learning-prompts/") {
			return nil, fmt.Errorf("invalid manifest resource")
		}
		rel := strings.TrimPrefix(r.Path, "learning-prompts/")
		ext := filepath.Ext(rel)
		name := strings.TrimSuffix(rel, ext)
		if !promptName.MatchString(name) || (ext != ".md" && ext != ".json") {
			return nil, fmt.Errorf("invalid manifest path")
		}
		if _, exists := result[rel]; exists {
			return nil, fmt.Errorf("duplicate manifest resource")
		}
		data, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if e != nil {
			return nil, e
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != r.SHA256 {
			return nil, fmt.Errorf("prompt resource hash mismatch: %s", rel)
		}
		if r.Variables != nil {
			declared := map[string]bool{}
			for _, v := range r.Variables {
				if declared[v] {
					return nil, fmt.Errorf("duplicate prompt variable declaration")
				}
				declared[v] = true
			}
			used := map[string]bool{}
			for _, match := range promptVariable.FindAllStringSubmatch(string(data), -1) {
				used[match[1]] = true
			}
			if len(used) != len(declared) {
				return nil, fmt.Errorf("prompt variable declarations mismatch: %s", rel)
			}
			for v := range used {
				if !declared[v] {
					return nil, fmt.Errorf("undeclared prompt variable %s", v)
				}
			}
		}
		result[rel] = data
	}
	promptCache.key = key
	promptCache.files = result
	return result, nil
}
func snapshotFile(snapshot map[string][]byte, name, extension string) ([]byte, error) {
	if !promptName.MatchString(name) {
		return nil, fmt.Errorf("invalid prompt resource %q", name)
	}
	b, ok := snapshot[name+extension]
	if ok {
		return b, nil
	}
	// A bundle is stored once; only the explicitly selected entry is rendered.
	if extension == ".md" {
		if slash := strings.LastIndex(name, "/"); slash > 0 {
			if data, found := snapshot[name[:slash]+".json"]; found {
				var bundle struct {
					Prompts map[string]string `json:"prompts"`
				}
				if err := json.Unmarshal(data, &bundle); err != nil {
					return nil, fmt.Errorf("invalid prompt bundle %s: %w", name[:slash], err)
				}
				if value, found := bundle.Prompts[name[slash+1:]]; found {
					return []byte(value), nil
				}
			}
		}
	}
	return nil, fmt.Errorf("unregistered prompt resource %s", name)
}
func RenderPrompt(name string, variables map[string]string) (string, error) {
	return RenderPromptContext(context.Background(), name, variables)
}

func RenderPromptContext(ctx context.Context, name string, variables map[string]string) (string, error) {
	snapshot, err := promptSnapshotForContext(ctx)
	if err != nil {
		return "", err
	}
	return renderSnapshot(snapshot, name, variables)
}

type managedPromptKey struct{}

func ManagedPromptReference(ctx context.Context) *promptpack.Reference {
	content, _ := ctx.Value(managedPromptKey{}).(promptpack.Content)
	return content.Reference()
}

// CloudPromptContext captures the cloud template while keeping request assembly local.
func CloudPromptContext(ctx context.Context, scene string) context.Context {
	client, err := llmcloud.Default()
	if err != nil {
		return ctx
	}
	content, err := client.Prompts(ctx, scene)
	if err != nil {
		return ctx
	}
	return WithPromptContent(ctx, content)
}

func WithPromptContent(ctx context.Context, content promptpack.Content) context.Context {
	return context.WithValue(ctx, managedPromptKey{}, content)
}

func promptSnapshotForContext(ctx context.Context) (map[string][]byte, error) {
	snapshot, err := promptSnapshot()
	content, ok := ctx.Value(managedPromptKey{}).(promptpack.Content)
	if err != nil || !ok {
		return snapshot, err
	}
	snapshot = maps.Clone(snapshot)
	for name, value := range content.Resources {
		if _, exists := snapshot[name+".md"]; exists {
			snapshot[name+".md"] = []byte(value)
		}
	}
	snapshot["_manifest_version"] = []byte("cloud:" + content.Version)
	return snapshot, nil
}
func renderSnapshot(snapshot map[string][]byte, name string, variables map[string]string) (string, error) {
	b, e := snapshotFile(snapshot, name, ".md")
	if e != nil {
		return "", e
	}
	var missing string
	declared := map[string]bool{}
	for _, m := range promptVariable.FindAllStringSubmatch(string(b), -1) {
		declared[m[1]] = true
	}
	for key := range variables {
		if !declared[key] {
			return "", fmt.Errorf("unknown prompt variable %s", key)
		}
	}
	result := promptVariable.ReplaceAllStringFunc(string(b), func(s string) string {
		key := promptVariable.FindStringSubmatch(s)[1]
		v, ok := variables[key]
		if !ok {
			missing = key
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("missing prompt variable %s", missing)
	}
	unparsed := promptVariable.ReplaceAllString(string(b), "")
	if strings.Contains(unparsed, "{{") {
		return "", fmt.Errorf("invalid unresolved prompt variable")
	}
	return result, nil
}
func (c promptCondition) match(f map[string]string) (bool, error) {
	return c.matchDepth(f, 0)
}
func (c promptCondition) matchDepth(f map[string]string, depth int) (bool, error) {
	if depth > 32 {
		return false, fmt.Errorf("prompt condition nesting limit exceeded")
	}
	forms := 0
	if c.Fact != "" {
		forms++
	}
	if c.All != nil {
		forms++
	}
	if c.Any != nil {
		forms++
	}
	if c.Not != nil {
		forms++
	}
	if forms != 1 {
		return false, fmt.Errorf("invalid prompt condition")
	}
	if c.Fact != "" {
		v, ok := f[c.Fact]
		if !ok {
			if c.Optional {
				return false, nil
			}
			return false, fmt.Errorf("unknown prompt fact %s", c.Fact)
		}
		if c.In != nil {
			for _, candidate := range c.In {
				if v == candidate {
					return true, nil
				}
			}
			return false, nil
		}
		return v == c.Equals, nil
	}
	if c.Not != nil {
		v, e := c.Not.matchDepth(f, depth+1)
		return !v, e
	}
	list := c.All
	result := true
	if c.Any != nil {
		list = c.Any
		result = false
	}
	for _, n := range list {
		v, e := n.matchDepth(f, depth+1)
		if e != nil {
			return false, e
		}
		if c.Any != nil {
			result = result || v
		} else {
			result = result && v
		}
	}
	return result, nil
}
func ComposePrompt(ctx context.Context, scene string, facts map[string]string, sources map[string]PromptSource) (ComposedPrompt, error) {
	snapshot, err := promptSnapshotForContext(ctx)
	if err != nil {
		return ComposedPrompt{}, err
	}
	b, e := snapshotFile(snapshot, "profiles/"+scene, ".json")
	if e != nil {
		return ComposedPrompt{}, e
	}
	var p promptProfile
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&p); e != nil {
		return ComposedPrompt{}, e
	}
	if p.SchemaVersion != 1 || p.SceneID != scene {
		return ComposedPrompt{}, fmt.Errorf("invalid prompt profile identity")
	}
	if p.Facts != nil {
		for name, value := range facts {
			kind, ok := p.Facts[name]
			if !ok {
				return ComposedPrompt{}, fmt.Errorf("unregistered scene fact %s", name)
			}
			switch kind {
			case "boolean":
				if value != "true" && value != "false" {
					return ComposedPrompt{}, fmt.Errorf("invalid boolean fact %s", name)
				}
			case "string":
			default:
				return ComposedPrompt{}, fmt.Errorf("invalid scene fact type")
			}
		}
	}
	for _, tool := range p.Tools {
		if !promptName.MatchString(tool) {
			return ComposedPrompt{}, fmt.Errorf("invalid tool name %q", tool)
		}
	}
	var parts []string
	var messages []GatewayMessage
	var historyStart, historyEnd int
	var skipped []string
	var fragments []PromptFragment
	for _, s := range p.Slots {
		var binding promptBinding
		selectors := 0
		if s.History {
			selectors++
		}
		if s.Resource != "" {
			selectors++
		}
		if s.Source != "" {
			selectors++
		}
		if s.Binding != "" {
			selectors++
		}
		if selectors != 1 {
			return ComposedPrompt{}, fmt.Errorf("slot requires exactly one selection")
		}
		if s.When != nil {
			ok, err := s.When.match(facts)
			if err != nil {
				return ComposedPrompt{}, err
			}
			if !ok {
				skipped = append(skipped, "slot condition not satisfied or optional fact unavailable")
				continue
			}
		}
		if s.History {
			source, ok := sources["history"]
			if !ok || source.Messages == nil {
				return ComposedPrompt{}, fmt.Errorf("history source unavailable")
			}
			group, err := source.Messages(ctx)
			if err != nil {
				return ComposedPrompt{}, err
			}
			historyStart = len(messages)
			messages = append(messages, group...)
			historyEnd = len(messages)
			continue
		}
		if s.Binding != "" {
			var ok bool
			binding, ok = p.ContextBindings[s.Binding]
			if !ok {
				return ComposedPrompt{}, fmt.Errorf("unknown context binding %s", s.Binding)
			}
			if binding.Role != "context" && binding.Role != "user" {
				return ComposedPrompt{}, fmt.Errorf("context binding cannot elevate role")
			}
			s.Source = binding.SourceID
			if binding.When != nil {
				ok, err := binding.When.match(facts)
				if err != nil {
					return ComposedPrompt{}, err
				}
				if !ok {
					continue
				}
			}
		}
		if (s.Resource == "") == (s.Source == "") {
			return ComposedPrompt{}, fmt.Errorf("slot must select one resource or source")
		}
		if s.When != nil {
			ok, err := s.When.match(facts)
			if err != nil {
				return ComposedPrompt{}, err
			}
			if !ok {
				continue
			}
		}
		var text string
		if s.Resource != "" {
			resource, err := snapshotFile(snapshot, s.Resource, ".md")
			if err != nil {
				return ComposedPrompt{}, err
			}
			variables := map[string]string{}
			for _, m := range promptVariable.FindAllStringSubmatch(string(resource), -1) {
				if value, ok := facts[m[1]]; ok {
					variables[m[1]] = value
				}
			}
			text, e = renderSnapshot(snapshot, s.Resource, variables)
		} else {
			source, ok := sources[s.Source]
			if !ok || (source.Read == nil && source.ReadWithArgs == nil) {
				if s.Binding != "" && !binding.Required {
					continue
				}
				return ComposedPrompt{}, fmt.Errorf("unknown prompt source %s", s.Source)
			}
			if source.Role != "user" && source.Role != "context" {
				return ComposedPrompt{}, fmt.Errorf("invalid context source role")
			}
			args := map[string]string{}
			for arg, fact := range binding.Args {
				value, ok := facts[fact]
				if !ok {
					return ComposedPrompt{}, fmt.Errorf("missing binding fact %s", fact)
				}
				args[arg] = value
			}
			if source.ReadWithArgs != nil {
				text, e = source.ReadWithArgs(ctx, args)
			} else if len(args) > 0 {
				return ComposedPrompt{}, fmt.Errorf("source does not accept arguments")
			} else {
				text, e = source.Read(ctx)
			}
			if binding.MaxBytes > 0 && len(text) > binding.MaxBytes {
				return ComposedPrompt{}, fmt.Errorf("context capacity exceeded: %s", s.Source)
			}
			if e != nil {
				return ComposedPrompt{}, e
			}
			if binding.Template != "" {
				text, e = renderSnapshot(snapshot, binding.Template, map[string]string{"content": text})
				if e != nil {
					return ComposedPrompt{}, e
				}
			}

		}
		if e != nil {
			return ComposedPrompt{}, e
		}
		if s.Source != "" {
			messages = append(messages, GatewayMessage{Role: "user", Content: text, Pinned: true})
			fragments = append(fragments, PromptFragment{SourceID: s.Source, Role: "user", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text)))})
		} else {
			parts = append(parts, text)
			messages = append(messages, GatewayMessage{Role: "system", Content: text, Pinned: true})
			fragments = append(fragments, PromptFragment{Resource: s.Resource, Role: "system", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text)))})
		}
	}
	bytes := 0
	for _, m := range messages {
		bytes += len(m.Content)
	}
	for name, n := range p.Budgets {
		if n < 0 {
			return ComposedPrompt{}, fmt.Errorf("invalid prompt budget")
		}
		if name != "maxInputBytes" {
			return ComposedPrompt{}, fmt.Errorf("unknown prompt budget %s", name)
		}
		if n > 0 && bytes > n {
			return ComposedPrompt{}, fmt.Errorf("prompt input capacity exceeded")
		}
	}
	return ComposedPrompt{Text: strings.Join(parts, "\n\n"), Tools: p.Tools, Messages: messages, HistoryStart: historyStart, HistoryEnd: historyEnd, Budgets: p.Budgets, Skipped: skipped, Fragments: fragments, ResourceVersion: string(snapshot["_manifest_version"])}, nil
}

// PromptSceneTools resolves the declared tool names without reading context data.
func PromptSceneTools(scene string) ([]string, error) {
	snapshot, err := promptSnapshot()
	if err != nil {
		return nil, err
	}
	b, err := snapshotFile(snapshot, "profiles/"+scene, ".json")
	if err != nil {
		return nil, err
	}
	var p promptProfile
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return nil, err
	}
	if p.SchemaVersion != 1 || p.SceneID != scene {
		return nil, fmt.Errorf("invalid prompt profile identity")
	}
	return append([]string(nil), p.Tools...), nil
}
