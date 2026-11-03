package workspaceservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/modules/preferences"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/treasures"
)

func mergeGlobal(p string, old, in []byte, current, incoming []ProjectEntry) ([]byte, error) {
	switch p {
	case "preferences.md":
		if len(old) > preferences.MaxBytes || len(in) > preferences.MaxBytes {
			return nil, errors.New("preferences exceed 256 KiB")
		}
		if len(old) == 0 || bytes.Equal(old, in) {
			return in, nil
		}
		if len(in) == 0 || bytes.Contains(append(append([]byte("\n\n"), old...), []byte("\n\n")...), append(append([]byte("\n\n"), in...), []byte("\n\n")...)) {
			return old, nil
		}
		if len(old)+len(in)+2 > preferences.MaxBytes {
			return nil, errors.New("combined preferences exceed 256 KiB")
		}
		return bytes.Join([][]byte{old, in}, []byte("\n\n")), nil
	case "projects/folders.json":
		var local, added projects.Layout
		if len(old) > 0 {
			if e := json.Unmarshal(old, &local); e != nil {
				return nil, e
			}
		}
		if e := json.Unmarshal(in, &added); e != nil {
			return nil, e
		}
		slugs := map[string]bool{}
		for _, pr := range incoming {
			slugs[pr.Slug] = true
		}
		ids := map[string]bool{}
		for _, f := range local.Folders {
			ids[f.ID] = true
		}
		for _, f := range added.Folders {
			if ids[f.ID] || f.ID == "" {
				return nil, fmt.Errorf("folder identity conflict: %s", f.ID)
			}
			ids[f.ID] = true
			for _, slug := range append(append([]string{}, f.SlugOrder...), f.MapProjectSlug) {
				if slug != "" && !slugs[slug] {
					return nil, fmt.Errorf("unresolved folder project: %s", slug)
				}
			}
			local.Folders = append(local.Folders, f)
		}
		return json.MarshalIndent(local, "", "  ")
	case "treasures/ledger.json":
		var a, b treasures.Ledger
		if len(old) > 0 {
			if e := json.Unmarshal(old, &a); e != nil {
				return nil, e
			}
		}
		if e := json.Unmarshal(in, &b); e != nil {
			return nil, e
		}
		if b.Version != 1 || len(old) > 0 && a.Version != 1 {
			return nil, errors.New("unsupported treasure ledger")
		}
		if b.Processed == nil || b.LastText == nil || b.Rewards == nil || len(old) > 0 && (a.Processed == nil || a.LastText == nil || a.Rewards == nil) {
			return nil, errors.New("invalid treasure ledger")
		}
		a.Version = 1
		if a.Rewards == nil {
			a.Rewards = []treasures.Reward{}
		}
		if a.Processed == nil {
			a.Processed = map[string]bool{}
		}
		if a.LastText == nil {
			a.LastText = map[string]string{}
		}
		for k, v := range b.Processed {
			if _, ok := a.Processed[k]; ok {
				return nil, fmt.Errorf("treasure completion collision: %s", k)
			}
			a.Processed[k] = v
		}
		for k, v := range b.LastText {
			if val, ok := a.LastText[k]; ok && val != v {
				return nil, fmt.Errorf("treasure text collision: %s", k)
			}
			a.LastText[k] = v
		}
		ids := map[string]bool{}
		for _, r := range a.Rewards {
			ids[r.ID] = true
		}
		for _, r := range b.Rewards {
			if ids[r.ID] || r.ID == "" {
				return nil, fmt.Errorf("treasure reward collision: %s", r.ID)
			}
			ids[r.ID] = true
			a.Rewards = append(a.Rewards, r)
		}
		a.WelcomeGranted = a.WelcomeGranted || b.WelcomeGranted
		a.Misses += b.Misses
		return json.MarshalIndent(a, "", "  ")
	}
	return nil, fmt.Errorf("unknown global record %s", p)
}
