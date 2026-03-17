package auth

import (
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	password := "testpassword123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == "" {
		t.Error("Hash should not be empty")
	}

	if hash == password {
		t.Error("Hash should be different from password")
	}

	// Verify we can check the password
	if !CheckPassword(password, hash) {
		t.Error("CheckPassword should return true for correct password")
	}

	// Verify wrong password fails
	if CheckPassword("wrongpassword", hash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestGenerateToken(t *testing.T) {
	token1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token1 == "" {
		t.Error("Token should not be empty")
	}

	// Tokens should be unique
	token2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token1 == token2 {
		t.Error("Tokens should be unique")
	}
}

func TestCreateSession(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	session, err := CreateSession(1, "testuser")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.Token == "" {
		t.Error("Session token should not be empty")
	}

	if session.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", session.UserID)
	}

	if session.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", session.Username)
	}

	if session.IsExpired() {
		t.Error("New session should not be expired")
	}

	if !session.IsActive() {
		t.Error("New session should be active")
	}
}

func TestGetSession(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	// Create a session
	session, err := CreateSession(1, "testuser")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Retrieve the session
	retrieved, exists := GetSession(session.Token)
	if !exists {
		t.Error("Session should exist")
	}

	if retrieved.UserID != session.UserID {
		t.Error("Retrieved session should match original")
	}

	// Non-existent session
	_, exists = GetSession("nonexistenttoken")
	if exists {
		t.Error("Non-existent session should not exist")
	}
}

func TestDeleteSession(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	// Create a session
	session, err := CreateSession(1, "testuser")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Delete the session
	DeleteSession(session.Token)

	// Verify it's gone
	_, exists := GetSession(session.Token)
	if exists {
		t.Error("Deleted session should not exist")
	}
}

func TestSessionExpiration(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	// Create a session
	session, err := CreateSession(1, "testuser")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Manually expire the session
	session.ExpiresAt = time.Now().Add(-time.Hour)
	globalSessions.mu.Lock()
	globalSessions.sessions[session.Token] = session
	globalSessions.mu.Unlock()

	// Should be expired
	if !session.IsExpired() {
		t.Error("Session should be expired")
	}

	if session.IsActive() {
		t.Error("Expired session should not be active")
	}

	// GetSession should return false for expired session
	_, exists := GetSession(session.Token)
	if exists {
		t.Error("GetSession should return false for expired session")
	}
}

func TestCleanupSessions(t *testing.T) {
	// Clean up at end
	defer CleanupSessions()

	// Create sessions
	session1, _ := CreateSession(1, "user1")
	session2, _ := CreateSession(2, "user2")

	// Expire one session
	session1.ExpiresAt = time.Now().Add(-time.Hour)
	globalSessions.mu.Lock()
	globalSessions.sessions[session1.Token] = session1
	globalSessions.mu.Unlock()

	// Cleanup
	CleanupSessions()

	// Expired session should be removed
	_, exists := GetSession(session1.Token)
	if exists {
		t.Error("Expired session should be removed after cleanup")
	}

	// Active session should remain
	_, exists = GetSession(session2.Token)
	if !exists {
		t.Error("Active session should remain after cleanup")
	}
}

func TestGetActiveSessionsCount(t *testing.T) {
	// Clean up before and after
	ResetSessions()
	defer ResetSessions()

	// Initially should be 0
	if count := GetActiveSessionsCount(); count != 0 {
		t.Errorf("Expected 0 active sessions, got %d", count)
	}

	// Create sessions
	CreateSession(1, "user1")
	CreateSession(2, "user2")

	// Should be 2
	if count := GetActiveSessionsCount(); count != 2 {
		t.Errorf("Expected 2 active sessions, got %d", count)
	}
}

func TestIsAuthenticated(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	// Create a session
	session, _ := CreateSession(1, "testuser")

	// Valid token
	retrieved, valid := IsAuthenticated(session.Token)
	if !valid {
		t.Error("Valid token should be authenticated")
	}
	if retrieved.UserID != 1 {
		t.Error("Retrieved session should have correct UserID")
	}

	// Invalid token
	_, valid = IsAuthenticated("invalidtoken")
	if valid {
		t.Error("Invalid token should not be authenticated")
	}
}

func TestRequireAuth(t *testing.T) {
	// Clean up
	defer CleanupSessions()

	// Create a session
	session, _ := CreateSession(1, "testuser")

	// Valid token
	retrieved, err := RequireAuth(session.Token)
	if err != nil {
		t.Errorf("RequireAuth should not error for valid token: %v", err)
	}
	if retrieved.UserID != 1 {
		t.Error("Retrieved session should have correct UserID")
	}

	// Invalid token
	_, err = RequireAuth("invalidtoken")
	if err == nil {
		t.Error("RequireAuth should error for invalid token")
	}
}
