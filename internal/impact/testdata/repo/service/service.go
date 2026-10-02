package service

import "github.com/magicdrive/ark/internal/impact/testdata/repo/repository"

// Create creates a new entity and persists it.
func Create(id int) error {
	return repository.Save(id)
}
