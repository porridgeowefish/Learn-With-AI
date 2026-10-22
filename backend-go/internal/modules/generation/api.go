// Package generation is the public boundary for durable API learning jobs.
package generation

import (
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	jobs "github.com/xmz14/lll/backend-go/internal/modules/generation/internal/jobs"
)

type Input = jobs.Input
type Job = jobs.Job
type Progress = jobs.Progress
type Coverage = jobs.Coverage
type Service = jobs.Service
type APIError = jobs.APIError
type MapProjection = jobs.MapProjection
type Usage = jobs.Usage
type MapCandidate = jobs.MapCandidate
type MapValidationIssue = jobs.MapValidationIssue

func New(gateway ai.Gateway) *Service                     { return jobs.New(gateway) }
func ReadMap(slug, version string) (MapProjection, error) { return jobs.ReadMap(slug, version) }
func UsageRecords(slug string) ([]Usage, error)           { return jobs.UsageRecords(slug) }
func ValidatePractice(body string, count int) error       { return jobs.ValidatePractice(body, count) }
func ErrorCode(err error) string                          { return jobs.ErrorCode(err) }
func ErrorStatus(err error) int                           { return jobs.ErrorStatus(err) }
