package service

// AdminService handles admin operations.
type AdminService struct{}

// Update updates a record.
func (a *AdminService) Update(id int) error {
	svc := &UserService{}
	return svc.Save(id)
}

// Delete removes a record.
func (a *AdminService) Delete(id int) error {
	return nil
}
