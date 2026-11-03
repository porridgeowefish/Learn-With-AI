package workspaceservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

const transaction = ".workspace-import"

var ErrImportRecovery = errors.New("导入事务需要恢复，请重新打开应用")

var globalRecords = []string{"projects/folders.json", "treasures/ledger.json"}

func loadJSON(p string, v any) error {
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func entries(root string) ([]ProjectEntry, map[string]bool, error) {
	list := []ProjectEntry{}
	ids := map[string]bool{}
	dir := filepath.Join(root, "projects")
	ds, e := os.ReadDir(dir)
	if errors.Is(e, os.ErrNotExist) {
		return list, ids, nil
	}
	if e != nil {
		return nil, nil, e
	}
	for _, d := range ds {
		if d.Type()&os.ModeSymlink != 0 {
			return nil, nil, errors.New("project link rejected")
		}
		if !d.IsDir() {
			continue
		}
		slug := d.Name()
		base := filepath.Join(dir, slug)
		var st projects.ProjectState
		if e = loadJSON(filepath.Join(base, "state.json"), &st); e != nil {
			return nil, nil, e
		}
		if st.Slug != slug || !projects.ValidateSlug(slug) || strings.TrimSpace(st.Title) == "" || st.ID == "" || ids[st.ID] {
			return nil, nil, fmt.Errorf("invalid or duplicate project identity %s", slug)
		}
		ids[st.ID] = true
		if e = projects.ValidateTransferState(base, &st); e != nil {
			return nil, nil, e
		}
		entry := ProjectEntry{Slug: slug, Title: st.Title}
		var ptr struct {
			VersionID string `json:"versionId"`
		}
		e = loadJSON(filepath.Join(base, "map", "current.json"), &ptr)
		if e == nil {
			if !safeName(ptr.VersionID) || strings.Contains(ptr.VersionID, "/") {
				return nil, nil, errors.New("invalid map pointer")
			}
			var c projects.Catalog
			if e = loadJSON(filepath.Join(base, "map", "versions", ptr.VersionID, "topics.json"), &c); e != nil {
				return nil, nil, e
			}
			for _, t := range c.Topics {
				entry.Topics = append(entry.Topics, TopicEntry{ID: t.ID, Title: t.Title, Chapter: t.ChapterTitle})
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, nil, e
		}
		list = append(list, entry)
	}
	return list, ids, nil
}
func generationInput(p string) bool {
	matched, _ := path.Match("projects/*/generation/inputs/*/input.json", p)
	return matched
}

func sanitize(p string, v map[string]any) error {
	if generationInput(p) {
		delete(v, "preferencesHash")
		messages, _ := v["messages"].([]any)
		for _, item := range messages {
			message, _ := item.(map[string]any)
			for _, key := range []string{"Content", "content"} {
				text, ok := message[key].(string)
				if !ok {
					continue
				}
				var payload map[string]json.RawMessage
				if json.Unmarshal([]byte(text), &payload) != nil || payload["learnerPreferences"] == nil {
					continue
				}
				delete(payload, "learnerPreferences")
				encoded, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				message[key] = string(encoded)
			}
		}
	}
	// Persisted identifiers and relative storage pointers may be used to construct
	// filesystem paths later, including on an OS different from the exporter.
	var check func(any) error
	check = func(obj any) error {
		switch x := obj.(type) {
		case []any:
			for _, n := range x {
				if e := check(n); e != nil {
					return e
				}
			}
		case map[string]any:
			for key, n := range x {
				switch strings.ToLower(key) {
				case "apikey", "api_key", "accesstoken", "refreshtoken", "access_token", "refresh_token", "authorization":
					return fmt.Errorf("credential field %s in learning record", key)
				}
				s, ok := n.(string)
				if ok && s != "" {
					if (strings.EqualFold(key, "path") || strings.EqualFold(key, "filename")) && !safeName(s) {
						return fmt.Errorf("unsafe %s", key)
					}
					if (strings.HasSuffix(strings.ToLower(key), "id") || strings.HasSuffix(strings.ToLower(key), "slug")) && (strings.ContainsAny(s, "/\\") || s == "." || s == "..") {
						return fmt.Errorf("unsafe %s", key)
					}
				}
				if e := check(n); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if err := check(v); err != nil {
		return err
	}
	if strings.Contains(p, "/jobs/") || strings.HasSuffix(p, "/source.json") {
		switch v["status"] {
		case "stored", "queued", "pending", "running", "parsing", "processing":
			v["importedStatus"] = v["status"]
			v["status"] = "failed"
			v["errorCode"] = "imported-interrupted"
		}
	}
	if strings.HasSuffix(p, "/assets/notes/state.json") {
		if a, _ := v["pending"].([]any); len(a) > 0 {
			v["importedPending"] = a
		}
		v["pending"] = []any{}
	}
	if strings.HasSuffix(p, "/conversation/events.jsonl") {
		data, _ := v["data"].(map[string]any)

		for _, key := range []string{"blocks", "mediaBlocks"} {
			if blocks, ok := data[key].([]any); ok {
				clean := []any{}
				for _, b := range blocks {
					block, _ := b.(map[string]any)
					switch block["type"] {
					case "chat-image", "chat-audio":
						if s, _ := block["source"].(string); teacher.IsExtractedMediaText(s) {
							clean = append(clean, map[string]any{"type": "markdown", "source": s, "id": block["id"]})
						}
					default:
						clean = append(clean, b)
					}
				}
				data[key] = clean
			}
		}
	}
	if strings.Contains(p, "/sources/") {
		if path.Base(p) == "source.json" {
			v["originalUnavailable"] = true
			delete(v, "retrieval")
		}
		if path.Base(p) == "revision.json" {
			orig, _ := v["original"].(map[string]any)
			v["original"] = map[string]any{"sha256": orig["sha256"]}
			v["originalUnavailable"] = true
			delete(v, "parseTaskId")
			v["privacy"] = map[string]any{"cloudDisclosureAccepted": false}
		}
		for _, key := range []string{"files", "derivedFiles"} {
			if a, ok := v[key].([]any); ok {
				clean := []any{}
				for _, n := range a {
					ref, _ := n.(map[string]any)
					f, _ := ref["path"].(string)
					if strings.HasSuffix(f, "/content.md") || strings.HasSuffix(f, "/structure.json") {
						clean = append(clean, n)
					}
				}
				v[key] = clean
			}
		}
	}
	return nil
}

type change struct {
	Path   string
	Backup bool
	ID     string
}

func validChange(p string) bool {
	if p == "preferences.md" {
		return true // Recover legacy transaction journals.
	}
	for _, s := range globalRecords {
		if s == p {
			return true
		}
	}
	a := strings.Split(p, "/")
	return len(a) == 2 && a[0] == "projects" && projects.ValidateSlug(a[1]) && a[1] != "folders.json"
}
func guard(root, p string) error {
	for cur := p; cur != "."; cur = path.Dir(cur) {
		i, e := os.Lstat(filepath.Join(root, filepath.FromSlash(cur)))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("link rejected: %s", cur)
		}
	}
	return nil
}

const removedTransaction = ".workspace-import-cleanup"
const importOwner = "lll:workspace-import:v1"

// No other transaction file may be created before this marker is complete.
func writeImportOwner(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(importOwner)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// Unknown preexisting directories are user data, even when their names are reserved.
func ownedImport(root, name string) (bool, error) {
	if e := guard(root, name+"/owner"); e != nil {
		return false, e
	}
	dir := filepath.Join(root, name)
	_, e := os.Lstat(dir)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	b, e := os.ReadFile(filepath.Join(dir, "owner"))
	if errors.Is(e, os.ErrNotExist) {
		ds, err := os.ReadDir(dir)
		if err == nil && len(ds) == 0 {
			return false, os.Remove(dir)
		}
	}
	if e != nil || string(b) != importOwner {
		if name == transaction && e == nil && strings.HasPrefix(importOwner, string(b)) {
			ds, err := os.ReadDir(dir)
			if err == nil && len(ds) == 1 && ds[0].Name() == "owner" && ds[0].Type().IsRegular() {
				if err = os.Remove(filepath.Join(dir, "owner")); err != nil {
					return false, err
				}
				return false, os.Remove(dir)
			}
		}
		return false, fmt.Errorf("unrecognized import directory: %s", dir)
	}
	return true, nil
}
func clearOwnedTransaction(root, name string) error {
	owned, e := ownedImport(root, name)
	if !owned || e != nil {
		return e
	}
	dir := filepath.Join(root, name)
	ds, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	for _, d := range ds {
		if d.Name() != "owner" {
			if e = os.RemoveAll(filepath.Join(dir, d.Name())); e != nil {
				return e
			}
		}
	}
	if e = os.Remove(filepath.Join(dir, "owner")); e != nil {
		return e
	}
	return os.Remove(dir)
}

// Unlink the journal only after rollback or commit has finished. On restart,
// an owned directory without a journal contains disposable staging data only.
func retireImport(root string) error {
	journal := filepath.Join(root, transaction, "journal.json")
	if e := os.Remove(journal); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return clearOwnedTransaction(root, transaction)
}
func RecoverImport(root string) error {
	if e := clearOwnedTransaction(root, removedTransaction); e != nil {
		return e
	}
	owned, e := ownedImport(root, transaction)
	if !owned || e != nil {
		return e
	}
	tx := filepath.Join(root, transaction)
	var changes []change
	e = loadJSON(filepath.Join(tx, "journal.json"), &changes)
	if errors.Is(e, os.ErrNotExist) {
		return retireImport(root)
	}
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(tx, "complete")); errors.Is(e, os.ErrNotExist) {
		for i := len(changes) - 1; i >= 0; i-- {
			ch := changes[i]
			if !validChange(ch.Path) || !safeName(ch.Path) {
				return errors.New("invalid import journal")
			}
			if e = guard(root, ch.Path); e != nil {
				return e
			}
			dst := filepath.Join(root, filepath.FromSlash(ch.Path))

			src := filepath.Join(tx, "candidate", ch.Path)
			if _, err := os.Lstat(src); errors.Is(err, os.ErrNotExist) {
				id, err := transferFileID(dst)
				if err != nil {
					return err
				}
				if id != ch.ID || id == "" {
					return fmt.Errorf("import destination identity changed: %s", dst)
				}
				if err = filesystem.PublishDirectory(dst, src); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if ch.Backup {
				displaced := filepath.Join(tx, "displaced", ch.Path)
				if _, err := os.Lstat(displaced); err == nil {
					if err = filesystem.PublishDirectory(displaced, dst); err != nil {
						return err
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	} else if e != nil {
		return e
	}
	return retireImport(root)
}
func Import(ctx context.Context, root, zipPath string) ([]NameConflict, error) {
	return importArchive(ctx, root, zipPath, filesystem.PublishDirectory)
}
func importArchive(ctx context.Context, root, zipPath string, publish func(string, string) error) (conflicts []NameConflict, err error) {
	if err = RecoverImport(root); err != nil {
		err = errors.Join(ErrImportRecovery, err)
		return
	}
	if err = guard(root, "projects"); err != nil {
		return
	}
	tx := filepath.Join(root, transaction)
	if err = os.Mkdir(tx, 0700); err != nil {
		return
	}
	if err = writeImportOwner(tx); err != nil {
		return
	}
	defer func() {
		if cleanup := RecoverImport(root); cleanup != nil {
			err = errors.Join(ErrImportRecovery, err, cleanup)
		}
	}()
	stage := filepath.Join(tx, "candidate")
	if err = os.Mkdir(stage, 0700); err != nil {
		return
	}
	if err = readArchive(ctx, zipPath, stage); err != nil {
		return
	}
	if err = validateCandidate(ctx, stage); err != nil {
		return
	}
	current, oldIDs, e := entries(root)
	if e != nil {
		return nil, e
	}
	incoming, ids, e := entries(stage)
	if e != nil {
		return nil, e
	}
	if c := ImportNameConflicts(current, incoming); len(c) > 0 {
		return c, nil
	}
	for id := range ids {
		if oldIDs[id] {
			return nil, fmt.Errorf("project ID collision: %s", id)
		}
	}
	var changes []change
	for _, pr := range incoming {
		p := "projects/" + pr.Slug
		dst := filepath.Join(root, p)
		if _, e = os.Lstat(dst); !errors.Is(e, os.ErrNotExist) {
			return nil, fmt.Errorf("project destination unavailable: %s", p)
		}
		changes = append(changes, change{Path: p})
	}
	for _, p := range globalRecords {
		candidate := filepath.Join(stage, p)
		b, e := os.ReadFile(candidate)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if e = guard(root, p); e != nil {
			return nil, e
		}
		old, e := os.ReadFile(filepath.Join(root, p))
		exists := e == nil
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if !exists && p == "projects/folders.json" {
			if e = guard(root, "folders.json"); e != nil {
				return nil, e
			}
			old, e = os.ReadFile(filepath.Join(root, "folders.json"))
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return nil, e
			}
		}
		merged, e := mergeGlobal(p, old, b, current, incoming)
		if e != nil {
			return nil, e
		}
		if exists && bytes.Equal(old, merged) {
			continue
		}
		if exists {
			backup := filepath.Join(tx, "backup", p)
			if e = os.MkdirAll(filepath.Dir(backup), 0700); e != nil {
				return nil, e
			}
			if e = filesystem.AtomicWriteFile(backup, old, 0600); e != nil {
				return nil, e
			}
		}
		if e = filesystem.AtomicWriteFile(candidate, merged, 0600); e != nil {
			return nil, e
		}
		changes = append(changes, change{Path: p, Backup: exists})
	}
	for i := range changes {
		changes[i].ID, err = transferFileID(filepath.Join(stage, changes[i].Path))
		if err != nil {
			return
		}
	}
	if err = filesystem.StoreJSON(filepath.Join(tx, "journal.json"), changes); err != nil {
		return
	}
	for _, ch := range changes {
		if err = ctx.Err(); err != nil {
			return
		}
		src, dst := filepath.Join(stage, ch.Path), filepath.Join(root, ch.Path)
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return
		}

		if ch.Backup {
			displaced := filepath.Join(tx, "displaced", ch.Path)
			if err = os.MkdirAll(filepath.Dir(displaced), 0700); err != nil {
				return
			}
			if err = publish(dst, displaced); err != nil {
				return
			}
		}
		err = publish(src, dst)
		if err != nil {
			return
		}
	}
	if err = ctx.Err(); err != nil {
		return
	}
	err = filesystem.AtomicWriteFile(filepath.Join(tx, "complete"), []byte("1"), 0600)
	return
}
