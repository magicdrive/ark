package userservice

// UserStore is the persistence interface for users.
// Implementations may use SQL, NoSQL, or in-memory storage.
type UserStore interface {
	Save(u *User) error
	Find(id string) (*User, error)
	Delete(id string) error
}

// InMemoryStore is a simple in-memory UserStore for testing.
type InMemoryStore struct {
	data map[string]*User
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{data: make(map[string]*User)}
}

// Save stores a user in memory.
func (s *InMemoryStore) Save(u *User) error {
	if u == nil {
		return errorf("nil user")
	}
	s.data[u.ID] = u
	return nil
}

// Find retrieves a user by ID from memory.
func (s *InMemoryStore) Find(id string) (*User, error) {
	if u, ok := s.data[id]; ok {
		return u, nil
	}
	return nil, errorf("not found: " + id)
}

// Delete removes a user from memory.
func (s *InMemoryStore) Delete(id string) error {
	delete(s.data, id)
	return nil
}
