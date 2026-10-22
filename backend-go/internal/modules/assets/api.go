// Package assets exposes the public boundary for learning assets and annotations.
package assets

import (
	"github.com/xmz14/lll/backend-go/internal/modules/assets/internal/annotationstore"
	"github.com/xmz14/lll/backend-go/internal/modules/assets/internal/assetstore"
)

const (
	SchemaVersion = assetstore.SchemaVersion

	StateOpen     = annotationstore.StateOpen
	StateAsked    = annotationstore.StateAsked
	StateResolved = annotationstore.StateResolved
	StateDeleted  = annotationstore.StateDeleted
)

type (
	Meta            = assetstore.Meta
	Version         = assetstore.Version
	Asset           = assetstore.Asset
	ConflictError   = assetstore.ConflictError
	Store           = assetstore.Store
	CandidateInput  = assetstore.CandidateInput
	CandidateResult = assetstore.CandidateResult

	PracticeResult   = assetstore.PracticeResult
	PracticeItemMark = assetstore.PracticeItemMark
	FeynmanSession   = assetstore.FeynmanSession
	FeynmanMessage   = assetstore.FeynmanMessage

	State           = annotationstore.State
	Anchors         = annotationstore.Anchors
	Message         = annotationstore.Message
	Ask             = annotationstore.Ask
	Annotation      = annotationstore.Annotation
	CreateInput     = annotationstore.CreateInput
	Event           = annotationstore.Event
	AnnotationStore = annotationstore.Store
)

var (
	ErrInvalidPracticeResult  = assetstore.ErrInvalidPracticeResult
	ErrPracticeRoundNotFound  = assetstore.ErrPracticeRoundNotFound
	ErrFeynmanSessionNotFound = assetstore.ErrFeynmanSessionNotFound
)

func New(slug string) (*Store, error) { return assetstore.New(slug) }

func CoreKeys() []string { return assetstore.CoreKeys() }

func NewAnnotations(slug string) (*AnnotationStore, error) {
	return annotationstore.New(slug)
}
