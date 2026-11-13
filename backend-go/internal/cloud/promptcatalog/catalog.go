// Package promptcatalog loads immutable signed prompt snapshots for the cloud API.
package promptcatalog

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

var experimentID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

type Draft struct {
	Version    string            `json:"version"`
	Resources  map[string]string `json:"resources"`
	Comparison *ComparisonDraft  `json:"comparison,omitempty"`
}

type ComparisonDraft struct {
	ID       string              `json:"id"`
	Name     string              `json:"name,omitempty"`
	Model    string              `json:"model"`
	Percent  int                 `json:"percent"`
	A        string              `json:"a"`
	B        string              `json:"b"`
	Variants *ComparisonVariants `json:"variants,omitempty"`
}

type ComparisonVariants struct {
	A map[string]string `json:"a"`
	B map[string]string `json:"b"`
}

const OpeningResource = "teaching/runtime/opening"
const ReviewHTMLScene = "generation/review-html"

const reviewHTMLContract = "只输出一个完整 HTML 片段，从 <article> 开始，到 </article> 结束。不得输出 Markdown 围栏、页面外壳、脚本、样式、表单或嵌入资源。"

func HasReviewHTMLContract(text string) bool {
	return strings.Contains(text, "<article>") && strings.Contains(text, "</article>")
}

type ComparisonPack struct {
	promptpack.Pack
	Opening   string   `json:"opening,omitempty"`
	Overrides []string `json:"overrides"`
}

type Catalog struct {
	stable     map[string]promptpack.Pack
	signingKey ed25519.PrivateKey
	Comparison *ComparisonDraft
	A, B       ComparisonPack
}

func LoadDraft(root, version string) (Draft, error) {
	contents, err := readResources(root)
	return Draft{Version: version, Resources: contents}, err
}

func Build(draft Draft, key ed25519.PrivateKey) (*Catalog, error) {
	return BuildWithExperimentBase(draft, draft.Resources, key)
}

func BuildWithExperimentBase(draft Draft, experimentBase map[string]string, key ed25519.PrivateKey) (*Catalog, error) {
	for _, name := range promptpack.ResourceNames() {
		if draft.Resources[name] == "" {
			return nil, fmt.Errorf("missing cloud prompt resource %s", name)
		}
	}
	c := &Catalog{stable: make(map[string]promptpack.Pack), signingKey: key}
	for _, scene := range []string{"teacher/general", "auxiliary/section-summary", "generation/review", ReviewHTMLScene, "generation/practice", "auxiliary/notes"} {
		resources := maps.Clone(draft.Resources)
		if scene == ReviewHTMLScene {
			resources["review"] += "\n\n" + reviewHTMLContract
		} else if scene != "generation/review" {
			delete(resources, "review")
		}
		base, err := promptpack.Sign(promptpack.Content{Version: draft.Version, Scene: scene, Resources: resources}, key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", scene, err)
		}
		c.stable[scene] = base
	}
	if config := draft.Comparison; config != nil {
		if !experimentID.MatchString(config.ID) || !experimentID.MatchString(config.Model) ||
			config.Percent < 0 || config.Percent > 100 || len(config.Name) > 240 {
			return nil, errors.New("invalid teacher answer comparison")
		}
		if experimentBase == nil {
			return nil, errors.New("comparison template baseline is unavailable")
		}
		var overridesA, overridesB map[string]string
		if config.Variants != nil {
			overridesA, overridesB = config.Variants.A, config.Variants.B
		}
		if config.Variants == nil && (strings.TrimSpace(config.A) == "" || strings.TrimSpace(config.B) == "") {
			return nil, errors.New("both comparison teacher prompts are required")
		}
		signed := func(variant, text string, overrides map[string]string) (ComparisonPack, error) {
			resources := maps.Clone(experimentBase)
			changes := []string{}
			if strings.TrimSpace(text) != "" {
				resources["teaching/teacher"] = text
				changes = append(changes, "teaching/teacher")
			}
			for resource, value := range overrides {
				if resource == OpeningResource {
					if len(value) > 16<<10 || strings.Contains(value, "{{") {
						return ComparisonPack{}, fmt.Errorf("variant %s: invalid opening prompt", variant)
					}
					continue
				}
				if _, ok := resources[resource]; !ok {
					return ComparisonPack{}, fmt.Errorf("variant %s: unknown prompt %s", variant, resource)
				}
				if strings.TrimSpace(value) != "" {
					resources[resource] = value
					changes = append(changes, resource)
				}
			}
			pack, err := promptpack.Sign(promptpack.Content{Version: draft.Version, Scene: "teacher/general", Experiment: config.ID, Variant: variant, Resources: resources}, key)
			if err != nil {
				return ComparisonPack{}, fmt.Errorf("variant %s: %w", variant, err)
			}
			return ComparisonPack{Pack: pack, Opening: strings.TrimSpace(overrides[OpeningResource]), Overrides: changes}, nil
		}
		a, err := signed("a", config.A, overridesA)
		if err != nil {
			return nil, err
		}
		b, err := signed("b", config.B, overridesB)
		if err != nil {
			return nil, err
		}
		if maps.Equal(a.Content.Resources, b.Content.Resources) && a.Opening == b.Opening {
			return nil, errors.New("experiment A and B must differ")
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

func (c *Catalog) Selected(scene string, selected ComparisonPack) (promptpack.Pack, error) {
	base := c.stable[scene]
	content := base.Content
	if selected.Overrides == nil {
		selected.Overrides = []string{"teaching/teacher"}
	}
	content.Resources = maps.Clone(content.Resources)
	changed := false
	for _, resource := range selected.Overrides {
		value := selected.Content.Resources[resource]
		if consumes(scene, resource) && value != "" {
			content.Resources[resource] = value
			changed = true
		}
	}
	if !changed {
		return base, nil
	}
	if scene == ReviewHTMLScene {
		content.Resources["review"] += "\n\n" + reviewHTMLContract
	}
	content.Experiment = selected.Content.Experiment
	content.Variant = selected.Content.Variant
	return promptpack.Sign(content, c.signingKey)
}

func consumes(scene, resource string) bool {
	switch scene {
	case "teacher/general":
		return resource == "teaching/teacher" || strings.HasPrefix(resource, "modes/")
	case "generation/review", ReviewHTMLScene:
		return resource == "review"
	case "generation/practice":
		return resource == "practice"
	default:
		return scene == resource
	}
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
