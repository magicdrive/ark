package service

import "fmt"

// UserService handles user operations.
type UserService struct{}

// Create creates a new user.
func (s *UserService) Create(name string) error {
	u := NewUser(name)
	fmt.Println(u)
	return s.Save(u)
}

// Save persists a user.
func (s *UserService) Save(u interface{}) error {
	return nil
}

// NewUser constructs a user value.
func NewUser(name string) interface{} {
	return name
}

// List returns all users.
func List() []interface{} {
	return nil
}
