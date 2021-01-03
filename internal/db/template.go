package db

import (
	"fmt"

	"gopkg.in/lucsky/cuid.v1"
	"gorm.io/gorm"
)

// TemplateType type of a template struct
type TemplateType string

const (
	// TemplateTypeTmp temporary template, it will deleted after sent
	TemplateTypeTmp TemplateType = "tmp"

	// TemplateTypePermanent permanent template, it can be used
	TemplateTypePermanent TemplateType = "permanent"
)

// Template represent an HTML template to send
type Template struct {
	ID         uint         `gorm:"primarykey"`
	TemplateID string       `gorm:"index,unique"`
	Domain     string       `gorm:"index"`
	Type       TemplateType `gorm:"default:tmp"`
	HTML       string
}

// BeforeCreate hooks build UID of the template
func (t *Template) BeforeCreate(tx *gorm.DB) (err error) {
	if t.Type == TemplateTypePermanent {
		t.TemplateID = fmt.Sprintf("template-%v@%v", cuid.New(), t.Domain)
	} else {
		t.TemplateID = fmt.Sprintf("tmp-template-%v@%v", cuid.New(), t.Domain)
	}
	return nil
}
