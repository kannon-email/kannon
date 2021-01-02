package db

import (
	"fmt"

	"github.com/google/uuid"
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
	ID          uint   `gorm:"primarykey"`
	TemplateID  string `gorm:"index,unique"`
	HTML        string
	Type        TemplateType `gorm:"default:tmp"`
	Domain      string
	SendingPool []SendingPool
}

// BeforeCreate hooks build UID of the template
func (t *Template) BeforeCreate(tx *gorm.DB) (err error) {
	t.TemplateID = fmt.Sprintf("template/%v@%v", uuid.New().String(), t.Domain)
	return nil
}
