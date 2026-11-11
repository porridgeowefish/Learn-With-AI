package httpapi

import (
	"errors"
	"strings"
	"unicode"
)

type GuestModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func guestModelCatalog(configured []GuestModel, active []Model) ([]GuestModel, error) {
	if configured == nil {
		if len(active) == 0 {
			return []GuestModel{
				{ID: "glm-5.3", Name: "GLM-5.3"},
				{ID: "deepseek-flash", Name: "DeepSeek V4.1 Flash"},
			}, nil
		}
		models := make([]GuestModel, len(active))
		for i, model := range active {
			models[i] = GuestModel{ID: model.ID, Name: model.Name}
		}
		return models, nil
	}
	if len(configured) > 64 {
		return nil, errors.New("guest model catalog exceeds capacity")
	}
	catalog := make([]GuestModel, 0, len(configured))
	seen := make(map[string]bool, len(configured))
	for _, model := range configured {
		model.Name = strings.TrimSpace(model.Name)
		if !identifier.MatchString(model.ID) || seen[model.ID] || model.Name == "" || len(model.Name) > 160 ||
			strings.IndexFunc(model.Name, unicode.IsControl) >= 0 {
			return nil, errors.New("invalid or duplicate guest model")
		}
		catalog = append(catalog, model)
		seen[model.ID] = true
	}
	return catalog, nil
}
