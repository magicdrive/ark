package userservice

import "testing"

func TestValidateUser_Valid(t *testing.T) {
	u := &User{ID: "1", Name: "Alice", Email: "alice@example.com"}
	if err := ValidateUser(u); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateUser_EmptyName(t *testing.T) {
	u := &User{ID: "2", Name: "", Email: "bob@example.com"}
	if err := ValidateUser(u); err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestUserService_Create(t *testing.T) {
	store := NewInMemoryStore()
	repo := &UserRepository{}
	_ = repo
	svc := &UserService{repo: &UserRepository{}}
	_ = svc
	_ = store
}
