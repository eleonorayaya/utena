package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrRepoNotFound      = errors.New("repo not found")
	ErrRepoAlreadyExists = common.NewConflict("repo already exists")
)

type Repo struct {
	gorm.Model
	Path     string `json:"path" gorm:"uniqueIndex"`
	FullName string `json:"full_name" gorm:"index"`
}

func (r *Repo) OwnerAndName() (string, string, error) {
	parts := strings.SplitN(r.FullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo full name: %s", r.FullName)
	}
	return parts[0], parts[1], nil
}
