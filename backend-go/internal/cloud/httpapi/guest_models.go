package httpapi

import "strings"

type GuestModel struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Multiplier float64 `json:"multiplier,omitempty"`
}

func guestModelCatalog(active []Model) []GuestModel {
	var baseline float64
	for _, model := range active {
		identity := strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(model.Name))
		if identity == "kimik3.1" || strings.EqualFold(model.ID, "kimi-k3.1") {
			baseline = standardInputPrice(model.Rate)
			break
		}
	}
	models := make([]GuestModel, len(active))
	for i, model := range active {
		models[i] = GuestModel{ID: model.ID, Name: model.Name}
		if baseline > 0 {
			models[i].Multiplier = standardInputPrice(model.Rate) / baseline
		}
	}
	return models
}

func standardInputPrice(rate Rate) float64 {
	if rate.PerMillion != nil {
		return float64(rate.PerMillion.Input)
	}
	if rate.TokensPerPoint > 0 {
		return 1_000_000_000 / float64(rate.TokensPerPoint)
	}
	return float64(rate.Input) * 1_000_000
}
