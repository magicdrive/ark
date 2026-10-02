package userservice

// UserHandler is an HTTP-style request handler backed by UserService.
type UserHandler struct {
	svc *UserService
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(svc *UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// HandleCreate processes a create-user request.
func (h *UserHandler) HandleCreate(u *User) error {
	if u == nil {
		return errorf("nil request")
	}
	return h.svc.Create(u)
}

// HandleFind processes a find-user request.
func (h *UserHandler) HandleFind(id string) (*User, error) {
	if id == "" {
		return nil, errorf("empty id")
	}
	return h.svc.repo.Find(id)
}
