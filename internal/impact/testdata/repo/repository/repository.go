package repository

import "fmt"

// Save persists data by id.
func Save(id int) error {
	fmt.Printf("saving %d\n", id)
	return nil
}
