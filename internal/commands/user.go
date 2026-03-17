package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/auth"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// HandleUser handles user commands.
func HandleUser(args []string) error {
	if len(args) == 0 {
		printUserHelp()
		return nil
	}

	subcommand := args[0]

	switch subcommand {
	case "create", "register":
		return userCreate(args[1:])
	case "login":
		return userLogin(args[1:])
	case "logout":
		return userLogout(args[1:])
	case "whoami":
		return userWhoami(args[1:])
	default:
		return fmt.Errorf("unknown user subcommand: %s", subcommand)
	}
}

// userCreate creates a new user.
func userCreate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("username and password required")
	}

	username := args[0]
	password := args[1]

	user := &models.User{
		Username: username,
		Password: password,
	}

	if err := user.IsValid(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}

	// Hash password
	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	// In a real implementation, we would store the user in the database
	// For now, just print success
	fmt.Printf("User created: %s\n", username)
	fmt.Printf("Password hash: %s...\n", hashedPassword[:20])

	return nil
}

// userLogin logs in a user.
func userLogin(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("username and password required")
	}

	username := args[0]
	password := args[1]

	// In a real implementation, we would verify against stored hash
	// For demo purposes, accept any password
	_ = password

	// Create session
	session, err := auth.CreateSession(1, username)
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	fmt.Printf("Logged in as: %s\n", username)
	fmt.Printf("Session token: %s\n", session.Token)
	fmt.Printf("Expires at: %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05"))

	return nil
}

// userLogout logs out a user.
func userLogout(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("session token required")
	}

	token := args[0]
	auth.DeleteSession(token)

	fmt.Println("Logged out successfully")
	return nil
}

// userWhoami shows current user info.
func userWhoami(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("session token required")
	}

	token := args[0]
	session, valid := auth.GetSession(token)
	if !valid {
		return fmt.Errorf("not logged in or session expired")
	}

	fmt.Printf("Username: %s\n", session.Username)
	fmt.Printf("User ID: %d\n", session.UserID)
	fmt.Printf("Session created: %s\n", session.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Session expires: %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05"))

	return nil
}

// printUserHelp prints help for user commands.
func printUserHelp() {
	fmt.Println("Usage: rmp user [command] [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  create <username> <password>   Create a new user")
	fmt.Println("  login <username> <password>    Log in")
	fmt.Println("  logout <token>                Log out")
	fmt.Println("  whoami <token>                Show current user")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  rmp user create john mypassword123")
	fmt.Println("  rmp user login john mypassword123")
	fmt.Println("  rmp user whoami <token>")
}
