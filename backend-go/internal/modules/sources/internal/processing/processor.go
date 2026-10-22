// Package processing owns asynchronous local extraction and restart recovery.
package processing

import (
	"context"
	"fmt"
	"sync"
	"time"

	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/enhancement"
	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/parser"
	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/store"
)

type Processor struct {
	OnUpdated func(string, store.Source)
	notify    chan struct{}
	done      chan struct{}
	cancel    context.CancelFunc
	once      sync.Once
}

func New(onUpdated func(string, store.Source)) *Processor {
	return &Processor{OnUpdated: onUpdated, notify: make(chan struct{}, 1), done: make(chan struct{})}
}
func (p *Processor) Start(ctx context.Context) {
	p.once.Do(func() { ctx, p.cancel = context.WithCancel(ctx); go p.loop(ctx) })
}
func (p *Processor) Notify() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}
func (p *Processor) Close() {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
}
func (p *Processor) loop(ctx context.Context) {
	defer close(p.done)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		p.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-p.notify:
		case <-ticker.C:
		}
	}
}
func (p *Processor) drain(ctx context.Context) {
	units, err := projects.IndexAll()
	if err != nil {
		return
	}
	for _, unit := range units {
		if ctx.Err() != nil {
			return
		}
		if unit.ProjectType != projects.ProjectTypeSystemLearning && unit.ProjectType != projects.ProjectTypeDisciplineMap {
			continue
		}
		st, err := store.New(unit.Slug)
		if err != nil {
			continue
		}
		files, err := st.List()
		if err != nil {
			continue
		}
		for i := len(files) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				return
			}
			source := files[i]
			if source.Status != "stored" && source.Status != "processing" {
				continue
			}
			if source.Status == "stored" {
				if _, err = st.BeginParse(source.SourceID); err != nil {
					continue
				}
			}
			source, err = Process(ctx, st, source.SourceID)
			if err == nil && p.OnUpdated != nil {
				p.OnUpdated(unit.Slug, source)
			}
			if err == nil && (source.Status == "ready" || source.FailureCode == "no_extractable_text") {
				enhancement.NotifyAutomatic(unit.Slug, st, source.SourceID)
			}
		}
	}
}
func extract(ctx context.Context, path, name string) (result parser.Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parser panic: %v", recovered)
		}
	}()
	return parser.Extract(ctx, path, name)
}
func Process(ctx context.Context, st *store.Store, sourceID string) (store.Source, error) {
	source, revision, path, err := st.OriginalPath(sourceID)
	if err != nil {
		if ctx.Err() == nil && source.Status == "processing" && revision.RevisionID != "" {
			return st.FailParse(sourceID, revision.RevisionID, "parse_failed")
		}
		return source, err
	}
	deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, parseErr := extract(deadline, path, revision.Original.Filename)
	if ctx.Err() != nil {
		return source, ctx.Err()
	}
	if parseErr != nil {
		return st.FailParse(sourceID, revision.RevisionID, parser.ErrorCode(parseErr))
	}
	return st.CommitStructured(sourceID, revision.RevisionID, result.Parser, result.Content, result.Truncated, result.Structure)
}
