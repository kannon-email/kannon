package meta

import "time"

type Meta struct {
	Title      string
	Desciption string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewMeta(title, description string) Meta {
	return Meta{
		Title:      title,
		Desciption: description,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func (m *Meta) Update() {
	m.UpdatedAt = time.Now()
}
