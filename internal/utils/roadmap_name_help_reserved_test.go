package utils

import (
	"errors"
	"testing"
)

// TestValidateRoadmapName_HelpIsReservedInItsExactSpelling is a regression guard
// for rmp task 476.
//
// A roadmap named `help` cannot be created or removed by any command: the name is
// a positional argument there, where the word is a help token. SPEC/COMMANDS.md
// § Roadmap Name Validation therefore reserves it, in that spelling alone, with
// the line every reserved name is refused with. Any other letter case is refused
// by the character rule, and a name that merely contains the word is valid.
func TestValidateRoadmapName_HelpIsReservedInItsExactSpelling(t *testing.T) {
	err := ValidateRoadmapName("help")
	if !errors.Is(err, ErrRoadmapNameReserved) || !errors.Is(err, ErrValidation) {
		t.Fatalf(`ValidateRoadmapName("help") = %v, want the reserved-name refusal`, err)
	}
	if got, want := err.Error(), `validation error: "help": roadmap name is a reserved system name`; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}

	for _, name := range []string{"Help", "HELP", "hElp"} {
		err := ValidateRoadmapName(name)
		if !errors.Is(err, ErrInvalidRoadmapName) || errors.Is(err, ErrRoadmapNameReserved) {
			t.Errorf("ValidateRoadmapName(%q) = %v, want the character rule's refusal, not the reserved-name one",
				name, err)
		}
	}

	for _, name := range []string{"helpdesk", "help-centre", "customer-help"} {
		if err := ValidateRoadmapName(name); err != nil {
			t.Errorf("ValidateRoadmapName(%q) = %v, want a valid name", name, err)
		}
	}
}
