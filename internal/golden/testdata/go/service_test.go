package app

import "testing"

func TestCreate(t *testing.T) {
	s := &UserService{}
	if err := s.Create("alice"); err != nil {
		t.Fatal(err)
	}
}
