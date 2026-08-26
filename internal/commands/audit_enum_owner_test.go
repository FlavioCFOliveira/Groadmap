package commands

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file guards the outcome of rmp task #289: the `audit` command used to
// validate its two enums inline — `models.IsValidAuditOperation` /
// `models.IsValidEntityType` followed by a hand-built message — while
// `models.ParseAuditOperation` and `models.ParseEntityType` existed for exactly
// that purpose and had no caller anywhere in production code.
//
// Two things were wrong with that, and this file pins both:
//
//  1. The enum had two validators, so it had two ways to be wrong. The wording
//     drifted apart unnoticed: `audit` named the rejected value unquoted and
//     called the operation enum "operation", while every other enum refusal
//     quoted the value and named the enum in full. A user met two conventions
//     for one class of mistake depending on the command typed.
//  2. Because the parsers had no caller, a defect inside them was invisible
//     from the CLI. The doubled-sentinel bug rmp task #192 fixed had been
//     sitting in both of them, and no command surface could have revealed it.
//
// TestEnumRefusalsShareOneShape below asserts the CONVENTION rather than a
// single literal, so it keeps its meaning if the wording is deliberately
// revised later: what must hold is that `audit`'s enums read exactly like every
// other enum's. The exact per-command literals are pinned separately, in
// internal/commands/enum_message_dedup_test.go.

// enumRefusalShape is the one rendering every enum refusal in the CLI follows:
//
//	validation error: invalid <enum name>: "<the rejected value>"
//
// The quotes are load-bearing. The hand-built `audit` messages this task
// removed rendered the value bare, so a relapse fails this pattern outright
// rather than needing a literal to be updated in step with it.
var enumRefusalShape = regexp.MustCompile(`^validation error: invalid ([a-z][a-z ]*): (".*")$`)

// enumRefusal is one command invocation refused for an enum value, together
// with the enum the message must name and the value it must quote back.
type enumRefusal struct {
	name string
	// run performs the invocation against the roadmap named by the argument.
	run func(roadmap string) error
	// wantEnum is the enum name the message must use, in full.
	wantEnum string
	// wantValue is the rejected value, which must be quoted back verbatim.
	wantValue string
}

// TestEnumRefusalsShareOneShape drives both `audit` surfaces — the `-o` and
// `-e` flags of `audit list` and the leading positional of `audit history` —
// next to controls drawn from three other enums, and asserts every one of them
// renders in the same shape. The controls are what make this a test of the
// shared convention: if a future change moved `audit` off it, the audit rows
// would fail while the controls passed, naming the divergence precisely.
func TestEnumRefusalsShareOneShape(t *testing.T) {
	roadmap := "testauditenumowner"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	cases := []enumRefusal{
		// The two surfaces this task moved onto the shared owner.
		{
			name:      "audit list --operation",
			run:       func(r string) error { return HandleAudit([]string{"list", "-r", r, "-o", "BOGUS"}) },
			wantEnum:  "audit operation",
			wantValue: "BOGUS",
		},
		{
			name:      "audit list --entity-type",
			run:       func(r string) error { return HandleAudit([]string{"list", "-r", r, "-e", "BOGUS"}) },
			wantEnum:  "entity type",
			wantValue: "BOGUS",
		},
		{
			name:      "audit history <entity-type>",
			run:       func(r string) error { return HandleAudit([]string{"history", "-r", r, "BOGUS", "1"}) },
			wantEnum:  "entity type",
			wantValue: "BOGUS",
		},
		{
			// SPEC/COMMANDS.md § Entity History states this exact case: there
			// is no `-e` flag form here, so a leading `-e` is consumed as the
			// entity-type value and refused as one.
			name:      "audit history leading -e",
			run:       func(r string) error { return HandleAudit([]string{"history", "-r", r, "-e", "1"}) },
			wantEnum:  "entity type",
			wantValue: "-e",
		},
		// Controls: enums that already owned their message before this task.
		{
			name:      "control task list --type",
			run:       func(r string) error { return HandleTask([]string{"list", "-r", r, "-y", "BOGUS"}) },
			wantEnum:  "task type",
			wantValue: "BOGUS",
		},
		{
			name:      "control task list --status",
			run:       func(r string) error { return HandleTask([]string{"list", "-r", r, "--status", "BOGUS"}) },
			wantEnum:  "task status",
			wantValue: "BOGUS",
		},
		{
			name:      "control sprint list --status",
			run:       func(r string) error { return HandleSprint([]string{"list", "-r", r, "--status", "BOGUS"}) },
			wantEnum:  "sprint status",
			wantValue: "BOGUS",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(roadmap)
			if err == nil {
				t.Fatalf("want a rejection of %q, got nil", tc.wantValue)
			}
			line := err.Error()

			m := enumRefusalShape.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("refusal does not follow the shared enum convention\n"+
					"  line:  %q\n  want:  validation error: invalid <enum>: %q\n"+
					"the value must be quoted and the enum named in full, as every other enum refusal does",
					line, tc.wantValue)
			}
			if m[1] != tc.wantEnum {
				t.Errorf("enum named %q, want %q\n  line: %q", m[1], tc.wantEnum, line)
			}
			if want := strconv.Quote(tc.wantValue); m[2] != want {
				t.Errorf("quoted value = %s, want %s\n  line: %q", m[2], want, line)
			}
			// The message changed in this task; the exit code must not have.
			// utils.ErrValidation is what cmd/rmp/main.go maps to exit 6.
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation so handleError returns exit 6; got %v", err)
			}
		})
	}
}

// TestCommandsDelegateEnumValidationToModels is the structural half of the
// guard. TestEnumRefusalsShareOneShape would still pass if someone re-created a
// hand-built message that happened to be spelled correctly today; this one
// fails the moment the package validates an enum itself again, which is the
// condition that let the two wordings drift apart in the first place.
//
// The rule is scoped to internal/commands: a command MUST reach an enum through
// its models.Parse* owner, so the refusal has exactly one author. Other
// packages may legitimately call the IsValid* predicates for non-message
// purposes (internal/aihelp does, to filter a documented value set).
func TestCommandsDelegateEnumValidationToModels(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the commands package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package parsed; this guard would pass vacuously")
	}

	var offenders []string
	parsersReached := map[string]bool{}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "models" {
					return true
				}
				switch {
				case strings.HasPrefix(sel.Sel.Name, "IsValid"):
					offenders = append(offenders,
						path+": models."+sel.Sel.Name+" at "+fset.Position(sel.Pos()).String())
				case sel.Sel.Name == "ParseAuditOperation" || sel.Sel.Name == "ParseEntityType":
					parsersReached[sel.Sel.Name] = true
				}
				return true
			})
		}
	}

	if len(offenders) > 0 {
		t.Errorf("internal/commands must not validate an enum itself; every enum reaches its\n"+
			"models.Parse* owner so the refusal has one author (rmp task #289). Found %d call(s):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}

	// The positive half: prove the audit parsers really are the ones reached,
	// so this guard cannot be satisfied by deleting the validation altogether.
	for _, want := range []string{"ParseAuditOperation", "ParseEntityType"} {
		if !parsersReached[want] {
			t.Errorf("no call to models.%s in internal/commands: the audit enums must be "+
				"validated through their model owner, not dropped", want)
		}
	}
}
