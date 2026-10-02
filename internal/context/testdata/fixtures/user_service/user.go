package userservice

// User represents a system user.
type User struct {
	ID    string
	Name  string
	Email string
}

// ValidateUser checks required fields.
func ValidateUser(u *User) error {
	if u.Name == "" {
		return errInvalidUser
	}
	return nil
}

var errInvalidUser = errorf("invalid user")

func errorf(msg string) error { return &validationError{msg: msg} }

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
