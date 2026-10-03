package app

// Draft is an unsaved user. It intentionally also declares a Save method so
// that calls to Save() have more than one candidate (ambiguity test).
type Draft struct{}

// Save discards the draft.
func (d *Draft) Save() error {
	return nil
}
