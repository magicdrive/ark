package app

import "fmt"

// Repository stores users.
type Repository struct{}

// Find looks up a user by id.
func (r *Repository) Find(id int64) *User {
	fmt.Println("finding", id)
	return &User{ID: id}
}
