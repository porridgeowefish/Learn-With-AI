// Package promptcatalog loads immutable signed prompt snapshots for the cloud API.
package promptcatalog

import (
	"crypto/ed25519"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"

	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

var experimentID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

type Draft struct {
	Version    string            `json:"version"`
	Resources  map[string]string `json:"resources"`
	Comparison *ComparisonDraft  `json:"comparison,omitempty"`
}

type ComparisonDraft struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Percent int    `json:"percent"`
	A       string `json:"a"`
	B       string `json:"b"`
}

type Catalog struct {
	stable     map[string]promptpack.Pack
	signingKey ed25519.PrivateKey
	Comparison *ComparisonDraft
	A, B       promptpack.Pack
}

func LoadDraft(root, version string) (Draft, error) {
	contents, err := readResources(root)
	return Draft{Version: version, Resources: contents}, err
}

func Build(draft Draft, key ed25519.PrivateKey) (*Catalog, error) {
	for _, name := range promptpack.ResourceNames() {
		if draft.Resources[name] == "" {
			return nil, errors.New("missing required cloud prompt resource")
		}
	}
	c := &Catalog{stable: make(map[string]promptpack.Pack), signingKey: key}
	for _, scene := range []string{"teacher/general", "auxiliary/section-summary", "generation/review", "generation/practice", "auxiliary/notes"} {
		resources := maps.Clone(draft.Resources)
		if scene != "generation/review" {
			delete(resources, "review")
		}
		base, err := promptpack.Sign(promptpack.Content{Version: draft.Version, Scene: scene, Resources: resources}, key)
		if err != nil {
			return nil, err
		}
		c.stable[scene] = base
	}
	if config := draft.Comparison; config != nil {
		if !experimentID.MatchString(config.ID) || !experimentID.MatchString(config.Model) ||
			config.Percent < 0 || config.Percent > 100 || config.A == config.B || config.A == "" || config.B == "" {
			return nil, errors.New("invalid teacher answer comparison")
		}
		scene := "teacher/general"
		signed := func(variant, text string) (promptpack.Pack, error) {
			resources := maps.Clone(c.stable[scene].Content.Resources)
			resources["teaching/teacher"] = text
			return promptpack.Sign(promptpack.Content{Version: draft.Version, Scene: scene, Experiment: config.ID, Variant: variant, Resources: resources}, key)
		}
		a, err := signed("a", config.A)
		if err != nil {
			return nil, err
		}
		b, err := signed("b", config.B)
		if err != nil {
			return nil, err
		}
		copy := *config
		c.Comparison, c.A, c.B = &copy, a, b
	}
	return c, nil
}

func (c *Catalog) Stable(scene string) (promptpack.Pack, bool) {
	base, exists := c.stable[scene]
	return base, exists
}

func (c *Catalog) SelectedTeacher(selected promptpack.Pack) (promptpack.Pack, error) {
	content := c.stable["teacher/general"].Content
	content.Resources = maps.Clone(content.Resources)
	content.Resources["teaching/teacher"] = selected.Content.Resources["teaching/teacher"]
	content.Experiment = selected.Content.Experiment
	content.Variant = selected.Content.Variant
	return promptpack.Sign(content, c.signingKey)
}

func readResources(root string) (map[string]string, error) {
	contents := make(map[string]string)
	for _, name := range promptpack.ResourceNames() {
		text, err := ReadResource(root, name)
		if err != nil {
			return nil, err
		}
		contents[name] = text
	}
	return contents, nil
}

func ReadResource(root, name string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(name+".md"))
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<10 {
		return "", errors.New("invalid cloud prompt file")
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
