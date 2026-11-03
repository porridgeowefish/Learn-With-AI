package workspaceservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/modules/preferences"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const moveMarker = ".workspace-moved"

type moveRecord struct {
	Before                                      paths.Locator
	Target, Staging, Transaction, Digest, Phase string
}

type moveManifest struct {
	digest string
	bytes  uint64
}

func moveFiles(ctx context.Context, root, dest string) (moveManifest, error) {
	var manifest moveManifest
	sum := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil || rel == "." {
			return e
		}
		if rel == moveMarker || rel == "tmp" || rel == ".learn-with-ai.lock" || rel == ".learn-with-ai.window" || (rel == "browser-cache" || rel == "webview2") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace contains link: %s", rel)
		}
		if d.IsDir() {
			fmt.Fprintf(sum, "D%s\x00", rel)
			if dest != "" {
				return os.Mkdir(filepath.Join(dest, rel), 0700)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unsupported workspace entry: %s", rel)
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		h := sha256.New()
		var w io.Writer = h
		var out *os.File
		if dest != "" {
			out, e = os.OpenFile(filepath.Join(dest, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			w = io.MultiWriter(h, out)
		}
		n, e := io.Copy(w, cancelReader{ctx, f})
		if out != nil {
			e = errors.Join(e, out.Close())
		}
		if e != nil {
			return e
		}
		manifest.bytes += uint64(n)
		fmt.Fprintf(sum, "F%s\x00%d\x00%x", rel, n, h.Sum(nil))
		return nil
	})
	manifest.digest = hex.EncodeToString(sum.Sum(nil))
	return manifest, err
}
func insideMovePath(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator))
}

