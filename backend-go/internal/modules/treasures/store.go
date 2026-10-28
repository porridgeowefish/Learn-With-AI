package treasures

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed catalog.json
var catalogJSON []byte

type Item struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	English string `json:"english"`
	Kind    string `json:"kind"`
	Form    string `json:"form"`
	Color   string `json:"color"`
	Tier    int    `json:"tier"`
	URL     string `json:"url"`
}

func Catalog() []Item {
	var items []Item
	if err := json.Unmarshal(catalogJSON, &items); err != nil {
		panic(err)
	}
	return items
}

// Completion is emitted only by trusted, successful learning operations, never HTTP input.
type Completion struct{ Key, Kind, ProjectSlug, ProjectID, ProjectCreatedAt, ProjectTitle, SourceID, MessageID, Text string }
type Reward struct {
	ID               string    `json:"id"`
	ItemID           string    `json:"itemId"`
	Kind             string    `json:"kind"`
	ProjectSlug      string    `json:"projectSlug"`
	ProjectID        string    `json:"projectId"`
	ProjectCreatedAt string    `json:"projectCreatedAt"`
	ProjectTitle     string    `json:"projectTitle"`
	SourceID         string    `json:"sourceId"`
	MessageID        string    `json:"messageId"`
	CreatedAt        time.Time `json:"createdAt"`
	IsNew            bool      `json:"isNew"`
	Seen             bool      `json:"seen"`
	Welcome          bool      `json:"welcome,omitempty"`
}
type Ledger struct {
	Version        int               `json:"version"`
	Processed      map[string]bool   `json:"processed"`
	LastText       map[string]string `json:"lastText"`
	Misses         int               `json:"misses"`
	Rewards        []Reward          `json:"rewards"`
	WelcomeGranted bool              `json:"welcomeGranted,omitempty"`
}
type Store struct {
	root   string
	mu     *sync.Mutex
	random func(int) (int, error)
}

var locks sync.Map
var ErrRewardNotFound = errors.New("reward not found")

func New(root string) *Store {
	abs, _ := filepath.Abs(root)
	m, _ := locks.LoadOrStore(abs, &sync.Mutex{})
	return &Store{root: abs, mu: m.(*sync.Mutex), random: func(n int) (int, error) {
		v, e := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if e != nil {
			return 0, e
		}
		return int(v.Int64()), nil
	}}
}
func (s *Store) path() string { return filepath.Join(s.root, "treasures", "ledger.json") }
func (s *Store) read() (Ledger, error) {
	l := Ledger{Version: 1, Processed: map[string]bool{}, LastText: map[string]string{}, Rewards: []Reward{}}
	b, e := os.ReadFile(s.path())
	if errors.Is(e, os.ErrNotExist) {
		return l, nil
	}
	if e != nil {
		return l, e
	}
	if e = json.Unmarshal(b, &l); e != nil {
		return l, fmt.Errorf("read treasure ledger: %w", e)
	}
	if l.Version != 1 || l.Processed == nil || l.LastText == nil || l.Rewards == nil {
		return l, errors.New("invalid treasure ledger")
	}
	return l, nil
}
func (s *Store) save(l Ledger) error {
	b, e := json.MarshalIndent(l, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(s.path()), 0700); e != nil {
		return e
	}
	return paths.AtomicWriteFile(s.path(), b, 0600)
}
func (s *Store) Snapshot() (Ledger, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.read() }
func (s *Store) Complete(in Completion) (*Reward, error) {
	if in.Key == "" || in.ProjectID == "" || in.SourceID == "" || (in.Kind != "teacher" && in.Kind != "feynman") {
		return nil, errors.New("invalid reward completion")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, e := s.read()
	if e != nil {
		return nil, e
	}
	if l.Processed[in.Key] {
		return nil, nil
	}
	l.Processed[in.Key] = true
	digest := sha256.Sum256([]byte(strings.Join(strings.Fields(in.Text), " ")))
	hash := hex.EncodeToString(digest[:])
	scope := in.ProjectID + ":" + in.ProjectCreatedAt + ":" + in.Kind
	if strings.TrimSpace(in.Text) == "" || l.LastText[scope] == hash {
		return nil, s.save(l)
	}
	l.LastText[scope] = hash
	// Existing pre-release collections keep their contents without another welcome gift.
	welcome := !l.WelcomeGranted && len(l.Rewards) == 0
	if len(l.Rewards) > 0 {
		l.WelcomeGranted = true
	}
	chance := 18
	if l.Misses >= 4 {
		chance += 3 * (l.Misses - 3)
	}
	if chance > 45 {
		chance = 45
	}
	roll, e := s.random(100)
	if e != nil {
		return nil, e
	}
	if !welcome && l.Misses < 11 && roll >= chance {
		l.Misses++
		return nil, s.save(l)
	}
	rarity, e := s.random(100)
	if e != nil {
		return nil, e
	}
	tier := 1
	switch {
	case rarity >= 99:
		tier = 5
	case rarity >= 95:
		tier = 4
	case rarity >= 85:
		tier = 3
	case rarity >= 60:
		tier = 2
	}
	kindRoll, e := s.random(2)
	if e != nil {
		return nil, e
	}
	kind := "gem"
	if !welcome && kindRoll == 0 {
		kind = "shell"
	}
	candidates := []Item{}
	for _, item := range Catalog() {
		if item.Kind == kind && item.Tier == tier {
			candidates = append(candidates, item)
		}
	}
	choice, e := s.random(len(candidates))
	if e != nil {
		return nil, e
	}
	id := candidates[choice].ID
	fresh := true
	for _, r := range l.Rewards {
		if r.ItemID == id {
			fresh = false
			break
		}
	}
	sum := sha256.Sum256([]byte(in.Key))
	r := Reward{ID: hex.EncodeToString(sum[:16]), ItemID: id, Kind: in.Kind, ProjectSlug: in.ProjectSlug, ProjectID: in.ProjectID, ProjectCreatedAt: in.ProjectCreatedAt, ProjectTitle: in.ProjectTitle, SourceID: in.SourceID, MessageID: in.MessageID, CreatedAt: time.Now().UTC(), IsNew: fresh}
	l.Misses = 0
	r.Welcome = welcome
	l.WelcomeGranted = true
	l.Rewards = append(l.Rewards, r)
	if e = s.save(l); e != nil {
		return nil, e
	}
	return &r, nil
}
func (s *Store) Acknowledge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, e := s.read()
	if e != nil {
		return e
	}
	for i := range l.Rewards {
		if l.Rewards[i].ID == id {
			if l.Rewards[i].Seen {
				return nil
			}
			l.Rewards[i].Seen = true
			return s.save(l)
		}
	}
	return ErrRewardNotFound
}
