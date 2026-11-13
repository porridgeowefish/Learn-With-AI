package httpapi

type GuestModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func guestModelCatalog(active []Model) []GuestModel {
	models := make([]GuestModel, len(active))
	for i, model := range active {
		models[i] = GuestModel{ID: model.ID, Name: model.Name}
	}
	return models
}
