package commands

import (
	"strings"
	"testing"
)

func TestFlagParser_Parse(t *testing.T) {
	defs := []FlagDef{
		{Name: "--description", Short: "-d", Field: "Description", Type: "string", Required: true},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int", Default: "0"},
		{Name: "--verbose", Short: "-v", Field: "Verbose", Type: "bool"},
	}

	parser := NewFlagParser(defs)

	tests := []struct {
		name      string
		args      []string
		wantErr   bool
		errMsg    string
		checkFunc func(t *testing.T, result *ParseResult)
	}{
		{
			name:    "valid flags",
			args:    []string{"-d", "test description", "-p", "5"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Flags["Description"] != "test description" {
					t.Errorf("expected Description='test description', got %v", result.Flags["Description"])
				}
				if result.Flags["Priority"] != 5 {
					t.Errorf("expected Priority=5, got %v", result.Flags["Priority"])
				}
			},
		},
		{
			name:    "long form flags",
			args:    []string{"--description", "test", "--priority", "3"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Flags["Description"] != "test" {
					t.Errorf("expected Description='test', got %v", result.Flags["Description"])
				}
			},
		},
		{
			name:    "missing required flag",
			args:    []string{"-p", "5"},
			wantErr: true,
			errMsg:  "missing required flag",
		},
		{
			name:    "boolean flag",
			args:    []string{"-d", "test", "-v"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Flags["Verbose"] != true {
					t.Errorf("expected Verbose=true, got %v", result.Flags["Verbose"])
				}
			},
		},
		{
			name:    "default value",
			args:    []string{"-d", "test"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Flags["Priority"] != 0 {
					t.Errorf("expected Priority=0 (default), got %v", result.Flags["Priority"])
				}
			},
		},
		{
			name:    "roadmap flag",
			args:    []string{"-r", "myroadmap", "-d", "test"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Roadmap != "myroadmap" {
					t.Errorf("expected Roadmap='myroadmap', got %v", result.Roadmap)
				}
			},
		},
		{
			name:    "long roadmap flag",
			args:    []string{"--roadmap", "myroadmap", "-d", "test"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if result.Roadmap != "myroadmap" {
					t.Errorf("expected Roadmap='myroadmap', got %v", result.Roadmap)
				}
			},
		},
		{
			name:    "positional arguments",
			args:    []string{"-d", "test", "arg1", "arg2"},
			wantErr: false,
			checkFunc: func(t *testing.T, result *ParseResult) {
				if len(result.Args) != 2 || result.Args[0] != "arg1" || result.Args[1] != "arg2" {
					t.Errorf("expected Args=['arg1', 'arg2'], got %v", result.Args)
				}
			},
		},
		{
			name:    "unknown flag",
			args:    []string{"-d", "test", "--unknown"},
			wantErr: true,
			errMsg:  "unknown flag",
		},
		{
			name:    "flag without value",
			args:    []string{"-d"},
			wantErr: true,
			errMsg:  "requires a value",
		},
		{
			name:    "invalid int value",
			args:    []string{"-d", "test", "-p", "notanumber"},
			wantErr: true,
			errMsg:  "invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, result)
			}
		})
	}
}

func TestFlagParser_Bind(t *testing.T) {
	type TestStruct struct {
		Description string
		Priority    int
		Verbose     bool
	}

	defs := []FlagDef{
		{Name: "--description", Short: "-d", Field: "Description", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int"},
		{Name: "--verbose", Short: "-v", Field: "Verbose", Type: "bool"},
	}

	parser := NewFlagParser(defs)

	result := &ParseResult{
		Flags: map[string]interface{}{
			"Description": "test",
			"Priority":    5,
			"Verbose":     true,
		},
		Args: []string{},
	}

	var target TestStruct
	err := parser.Bind(result, &target)
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if target.Description != "test" {
		t.Errorf("Description = %q, want %q", target.Description, "test")
	}
	if target.Priority != 5 {
		t.Errorf("Priority = %d, want %d", target.Priority, 5)
	}
	if target.Verbose != true {
		t.Errorf("Verbose = %v, want %v", target.Verbose, true)
	}
}

func TestFlagParser_BindInvalidTarget(t *testing.T) {
	defs := []FlagDef{
		{Name: "--description", Short: "-d", Field: "Description", Type: "string"},
	}

	parser := NewFlagParser(defs)

	result := &ParseResult{
		Flags: map[string]interface{}{"Description": "test"},
		Args:  []string{},
	}

	// Test with non-pointer
	err := parser.Bind(result, "not a struct")
	if err == nil {
		t.Error("Bind() should fail with non-pointer target")
	}

	// Test with non-struct pointer
	var i int
	err = parser.Bind(result, &i)
	if err == nil {
		t.Error("Bind() should fail with non-struct pointer target")
	}
}

func TestCommonFlagDefs(t *testing.T) {
	// Verify that common flag definitions are valid
	tests := []struct {
		name string
		defs []FlagDef
	}{
		{"TaskCreateFlags", TaskCreateFlags},
		{"TaskEditFlags", TaskEditFlags},
		{"SprintCreateFlags", SprintCreateFlags},
		{"AuditListFlags", AuditListFlags},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewFlagParser(tt.defs)
			if parser == nil {
				t.Error("NewFlagParser() returned nil")
			}
		})
	}
}
