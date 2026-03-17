// Package models defines the data structures for Groadmap entities.
package models

import (
	"fmt"
	"time"
)

// User represents a user in the system.
type User struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"` // Never expose password in JSON
	CreatedAt time.Time `json:"created_at"`
	LastLogin time.Time `json:"last_login,omitempty"`
}

// UserSession represents an active user session.
type UserSession struct {
	Token     string    `json:"token"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsValid validates user data.
func (u *User) IsValid() error {
	if u.Username == "" {
		return fmt.Errorf("username is required")
	}
	if len(u.Username) < 3 {
		return fmt.Errorf("username must be at least 3 characters")
	}
	if len(u.Username) > 50 {
		return fmt.Errorf("username must be at most 50 characters")
	}
	if u.Password == "" {
		return fmt.Errorf("password is required")
	}
	if len(u.Password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

// IsExpired checks if the session has expired.
func (s *UserSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsActive checks if the session is active (not expired).
func (s *UserSession) IsActive() bool {
	return !s.IsExpired()
}
