package workspaceservice

import (
	"fmt"
	"path"
	"strings"
)

// The manifest's kind is advisory. Match each record to the receiver's own
// portable formats before accepting it, including records inside JSONL streams.
var requiredFields = map[string]string{
	"projects/folders.json": "folders", "treasures/ledger.json": "version processed lastText rewards",
	"state.json": "id slug title", "unit.json": "unitId conversationId projectSlug",
	"learning-scope.json": "schemaVersion status title", "learning-plan.json": "schemaVersion items",
	"discipline-topics.json": "schemaVersion topics", "map/current.json": "versionId",
	"map/versions/*/manifest.json": "versionId", "map/versions/*/topics.json": "topics",
	"conversation/conversation.json": "unitId id", "conversation/events.jsonl": "eventId seq type data",
	"conversation/learning-mode.json": "learningMode version guideline",
	"conversation/usage.jsonl":        "responseId inputTokens outputTokens", "conversation/note-usage.jsonl": "responseId inputTokens outputTokens",
	"assets/notes/state.json": "schemaVersion", "assets/notes/entries/*.json": "id content",
	"assets/*/asset.json": "key assetId currentVersionId", "assets/*/versions/*/version.json": "versionId assetId",
	"assets/review/annotations.jsonl": "eventId annotationId seq type", "assets/practice/results/*.json": "versionId score",
	"assets/feynman/sessions/*.json": "sessionId messages", "assets/notes/jobs/*.json": "id status",
	"progress/events.jsonl": "id sourceType sourceId", "progress/summary.json": "total bySource",
	"generation/jobs/*.json": "id status", "generation/inputs/*/input.json": "system messages", "generation/usage.jsonl": "requestId jobId",
	"sources/*/source.json": "sourceId currentRevisionId status", "sources/*/revisions/*/revision.json": "sourceId revisionId",
	"sources/*/revisions/*/derived/current.json": "derivedVersionId", "sources/*/revisions/*/derived/versions/*/manifest.json": "sourceRevisionId derivedVersionId files",
	"sources/*/revisions/*/derived/versions/*/structure.json": "units parserVersion", "sources/*/revisions/*/derived/structure.json": "units parserVersion",
	"sources/*/revisions/*/structure/jobs/*.json": "id status", "sources/*/revisions/*/structure/automatic-budget.json": "requests pages",
}

func checkShape(p string, v map[string]any) error {
	a := strings.SplitN(p, "/", 3)
	if len(a) == 3 && a[0] == "projects" {
		p = a[2]
	}
	for pat, fields := range requiredFields {
		if ok, _ := path.Match(pat, p); ok {
			for _, field := range strings.Fields(fields) {
				if _, ok := v[field]; !ok {
					return fmt.Errorf("missing %s in %s", field, p)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("missing record validator for %s", p)
}
