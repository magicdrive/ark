package app

// User represents an account.
type User struct {
	ID   int64
	Name string
}

// Save persists the user.
func (u *User) Save() error {
	return nil
}

const DefaultRole = "member"
