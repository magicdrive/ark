package userservice

// UserRepository persists users.
type UserRepository struct{}

// Save stores a user.
func (r *UserRepository) Save(u *User) error {
	if u == nil {
		return errorf("nil user")
	}
	return nil
}

// Find retrieves a user by ID.
func (r *UserRepository) Find(id string) (*User, error) {
	return nil, nil
}
