package workspaceservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Validate module pointers against staged files before publishing a project.
// Metadata may select another object in the archive only within its own namespace.
func validateCandidate(ctx context.Context, stage string) error {
	dirs, e := os.ReadDir(filepath.Join(stage, "projects"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		base := filepath.Join(stage, "projects", d.Name())
		if e = validateProjectFiles(ctx, base); e != nil {
			return fmt.Errorf("%s: %w", d.Name(), e)
		}
	}
	return nil
}
func validateProjectFiles(ctx context.Context, base string) error {
	require := func(rel string) error {
		i, e := os.Stat(filepath.Join(base, filepath.FromSlash(rel)))
		if e != nil {
			return e
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("invalid record %s", rel)
		}
		return nil
	}
	// Only source records with independent text should remain visible after import.
	visible := map[string]bool{}
	remove := []string{}
	e := filepath.WalkDir(base, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		a := strings.Split(filepath.ToSlash(rel), "/")
		file := a[len(a)-1]
		var v map[string]any
		if e = loadJSON(p, &v); e != nil {
			return e
		}
		val := func(k string) string { s, _ := v[k].(string); return s }
		switch {
		case a[0] == "sources" && file == "source.json":
			if val("sourceId") != a[1] {
				return errors.New("source identity mismatch")
			}
			src := filepath.Dir(p)
			rev := val("currentRevisionId")
			if v["permanentlyDeletedAt"] == nil {
				if e = require("sources/" + a[1] + "/revisions/" + rev + "/revision.json"); e != nil {
					return e
				}
			}
			var readable bool
			if e = filepath.WalkDir(src, func(name string, d fs.DirEntry, e error) error {
				if e == nil && !d.IsDir() && filepath.Base(name) == "content.md" {
					i, err := d.Info()
					if err != nil {
						return err
					}
					readable = readable || i.Size() > 0
				}
				return e
			}); e != nil {
				return e
			}
			if readable {
				visible[a[1]] = true
			} else {
				remove = append(remove, src)
			}
		case a[0] == "sources" && file == "revision.json":
			if val("sourceId") != a[1] || val("revisionId") != a[3] {
				return errors.New("revision identity mismatch")
			}
		case a[0] == "sources" && file == "current.json":
			if e = require(strings.Join(a[:len(a)-1], "/") + "/versions/" + val("derivedVersionId") + "/manifest.json"); e != nil {
				return e
			}
		case a[0] == "sources" && (file == "manifest.json"):
			if val("derivedVersionId") != a[len(a)-2] || val("sourceRevisionId") != a[3] {
				return errors.New("derived manifest identity mismatch")
			}
			refs, _ := v["files"].([]any)
			for _, n := range refs {
				ref, _ := n.(map[string]any)
				name, _ := ref["path"].(string)
				prefix := "derived/versions/" + val("derivedVersionId") + "/"
				if name != prefix+"content.md" && name != prefix+"structure.json" {
					return errors.New("invalid derived reference")
				}
				data, e := os.ReadFile(filepath.Join(base, strings.Join(a[:4], "/"), name))
				if e != nil {
					return e
				}
				if ref["sha256"] != "sha256:"+digest(data) {
					return fmt.Errorf("invalid derived digest %s", name)
				}
			}
		case a[0] == "assets" && file == "asset.json":
			if val("key") != a[1] || val("assetId") == "" {
				return errors.New("asset identity mismatch")
			}
			for _, n := range []string{"version.json", "content.md"} {
				if e = require("assets/" + a[1] + "/versions/" + val("currentVersionId") + "/" + n); e != nil {
					return e
				}
			}
		case a[0] == "assets" && file == "version.json":
			if val("versionId") != a[len(a)-2] {
				return errors.New("asset version identity mismatch")
			}
			return require(strings.Join(a[:len(a)-1], "/") + "/content.md")
		case a[0] == "map" && file == "topics.json":
			var cat projects.Catalog
			if e = loadJSON(p, &cat); e != nil {
				return e
			}
			if e = projects.ValidateCatalog(&cat); e != nil {
				return e
			}
		case (a[0] == "generation" || a[0] == "assets" || a[0] == "sources") && len(a) > 2 && a[len(a)-2] == "jobs":
			if val("id") != strings.TrimSuffix(file, ".json") {
				return errors.New("job identity mismatch")
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, p := range remove {
		if e = os.RemoveAll(p); e != nil {
			return e
		}
	}
	if e = filterCitations(filepath.Join(base, "conversation", "events.jsonl"), visible); e != nil {
		return e
	}
	return teacher.PrepareImportedConversation(base)
}
func filterCitations(p string, visible map[string]bool) error {
	b, e := os.ReadFile(p)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	changed := false
	var out bytes.Buffer
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev map[string]json.RawMessage
		if e = json.Unmarshal(line, &ev); e != nil {
			return e
		}
		var data map[string]json.RawMessage
		if e = json.Unmarshal(ev["data"], &data); e != nil {
			return e
		}
		modified := false
		if refs, ok := data["attachmentRefs"]; ok {
			var a []string
			if e = json.Unmarshal(refs, &a); e != nil {
				return e
			}
			keep := a[:0]
			for _, id := range a {
				if visible[id] {
					keep = append(keep, id)
				}
			}
			if len(keep) != len(a) {
				modified = true
				data["attachmentRefs"], _ = json.Marshal(keep)
			}
		}
		for _, key := range []string{"blocks", "mediaBlocks"} {
			if raw, ok := data[key]; ok {
				var a []json.RawMessage
				if e = json.Unmarshal(raw, &a); e != nil {
					return e
				}
				keep := a[:0]
				for _, item := range a {
					var block struct{ Type, ArtifactRef string }
					if e = json.Unmarshal(item, &block); e != nil {
						return e
					}
					if block.Type != "attachment" || visible[block.ArtifactRef] {
						keep = append(keep, item)
					}
				}
				if len(keep) != len(a) {
					modified = true
					data[key], _ = json.Marshal(keep)
				}
			}
		}
		for _, k := range []string{"sourceCitations", "readingCoverage", "citations", "coverage"} {
			var arr []json.RawMessage
			if json.Unmarshal(data[k], &arr) != nil {
				continue
			}
			keep := arr[:0]
			for _, item := range arr {
				var ref struct {
					SourceID string `json:"sourceId"`
				}
				if json.Unmarshal(item, &ref) != nil || ref.SourceID == "" || visible[ref.SourceID] {
					keep = append(keep, item)
				}
			}
			if len(keep) != len(arr) {
				data[k], _ = json.Marshal(keep)
				modified = true
			}
		}
		if modified {
			changed = true
			ev["data"], _ = json.Marshal(data)
			line, _ = json.Marshal(ev)
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	if changed {
		return filesystem.AtomicWriteFile(p, out.Bytes(), 0600)
	}
	return nil
}
