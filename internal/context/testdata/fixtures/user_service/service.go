package userservice

// UserService handles user business logic.
type UserService struct {
	repo *UserRepository
}

// NewUserService creates a UserService.
func NewUserService(repo *UserRepository) *UserService {
	return &UserService{repo: repo}
}

// Create validates and persists a new user.
func (s *UserService) Create(u *User) error {
	if err := ValidateUser(u); err != nil {
		return err
	}
	return s.repo.Save(u)
}

// UnrelatedService has nothing to do with users.
type UnrelatedService struct{}

// Process does something unrelated.
func (u *UnrelatedService) Process() {}
