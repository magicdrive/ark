package app

// UserService coordinates user operations.
type UserService struct {
	repo *Repository
}

// Create builds and stores a new user.
func (s *UserService) Create(name string) error {
	u := User{Name: name}
	found := s.repo.Find(u.ID)
	return found.Save()
}
