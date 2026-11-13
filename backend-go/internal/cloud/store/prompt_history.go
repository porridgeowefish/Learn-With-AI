package store

import (
	"context"
	"encoding/json"
)

type PromptPublication struct {
	Version     int64  `json:"version"`
	PublishedAt int64  `json:"publishedAt"`
	PublishedBy string `json:"publishedBy"`
}

func (s *Store) PromptHistory(ctx context.Context, offset int) ([]PromptPublication, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version,created_at,actor FROM prompt_config_version ORDER BY version DESC LIMIT 50 OFFSET ?`, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PromptPublication{}
	for rows.Next() {
		var entry PromptPublication
		if err := rows.Scan(&entry.Version, &entry.PublishedAt, &entry.PublishedBy); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *Store) InitialExperimentConfig(ctx context.Context, id string) (ModelConfigVersion, error) {
	return readModelConfig(s.db.QueryRowContext(ctx, `SELECT version,content,created_at FROM prompt_config_version
		WHERE json_extract(content,'$.comparison.id')=? ORDER BY version LIMIT 1`, id))
}

type PublishedComparison struct {
	ID, Model     string
	Name          string
	Percent       int
	BaseVersion   int64
	CreatedAt     int64
	PublishedAt   int64
	EndedAt       int64
	Active        bool
	Config        json.RawMessage
	BaseResources json.RawMessage
}

// PublishedComparisons indexes the existing immutable snapshots. Older sampled
// experiments still appear even if no saved configuration was available.
func (s *Store) PublishedComparisons(ctx context.Context, id string, includeResources bool) ([]PublishedComparison, error) {
	rows, err := s.db.QueryContext(ctx, `WITH active(version) AS (SELECT MAX(version) FROM prompt_config_version),
		published AS (
			SELECT json_extract(content,'$.comparison.id') AS id,MIN(version) AS first,MAX(version) AS latest
			FROM prompt_config_version WHERE json_type(content,'$.comparison.id')='text' GROUP BY id
		), legacy AS (
			SELECT experiment AS id,MAX(model) AS model,MIN(created_at) AS created_at FROM answer_comparison
			WHERE experiment<>'' GROUP BY experiment
		), ids(id) AS (SELECT id FROM published UNION SELECT id FROM legacy)
		SELECT ids.id,COALESCE(json_extract(latest.content,'$.comparison.model'),legacy.model,''),
			COALESCE(NULLIF(TRIM(json_extract(latest.content,'$.comparison.name')),''),ids.id),
			COALESCE(json_extract(latest.content,'$.comparison.percent'),0),
			COALESCE(first.version,0),COALESCE(first.created_at,legacy.created_at,0),COALESCE(latest.created_at,0),
			COALESCE((SELECT created_at FROM prompt_config_version WHERE version>p.latest ORDER BY version LIMIT 1),0),
			COALESCE(p.latest=active.version,0),
			CASE WHEN ? THEN COALESCE(json_extract(latest.content,'$.comparison'),'') ELSE '' END,
			CASE WHEN ? THEN COALESCE(json_extract(first.content,'$.resources'),'') ELSE '' END
		FROM ids CROSS JOIN active LEFT JOIN published p ON p.id=ids.id LEFT JOIN legacy ON legacy.id=ids.id
		LEFT JOIN prompt_config_version first ON first.version=p.first LEFT JOIN prompt_config_version latest ON latest.version=p.latest
		WHERE (?='' OR ids.id=?) ORDER BY p.latest DESC,legacy.created_at DESC,ids.id`, includeResources, includeResources, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PublishedComparison{}
	for rows.Next() {
		var entry PublishedComparison
		var config, base string
		if err := rows.Scan(&entry.ID, &entry.Model, &entry.Name, &entry.Percent, &entry.BaseVersion, &entry.CreatedAt, &entry.PublishedAt, &entry.EndedAt, &entry.Active, &config, &base); err != nil {
			return nil, err
		}
		entry.Config, entry.BaseResources = json.RawMessage(config), json.RawMessage(base)
		result = append(result, entry)
	}
	return result, rows.Err()
}
