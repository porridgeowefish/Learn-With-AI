package workspaceservice

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/activity"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

const fileLimit = 64 << 20
const archiveLimit = 512 << 20
const entryLimit = 30000

type record struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	SHA      string `json:"sha256"`
	Required bool   `json:"required"`
}
type manifest struct {
	Format   string    `json:"format"`
	Version  int       `json:"formatVersion"`
	Producer string    `json:"producerVersion"`
	ID       string    `json:"workspaceId"`
	Created  time.Time `json:"createdAt"`
	Excluded int       `json:"excludedAttachments"`
	Files    []record  `json:"files"`
	Digest   string    `json:"digest"`
}

var ProducerVersion = "development"
var rules = map[string][]string{"projects": projects.TransferPaths(), "teacher": teacher.TransferPaths(), "sources": sources.TransferPaths(), "generation": generation.TransferPaths(), "assets": assets.TransferPaths(), "activity": activity.TransferPaths()}

func kindOf(p string) string {
	switch p {
	case "projects/folders.json":
		return "folders"
	case "preferences.md":
		return "preferences"
	case "treasures/ledger.json":
		return "treasures"
	}
	s := strings.SplitN(p, "/", 3)
	if len(s) != 3 || s[0] != "projects" || !projects.ValidateSlug(s[1]) {
		return ""
	}
	for kind, patterns := range rules {
		for _, pat := range patterns {
			if ok, _ := path.Match(pat, s[2]); ok {
				return kind
			}
		}
	}
	return ""
}
func safeName(p string) bool {
	if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, "\\:<>\"|?*") || !utf8.ValidString(p) {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") || !filepath.IsLocal(s) {
			return false
		}
		// Archives produced on Unix must also extract safely on Windows.
		base, _, _ := strings.Cut(s, ".")
		base = strings.ToUpper(strings.TrimRight(base, " "))
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
			for _, digit := range "123456789¹²³" {
				if base[3:] == string(digit) {
					return false
				}
			}
		}
		for _, r := range s {
			if r < 32 {
				return false
			}
		}
	}
	return true
}
func skipOperational(p string) bool {
	first := strings.SplitN(p, "/", 2)[0]
	switch first {
	case "workspace.json", "config.local.json", "webview2", "browser-cache", "tmp", ".learn-with-ai.window", ".learn-with-ai.lock", ".workspace-moved", "visual-plugins.json":
		return true
	}
	return strings.Contains(p, "/derived/retrieval/")
}
func attachment(p string) bool {
	return strings.Contains(p, "/conversation/attachments/") || strings.Contains(p, "/sources/") && (strings.Contains(p, "/original/") || strings.Contains(p, "/images/") || strings.Contains(p, "/pages/"))
}
func digest(b []byte) string        { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func fileDigest(rs []record) string { b, _ := json.Marshal(rs); return digest(b) }

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancelReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
func Export(ctx context.Context, root string, out io.Writer) error {
	meta, e := filesystem.ReadMetadata(root)
	if e != nil {
		return e
	}
	if _, _, e = entries(root); e != nil {
		return e
	}
	z := zip.NewWriter(out)
	m := manifest{Format: "lll-learning-export", Version: 1, Producer: ProducerVersion, ID: meta.ID, Created: time.Now().UTC(), Files: []record{}}
	var total int64
	e = filepath.WalkDir(root, func(f string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		rel, e := filepath.Rel(root, f)
		if e != nil {
			return e
		}
		p := filepath.ToSlash(rel)
		if p == "preferences.md" || p == "learning-profile.json" || strings.HasPrefix(p, ".learning-profile-") || skipOperational(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported link %s", p)
		}
		if d.IsDir() {
			return nil
		}
		if attachment(p) {
			m.Excluded++
			return nil
		}
		if p == "folders.json" {
			if _, e = os.Stat(filepath.Join(root, "projects", "folders.json")); e == nil {
				return nil
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
			p = "projects/folders.json"
		}
		k := kindOf(p)
		if k == "" || !safeName(p) || !d.Type().IsRegular() {
			return fmt.Errorf("unclassified workspace file %s", p)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		total += info.Size()
		if info.Size() > fileLimit || total > archiveLimit || len(m.Files) >= entryLimit {
			return errors.New("archive limit exceeded")
		}
		src, e := os.Open(f)
		if e != nil {
			return e
		}
		defer src.Close()
		w, e := z.Create(p)
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(cancelReader{ctx, src}, info.Size()+1))
		if e != nil {
			return e
		}
		originalSize := int64(len(data))
		projected, e := projectRecord(p, data)
		if e != nil {
			return fmt.Errorf("%s: %w", p, e)
		}
		if generationInput(p) {
			data = projected
		}
		if _, e = w.Write(data); e != nil {
			return e
		}
		n := int64(len(data))
		after, e := src.Stat()
		if e != nil {
			return e
		}
		if originalSize != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("changed during export: %s", p)
		}
		m.Files = append(m.Files, record{p, k, n, digest(data), true})
		return nil
	})
	if e != nil {
		return e
	}
	m.Digest = fileDigest(m.Files)
	w, e := z.Create("manifest.json")
	if e != nil {
		return e
	}
	if e = json.NewEncoder(w).Encode(m); e != nil {
		return e
	}
	return z.Close()
}
func readArchive(ctx context.Context, zipPath, stage string) error {
	z, e := zip.OpenReader(zipPath)
	if e != nil {
		return e
	}
	defer z.Close()
	if len(z.File) > entryLimit+1 {
		return errors.New("too many files")
	}
	files := map[string]*zip.File{}
	seen := map[string]string{}
	var total uint64
	for _, f := range z.File {
		if !safeName(f.Name) || !f.Mode().IsRegular() || f.UncompressedSize64 > fileLimit {
			return fmt.Errorf("unsafe ZIP member %q", f.Name)
		}
		for p := f.Name; p != "."; p = path.Dir(p) {
			key := strings.ToLower(p)
			if old, ok := seen[key]; ok && old != p {
				return fmt.Errorf("case collision: %s", p)
			}
			seen[key] = p
		}
		if files[f.Name] != nil {
			return fmt.Errorf("duplicate member %s", f.Name)
		}
		files[f.Name] = f
		total += f.UncompressedSize64
		if total > archiveLimit {
			return errors.New("archive too large")
		}
	}
	read := func(f *zip.File) ([]byte, error) {
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		return io.ReadAll(io.LimitReader(cancelReader{ctx, r}, fileLimit+1))
	}
	mf := files["manifest.json"]
	if mf == nil || mf.UncompressedSize64 > 8<<20 {
		return errors.New("invalid manifest")
	}
	b, e := read(mf)
	if e != nil {
		return e
	}
	var m manifest
	if json.Unmarshal(b, &m) != nil || m.Format != "lll-learning-export" || m.Version != 1 || m.Digest != fileDigest(m.Files) || len(m.Files) != len(files)-1 {
		return errors.New("unsupported or invalid manifest")
	}
	delete(files, "manifest.json")
	for _, r := range m.Files {
		f := files[r.Path]
		if f == nil || !r.Required || kindOf(r.Path) == "" || kindOf(r.Path) != r.Kind || uint64(r.Size) != f.UncompressedSize64 {
			return fmt.Errorf("invalid manifest entry %s", r.Path)
		}
		delete(files, r.Path)
		b, e := read(f)
		if e != nil {
			return e
		}
		if digest(b) != r.SHA {
			return fmt.Errorf("checksum mismatch: %s", r.Path)
		}
		if r.Path == "preferences.md" {
			continue // Legacy ZIP member has passed the integrity checks.
		}
		b, e = projectRecord(r.Path, b)
		if e != nil {
			return fmt.Errorf("%s: %w", r.Path, e)
		}
		dst := filepath.Join(stage, filepath.FromSlash(r.Path))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(dst, b, 0600); e != nil {
			return e
		}
	}
	return nil
}
func projectRecord(p string, b []byte) ([]byte, error) {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return nil, errors.New("invalid text")
	}
	if !(strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".jsonl")) {
		return b, nil
	}
	changed := false
	var out bytes.Buffer
	lines := [][]byte{b}
	stream := strings.HasSuffix(p, ".jsonl")
	if stream {
		lines = bytes.Split(b, []byte("\n"))
	}
	for _, line := range lines {
		if stream && len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var v map[string]any
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if !json.Valid(line) || dec.Decode(&v) != nil || v == nil {
			return nil, errors.New("invalid record")
		}
		if e := checkShape(p, v); e != nil {
			return nil, e
		}
		before, _ := json.Marshal(v)
		if e := sanitize(p, v); e != nil {
			return nil, e
		}
		after, _ := json.Marshal(v)
		if !bytes.Equal(before, after) {
			changed = true
		}
		if e := json.NewEncoder(&out).Encode(v); e != nil {
			return nil, e
		}
	}
	if !changed {
		return b, nil
	}
	return out.Bytes(), nil
}
