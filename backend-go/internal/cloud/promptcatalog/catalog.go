// Package promptcatalog loads immutable signed prompt snapshots for the cloud API.
package promptcatalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

var experimentID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

type Experiment struct {
	ID, Scene, Root, Version string
	Percent                  int
}

type Draft struct {
	Version     string            `json:"version"`
	Resources   map[string]string `json:"resources"`
	Experiments []VariantDraft    `json:"experiments"`
}

type VariantDraft struct {
	ID        string            `json:"id"`
	Scene     string            `json:"scene"`
	Version   string            `json:"version"`
	Percent   int               `json:"percent"`
	Overrides map[string]string `json:"overrides"`
}

type Catalog struct {
	stable     map[string]promptpack.Pack
	experiment map[string]variant
}

type variant struct {
	control, treatment promptpack.Pack
	percent            int
}

type ExperimentAllocation struct {
	ID               string `json:"id"`
	Scene            string `json:"scene"`
	TreatmentPercent int    `json:"treatmentPercent"`
}

func (c *Catalog) ExperimentAllocations() []ExperimentAllocation {
	out := make([]ExperimentAllocation, 0, len(c.experiment))
	for scene, item := range c.experiment {
		out = append(out, ExperimentAllocation{item.control.Content.Experiment, scene, item.percent})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func New(root, version string, key ed25519.PrivateKey, tests []Experiment) (*Catalog, error) {
	draft, err := LoadDraft(root, version, tests)
	if err != nil {
		return nil, err
	}
	return Build(draft, key)
}

func LoadDraft(root, version string, tests []Experiment) (Draft, error) {
	contents, err := readResources(root)
	if err != nil {
		return Draft{}, err
	}
	draft := Draft{Version: version, Resources: contents}
	for _, test := range tests {
		experimental, err := readResources(test.Root)
		if err != nil {
			return Draft{}, err
		}
		draft.Experiments = append(draft.Experiments, VariantDraft{
			ID: test.ID, Scene: test.Scene, Version: test.Version, Percent: test.Percent, Overrides: experimental,
		})
	}
	return draft, nil
}

func Build(draft Draft, key ed25519.PrivateKey) (*Catalog, error) {
	for _, name := range promptpack.ResourceNames() {
		if draft.Resources[name] == "" {
			return nil, errors.New("missing required cloud prompt resource")
		}
	}
	c := &Catalog{stable: make(map[string]promptpack.Pack), experiment: make(map[string]variant)}
	for _, scene := range []string{"teacher/general", "auxiliary/section-summary", "generation/practice", "auxiliary/notes"} {
		base, err := promptpack.Sign(promptpack.Content{Version: draft.Version, Scene: scene, Resources: draft.Resources}, key)
		if err != nil {
			return nil, err
		}
		c.stable[scene] = base
	}
	ids := make(map[string]bool, len(draft.Experiments))
	for _, test := range draft.Experiments {
		base, exists := c.stable[test.Scene]
		if !exists || !experimentID.MatchString(test.ID) || ids[test.ID] || len(test.Overrides) == 0 || test.Percent < 0 || test.Percent > 100 || c.experiment[test.Scene].control.Digest != "" {
			return nil, errors.New("invalid or duplicate prompt experiment")
		}
		ids[test.ID] = true
		experimental := maps.Clone(draft.Resources)
		for name, value := range test.Overrides {
			experimental[name] = value
		}
		control := base.Content
		control.Experiment, control.Variant = test.ID, "control"
		a, err := promptpack.Sign(control, key)
		if err != nil {
			return nil, err
		}
		b, err := promptpack.Sign(promptpack.Content{
			Version: test.Version, Scene: test.Scene, Experiment: test.ID, Variant: "treatment", Resources: experimental,
		}, key)
		if err != nil {
			return nil, err
		}
		c.experiment[test.Scene] = variant{control: a, treatment: b, percent: test.Percent}
	}
	return c, nil
}

func (c *Catalog) ForAccount(scene, account string) (promptpack.Pack, bool) {
	if v, exists := c.experiment[scene]; exists {
		if assignedTreatment(v.control.Content.Experiment, account, v.percent) {
			return v.treatment, true
		}
		return v.control, true
	}
	base, exists := c.stable[scene]
	return base, exists
}

func assignedTreatment(experiment, account string, percent int) bool {
	hash := sha256.Sum256([]byte(experiment + "\x00" + account))
	return binary.BigEndian.Uint64(hash[:8])%100 < uint64(percent)
}

type Exposure struct {
	Experiment, Variant, Version, Digest string
}

type exposureVariants struct {
	control, treatment Exposure
	percent            int
}

type ExposureIndex map[string]exposureVariants

// ExposureIndex retains assignment metadata without retaining retired templates.
func (c *Catalog) ExposureIndex() ExposureIndex {
	result := make(ExposureIndex, len(c.experiment))
	exposure := func(p promptpack.Pack) Exposure {
		return Exposure{p.Content.Experiment, p.Content.Variant, p.Content.Version, p.Digest}
	}
	for scene, variant := range c.experiment {
		result[scene] = exposureVariants{exposure(variant.control), exposure(variant.treatment), variant.percent}
	}
	return result
}

func (i ExposureIndex) Match(account string, ref promptpack.Reference) (Exposure, bool) {
	variant, exists := i[ref.Scene]
	if !exists {
		return Exposure{}, false
	}
	exposure := variant.control
	if assignedTreatment(exposure.Experiment, account, variant.percent) {
		exposure = variant.treatment
	}
	if exposure.Digest != ref.Digest {
		return Exposure{}, false
	}
	return exposure, true
}

func readResources(root string) (map[string]string, error) {
	contents := make(map[string]string)
	for _, name := range promptpack.ResourceNames() {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name+".md")))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 128<<10 {
			return nil, errors.New("invalid cloud prompt file")
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name+".md")))
		if err != nil {
			return nil, err
		}
		contents[name] = string(data)
	}
	return contents, nil
}
