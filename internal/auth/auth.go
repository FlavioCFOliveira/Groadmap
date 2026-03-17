// Package auth provides authentication and authorization for Groadmap.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// Session duration constants
const (
	SessionDuration = 24 * time.Hour // Sessions expire after 24 hours
)

// SessionManager manages user sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*models.UserSession
}

// Global session manager
var globalSessions = &SessionManager{
	sessions: make(map[string]*models.UserSession),
}

// HashPassword hashes a password using bcrypt.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword verifies a password against a hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateToken generates a secure random token.
func GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// CreateSession creates a new session for a user.
func CreateSession(userID int, username string) (*models.UserSession, error) {
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &models.UserSession{
		Token:     token,
		UserID:    userID,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionDuration),
	}

	globalSessions.mu.Lock()
	globalSessions.sessions[token] = session
	globalSessions.mu.Unlock()

	return session, nil
}

// GetSession retrieves a session by token.
func GetSession(token string) (*models.UserSession, bool) {
	globalSessions.mu.RLock()
	defer globalSessions.mu.RUnlock()

	session, exists := globalSessions.sessions[token]
	if !exists {
		return nil, false
	}

	// Check if session is expired
	if session.IsExpired() {
		return nil, false
	}

	return session, true
}

// DeleteSession removes a session.
func DeleteSession(token string) {
	globalSessions.mu.Lock()
	defer globalSessions.mu.Unlock()
	delete(globalSessions.sessions, token)
}

// CleanupSessions removes expired sessions.
func CleanupSessions() {
	globalSessions.mu.Lock()
	defer globalSessions.mu.Unlock()

	for token, session := range globalSessions.sessions {
		if session.IsExpired() {
			delete(globalSessions.sessions, token)
		}
	}
}

// ResetSessions clears all sessions (for testing).
func ResetSessions() {
	globalSessions.mu.Lock()
	defer globalSessions.mu.Unlock()
	globalSessions.sessions = make(map[string]*models.UserSession)
}

// GetActiveSessionsCount returns the number of active sessions.
func GetActiveSessionsCount() int {
	globalSessions.mu.RLock()
	defer globalSessions.mu.RUnlock()

	count := 0
	for _, session := range globalSessions.sessions {
		if !session.IsExpired() {
			count++
		}
	}
	return count
}

// IsAuthenticated checks if a token is valid and returns the session.
func IsAuthenticated(token string) (*models.UserSession, bool) {
	return GetSession(token)
}

// RequireAuth returns an error if the token is not valid.
func RequireAuth(token string) (*models.UserSession, error) {
	session, valid := GetSession(token)
	if !valid {
		return nil, fmt.Errorf("authentication required: invalid or expired session")
	}
	return session, nil
}
