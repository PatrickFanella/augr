package domain

import "fmt"

// requireNonEmpty returns an error if value is empty.
func requireNonEmpty(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

// requirePositive returns an error if value is not positive.
func requirePositive(field string, value float64) error {
	if value <= 0 {
		return fmt.Errorf("%s must be positive, got %v", field, value)
	}
	return nil
}