// MoveDestination selects the first available child without creating or overwriting it.
func MoveDestination(parent string) (string, error) {
	if !filepath.IsAbs(parent) {
		return "", fmt.Errorf("choose an absolute destination parent")
	}
	dir, err := os.Stat(parent)
	if err != nil {
		return "", err
	}
	if !dir.IsDir() {
		return "", fmt.Errorf("destination parent is not a directory")
	}
	for suffix := 1; ; suffix++ {
		name := "Learn-With-AI"
		if suffix > 1 {
			name = fmt.Sprintf("Learn-With-AI-%d", suffix)
		}
		target := filepath.Join(parent, name)
		_, err = os.Lstat(target)
		if os.IsNotExist(err) {
			return target, nil
		}
		if err != nil {
			return "", err
		}
	}
}
func Move(ctx context.Context, root, parent string) (moved string, err error) {
	if e := preferences.MigrateBackup(); e != nil {
		return "", e
	}
	before, e := paths.BeginMove(root)
	if e != nil {
		return "", e
	}
	if !filepath.IsAbs(parent) {
		return "", fmt.Errorf("choose an absolute destination parent")
	}
	actual, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	parent, e = filepath.EvalSymlinks(parent)
	if e != nil {
		return "", e
	}
	target, e := MoveDestination(parent)
	if e != nil {
		return "", e
	}
	if insideMovePath(actual, target) || insideMovePath(target, actual) {
		return "", fmt.Errorf("workspace and destination overlap")
	}
	if _, e = os.Lstat(target); !os.IsNotExist(e) {
		return "", fmt.Errorf("destination exists or unavailable: %s", target)
	}
	tx, e := paths.NewWorkspaceID()
	if e != nil {
		return "", e
	}
	staging := filepath.Join(parent, ".workspace-stage-"+tx)
	record := moveRecord{Before: before, Target: target, Staging: staging, Transaction: tx, Phase: "copy"}
	marker := filepath.Join(root, moveMarker)
	if _, e = os.Lstat(marker); !os.IsNotExist(e) {
		return "", fmt.Errorf("restart to recover the pending workspace relocation")
	}
	if e = paths.StoreJSON(marker, record); e != nil {
		return "", e
	}
	if e = os.Mkdir(staging, 0700); e != nil {
		return "", errors.Join(e, os.Remove(marker))
	}
	published := false
	defer func() {
		if !published {
			_, cleanupErr := retireMoveStage(record)
			if cleanupErr == nil {
				cleanupErr = os.Remove(marker)
			}
			err = errors.Join(err, cleanupErr)
		}
	}()
	// This is the first entry in an exclusively created directory. Write directly
	// so an interrupted initialization can only leave this one known filename.
	initialOwner, e := json.MarshalIndent(record, "", "  ")
	if e != nil {
		return "", e
	}
	owner, e := os.OpenFile(filepath.Join(staging, moveMarker), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	_, e = owner.Write(initialOwner)
	e = errors.Join(e, owner.Close())
	if e != nil {
		return "", e
	}
	source, e := moveFiles(ctx, root, "")
	if e != nil {
		return "", e
	}
	free, e := paths.AvailableBytes(parent)
	if e != nil {
		return "", e
	}
	if source.bytes > free {
		return "", fmt.Errorf("insufficient destination space: need %d bytes", source.bytes)
	}
	copied, e := moveFiles(ctx, root, staging)
	if e != nil {
		return "", e
	}
	check, e := moveFiles(ctx, staging, "")
	if e != nil || check != source || copied != source {
		return "", fmt.Errorf("workspace verification failed: %v", e)
	}
	record.Digest = source.digest
	record.Phase = "publish-target"
	for {
		target, e = MoveDestination(parent)
		if e != nil {
			return "", e
		}
		record.Target = target
		if e = paths.StoreJSON(marker, record); e != nil {
			return "", e
		}
		if e = paths.StoreJSON(filepath.Join(staging, moveMarker), record); e != nil {
			return "", e
		}
		if resolved, err := filepath.EvalSymlinks(parent); err != nil || resolved != parent {
			return "", fmt.Errorf("destination changed")
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		e = paths.PublishDirectory(staging, target)
		if e == nil {
			break
		}
		if _, collision := os.Lstat(target); collision == nil {
			continue
		}
		return "", e
	}
	published = true
	if e = paths.CommitMove(before, target, tx); e != nil {
		return "", e
	}
	return target, nil
}

// retireMoveStage authenticates the transaction before removing an unpublished copy.
func retireMoveStage(r moveRecord) (bool, error) {
	if r.Staging == "" {
		return false, nil
	}
	id, err := hex.DecodeString(r.Transaction)
	if err != nil || len(id) != 16 {
		return false, fmt.Errorf("invalid relocation transaction")
	}
	expected := filepath.Join(filepath.Dir(r.Target), ".workspace-stage-"+r.Transaction)
	if r.Staging != expected || !filepath.IsAbs(expected) || insideMovePath(r.Before.Root, expected) || insideMovePath(expected, r.Before.Root) {
		return false, fmt.Errorf("invalid relocation staging path")
	}
	info, err := os.Lstat(expected)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	actual, err := filepath.EvalSymlinks(expected)
	if err != nil || actual != expected || !info.IsDir() {
		return false, fmt.Errorf("relocation staging path changed: %s", expected)
	}
	var owner moveRecord
	marker := filepath.Join(expected, moveMarker)
	info, err = os.Lstat(marker)
	if os.IsNotExist(err) {
		// mkdir may have completed before the ownership record. Only an empty directory can be removed.
		return true, os.Remove(expected)
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("invalid relocation staging marker")
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		return false, err
	}
	if err = json.Unmarshal(raw, &owner); err != nil {
		// Before owner initialization completes no learner data is copied. Only
		// retire a torn prefix of this transaction's record in an otherwise empty directory.
		want, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return false, e
		}
		items, e := os.ReadDir(expected)
		if e != nil {
			return false, e
		}
		if r.Phase != "copy" || len(items) != 1 || !bytes.HasPrefix(want, raw) {
			return false, fmt.Errorf("unrecognized relocation staging contents: %s", expected)
		}
		if e = os.Remove(marker); e != nil {
			return false, e
		}
		return true, os.Remove(expected)
	}
	if owner.Before != r.Before || owner.Transaction != r.Transaction || owner.Staging != expected {
		return false, fmt.Errorf("relocation staging ownership mismatch")
	}
	// Keep the ownership record until every data entry is gone, so cleanup itself is restartable.
	items, err := os.ReadDir(expected)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Name() != moveMarker {
			if err = os.RemoveAll(filepath.Join(expected, item.Name())); err != nil {
				return false, err
			}
		}
	}
	if err = os.Remove(marker); err != nil {
		return false, err
	}
	return true, os.Remove(expected)
}

// RecoverMove verifies a published target before its first writable opening.
func RecoverMove(root string) error {
	b, e := os.ReadFile(filepath.Join(root, moveMarker))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var r moveRecord
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	l, e := paths.MoveBaseline(root)
	if e != nil {
		return e
	}
	if root == r.Target && l.ID == r.Before.ID && l.Transaction == r.Transaction {
		h, e := moveFiles(context.Background(), root, "")
		if e != nil || h.digest != r.Digest {
			return fmt.Errorf("relocated workspace verification failed: %v", e)
		}
		return os.Remove(filepath.Join(root, moveMarker))
	}
	if l != r.Before {
		return fmt.Errorf("workspace migration identity mismatch")
	}
	retired, e := retireMoveStage(r)
	if e != nil {
		return e
	}
	if retired {
		return os.Remove(filepath.Join(root, moveMarker))
	}
	if _, e = os.Lstat(r.Target); os.IsNotExist(e) {
		return os.Remove(filepath.Join(root, moveMarker))
	}
	if e != nil {
		return e
	}
	other, e := os.ReadFile(filepath.Join(r.Target, moveMarker))
	if e != nil || string(other) != string(b) {
		return fmt.Errorf("unrecognized relocation target: %s", r.Target)
	}
	for _, p := range []string{root, r.Target} {
		h, e := moveFiles(context.Background(), p, "")
		if e != nil || h.digest != r.Digest {
			return fmt.Errorf("relocation content changed: %s", p)
		}
	}
	if e = paths.CommitMove(l, r.Target, r.Transaction); e != nil {
		return e
	}
	return errReopenWorkspace
}
