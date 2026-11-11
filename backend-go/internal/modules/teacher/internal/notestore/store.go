package notestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	root string
	slug string
	mu   *sync.Mutex
}
type state struct {
	SchemaVersion int             `json:"schemaVersion"`
	LastSeq       uint64          `json:"lastSeq"`
	Count         int             `json:"count"`
	Seen          map[string]bool `json:"seen"`
	Pending       []NoteTurn      `json:"pending"`
}

var locks sync.Map

func New(slug string) (*Store, error) {
	root, err := projects.ProjectRootForSlug(slug)
	if err != nil {
		return nil, err
	}
	st, err := projects.ReadProjectState(slug)
	if err != nil {
		return nil, err
	}
	if st.ProjectType != projects.ProjectTypeSystemLearning {
		return nil, errors.New("notes require learning project")
	}
	root = filepath.Join(root, "assets", "notes")
	value, _ := locks.LoadOrStore(root, &sync.Mutex{})
	return &Store{root: root, slug: slug, mu: value.(*sync.Mutex)}, nil
}
func read(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func write(path string, in any) error {
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(path, data, 0600)
}
func (s *Store) Initialize(latestSeq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st state
	if err := read(filepath.Join(s.root, "state.json"), &st); err == nil {
		if st.SchemaVersion != 1 || st.Seen == nil {
			return errors.New("invalid note state")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seen := map[string]bool{}
	c, err := conversation.New(s.slug)
	if err != nil {
		return err
	}
	turns, err := c.SuccessfulNoteTurns(0)
	if err != nil {
		return err
	}
	for _, turn := range turns {
		if turn.CompletedSeq <= latestSeq {
			seen[turn.LearnerMessageID] = true
		}
	}
	return write(filepath.Join(s.root, "state.json"), state{SchemaVersion: 1, LastSeq: latestSeq, Seen: seen, Pending: []NoteTurn{}})
}
func (s *Store) load() (state, error) {
	var st state
	err := read(filepath.Join(s.root, "state.json"), &st)
	if err == nil && (st.SchemaVersion != 1 || st.Seen == nil) {
		err = errors.New("invalid note state")
	}
	return st, err
}
func (s *Store) Observe(cfg AutoNotesSettings, latestSeq uint64, turns []NoteTurn) ([]NoteJob, error) {
	if cfg.EveryTurns < 1 || cfg.EveryTurns > 100 {
		return nil, errors.New("invalid note interval")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	resource, err := s.list()
	if err != nil {
		return nil, err
	}
	// Jobs are authoritative when a crash occurred between job and state writes.
	claimed := map[string]bool{}
	for _, job := range resource.Jobs {
		for _, turn := range job.Turns {
			claimed[turn.LearnerMessageID] = true
			st.Seen[turn.LearnerMessageID] = true
		}
		if job.ThroughTurn > st.Count {
			st.Count = job.ThroughTurn
		}
	}
	pending := st.Pending[:0]
	for _, turn := range st.Pending {
		if !claimed[turn.LearnerMessageID] {
			pending = append(pending, turn)
		}
	}
	st.Pending = pending
	for _, turn := range turns {
		if turn.CompletedSeq <= st.LastSeq || turn.CompletedSeq > latestSeq || turn.LearnerMessageID == "" {
			continue
		}
		if st.Seen[turn.LearnerMessageID] {
			for i := range st.Pending {
				if st.Pending[i].LearnerMessageID == turn.LearnerMessageID {
					st.Pending[i] = turn
				}
			}
			continue
		}
		st.Seen[turn.LearnerMessageID] = true
		if cfg.Enabled {
			st.Pending = append(st.Pending, turn)
		}
	}
	var jobs []NoteJob
	for cfg.Enabled && len(st.Pending) >= cfg.EveryTurns {
		batch := append([]NoteTurn(nil), st.Pending[:cfg.EveryTurns]...)
		job := NoteJob{ID: BatchID(batch), Status: "queued", FromTurn: st.Count + 1, ThroughTurn: st.Count + len(batch), Turns: batch, ProviderID: cfg.ProviderID, Model: cfg.Model, Retryable: true}
		if job.ProviderID == "" {
			job.ProviderID = batch[len(batch)-1].ProviderID
		}
		if job.Model == "" {
			job.Model = batch[len(batch)-1].Model
		}
		if ai.IsManagedProvider(job.ProviderID) && !AllManagedTurns(batch) {
			job.ProviderID, job.Model = "", ""
			for i := len(batch) - 1; i >= 0; i-- {
				turn := batch[i]
				if turn.ProviderID != "" && !ai.IsManagedProvider(turn.ProviderID) {
					job.ProviderID, job.Model = turn.ProviderID, turn.Model
					break
				}
			}
		}
		if err := write(filepath.Join(s.root, "jobs", job.ID+".json"), job); err != nil {
			return nil, err
		}
		st.Count = job.ThroughTurn
		st.Pending = st.Pending[cfg.EveryTurns:]
		jobs = append(jobs, job)
	}
	if latestSeq > st.LastSeq {
		st.LastSeq = latestSeq
	}
	return jobs, write(filepath.Join(s.root, "state.json"), st)
}

// AllManagedTurns requires explicit provenance before a note batch may use
// the cloud. Old turns with no provider and mixed batches remain local.
func AllManagedTurns(turns []NoteTurn) bool {
	for _, turn := range turns {
		if !ai.IsManagedProvider(turn.ProviderID) {
			return false
		}
	}
	return len(turns) > 0
}

func BatchID(turns []NoteTurn) string {
	ids := make([]string, 0, len(turns))
	for _, turn := range turns {
		ids = append(ids, turn.LearnerMessageID)
	}
	return stableID("notejob", strings.Join(ids, "\x00"))
}

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}
func (s *Store) list() (NotesResource, error) {
	r := NotesResource{Entries: []NoteEntry{}, Jobs: []NoteJob{}}
	for _, dir := range []string{"entries", "jobs"} {
		files, err := os.ReadDir(filepath.Join(s.root, dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return r, err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(s.root, dir, f.Name())
			if dir == "entries" {
				var v NoteEntry
				if err := read(path, &v); err != nil {
					return r, err
				}
				r.Entries = append(r.Entries, v)
			} else {
				var v NoteJob
				if err := read(path, &v); err != nil {
					return r, err
				}
				r.Jobs = append(r.Jobs, v)
			}
		}
	}
	sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].FromTurn < r.Entries[j].FromTurn })
	sort.Slice(r.Jobs, func(i, j int) bool { return r.Jobs[i].FromTurn < r.Jobs[j].FromTurn })
	return r, nil
}
func (s *Store) List() (NotesResource, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.list() }
func (s *Store) job(id string) (NoteJob, error) {
	if !strings.HasPrefix(id, "notejob_") || strings.ContainsAny(id, `/\`) {
		return NoteJob{}, ErrNoteJobNotFound
	}
	var job NoteJob
	err := read(filepath.Join(s.root, "jobs", id+".json"), &job)
	if errors.Is(err, os.ErrNotExist) {
		err = ErrNoteJobNotFound
	}
	return job, err
}
func (s *Store) Commit(id, content, provider, model string, usage NoteUsage) (NoteEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.job(id)
	if err != nil {
		return NoteEntry{}, err
	}
	entry := NoteEntry{ID: stableID("note", id), JobID: id, FromTurn: job.FromTurn, ThroughTurn: job.ThroughTurn, Content: content, ProviderID: provider, Model: model, CreatedAt: time.Now().UTC()}
	path := filepath.Join(s.root, "entries", entry.ID+".json")
	var existing NoteEntry
	if err := read(path, &existing); err == nil {
		entry = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return entry, err
	} else if err := write(path, entry); err != nil {
		return entry, err
	}
	job.Status, job.NoteID, job.ErrorCode, job.Retryable = "succeeded", entry.ID, "", false
	return entry, write(filepath.Join(s.root, "jobs", id+".json"), job)
}
func (s *Store) SetRunning(id string) (NoteJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.job(id)
	if err != nil {
		return job, err
	}
	if job.Status != "queued" {
		return job, ErrNoteJobRunning
	}
	job.Status = "running"
	return job, write(filepath.Join(s.root, "jobs", id+".json"), job)
}
func (s *Store) MarkFailed(id, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.job(id)
	if err != nil {
		return err
	}
	job.Status, job.ErrorCode, job.Retryable = "failed", code, true
	return write(filepath.Join(s.root, "jobs", id+".json"), job)
}
func (s *Store) Retry(id string) (NoteJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.job(id)
	if err != nil {
		return job, err
	}
	if job.Status != "failed" {
		return job, ErrNoteJobRunning
	}
	job.Status, job.ErrorCode = "queued", ""
	return job, write(filepath.Join(s.root, "jobs", id+".json"), job)
}

// Recover handles process restart and entry-before-job completion crashes.
func (s *Store) Recover() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.list()
	if err != nil {
		return err
	}
	entries := map[string]NoteEntry{}
	for _, e := range r.Entries {
		entries[e.JobID] = e
	}
	for _, job := range r.Jobs {
		if e, ok := entries[job.ID]; ok {
			job.Status, job.NoteID, job.Retryable = "succeeded", e.ID, false
		} else if job.Status == "running" {
			job.Status, job.ErrorCode, job.Retryable = "failed", "interrupted", true
		} else {
			continue
		}
		if err := write(filepath.Join(s.root, "jobs", job.ID+".json"), job); err != nil {
			return err
		}
	}
	return nil
}

// SealImportedNotes prevents a receiver's auto-note worker from turning historical
// conversation turns into new requests when the sender's note cursor was behind.
func SealImportedNotes(root string) error {
	p := filepath.Join(root, "assets", "notes", "state.json")
	var state map[string]json.RawMessage
	if e := read(p, &state); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	var meta struct {
		LatestSeq uint64 `json:"latestSeq"`
	}
	if e := read(filepath.Join(root, "conversation", "conversation.json"), &meta); errors.Is(e, os.ErrNotExist) {
		return nil
	} else if e != nil {
		return e
	}
	var last uint64
	if raw := state["lastSeq"]; raw != nil {
		if e := json.Unmarshal(raw, &last); e != nil {
			return e
		}
	}
	if meta.LatestSeq <= last {
		return nil
	}
	state["lastSeq"], _ = json.Marshal(meta.LatestSeq)
	return write(p, state)
}
