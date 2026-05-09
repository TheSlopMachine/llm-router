package util

import "time"

// Now returns the current time in UTC.
// Centralizes timestamp generation for consistency across the application.
func Now() time.Time {
	return time.Now().UTC()
}

// NowPtr returns a pointer to the current UTC time.
// Useful for optional timestamp fields.
func NowPtr() *time.Time {
	t := Now()
	return &t
}

