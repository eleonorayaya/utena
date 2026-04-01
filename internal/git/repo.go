package git

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrRepoNotFound      = errors.New("repo not found")
	ErrRepoAlreadyExists = errors.New("repo already exists")
)

type Repo struct {
	gorm.Model
	Path     string `json:"path" gorm:"uniqueIndex"`
	FullName string `json:"full_name" gorm:"index"`
}
