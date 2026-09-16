package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the structural regression gate for rmp task #386, and for the
// second shape of the same defect that rmp task #403 closes.
//
// The defect: `rmp graph serve` wrote its stderr diagnostics with timestamps in
// the machine's LOCAL zone — `time=2026-09-03T11:51:05.221+01:00` — where
// SPEC/DATA_FORMATS.md § Dates - ISO 8601 with UTC requires
// `YYYY-MM-DDTHH:mm:ss.sssZ` of every Groadmap timestamp. slog.TextHandler
// produces the local form BY DEFAULT, so the defect was an omission rather than
// a decision: `rmp web` had installed a ReplaceAttr hook and the graph server had
// not, and nothing in the module noticed that two surfaces answered one question
// differently.
//
// A measured test beside each logger proves that logger stamps correctly today
// (internal/web's TestLogTimestampIsCanonicalUTC, internal/graphserve's
// TestLogger_TimestampIsCanonicalUTC). This gate proves what they cannot,
// because a test can only check the loggers it knows about: that a THIRD handler
// cannot appear anywhere in the module already carrying the defect. That is
// exactly how #386 arrived — as the second one.
//
// Why here. The sweep spans every package, so no audited package is its home,
// and internal/testenv already hosts the module-wide AST gates whose repository
// walk, skip list and helpers this file reuses rather than duplicating.
//
// Known limits, stated rather than papered over:
//
//   - The sweep is syntactic. It reads the handler options where they are
//     written as a literal at the construction site. Options built elsewhere and
//     passed in by name are reported as unreadable rather than assumed correct,
//     which is the review this gate exists to force.
//   - It sees production files only. A test may construct any handler it likes;
//     several do, to assert what an unconfigured one would have produced.
//   - It proves the hook is INSTALLED, not that the hook is right. What the hook
//     does is internal/utils' own TestSlogTimestampUTC and the two measured
//     tests beside the loggers, which check the instant as well as the shape.
//   - It closes the second way a record escapes the hook (rmp task #403): a
//     production call to one of log/slog's package-level output functions —
//     Debug, Info, Warn and Error, their Context variants, Log and LogAttrs —
//     written through whatever name the file imports log/slog under. Each writes
//     through the process's DEFAULT logger, whose handler, unless
//     slog.SetDefault has replaced it, hands the record to the standard log
//     package; the record then reaches stderr under log's local-time prefix and
//     never passes through a hook any handler of this module installs.
//     TestNoProductionCodeLogsThroughTheDefaultLogger fails on every such call.
//     It does not see the default logger reached any other way: a method called
//     on slog.Default(), a function value such as `f := slog.Error`, a dot import
//     of log/slog, or the standard log package itself.

// slogHandlerConstructors are the standard-library handler constructors that
// stamp a record's time. Both are listed so that rewriting a TextHandler as a
// JSONHandler does not walk an unstamped logger past this gate.
var slogHandlerConstructors = map[string]bool{
	"NewTextHandler": true,
	"NewJSONHandler": true,
}

// slogDefaultLoggerFuncs are the log/slog package-level functions that write a
// record through the process's default logger rather than through a logger this
// module built (rmp task #403).
var slogDefaultLoggerFuncs = map[string]bool{
	"Debug":        true,
	"DebugContext": true,
	"Info":         true,
	"InfoContext":  true,
	"Warn":         true,
	"WarnContext":  true,
	"Error":        true,
	"ErrorContext": true,
	"Log":          true,
	"LogAttrs":     true,
}

// canonicalTimestampHook is the one hook every handler must install, written as
// it appears at a call site: the selector on the local name of the internal/utils
// import.
const canonicalTimestampHook = "SlogTimestampUTC"

// utilsImportPath is the package that owns the timestamp format and the hook.
const utilsImportPath = `"github.com/FlavioCFOliveira/Groadmap/internal/utils"`

// slogImportPath is the import path of log/slog, written as an import spec
// carries it.
const slogImportPath = `"log/slog"`

// TestEveryProductionLoggerStampsInCanonicalUTC asserts that every log/slog
// handler built in production code installs the shared UTC timestamp hook.
//
// A failure here is not necessarily a bug — a handler with no timestamp at all
// would be one legitimate reason — but it IS a decision that has to be made
// deliberately rather than by writing `&slog.HandlerOptions{Level: ...}` and
// accepting what slog does by default, which is what produced #386.
func TestEveryProductionLoggerStampsInCanonicalUTC(t *testing.T) {
	root := repoRoot(t)

	var offenders []string
	handlers := 0

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
			return nil
		}

		slogName, imported := importedAs(file, slogImportPath)
		if !imported {
			return nil
		}
		utilsName, utilsImported := importedAs(file, utilsImportPath)

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slogHandlerConstructors[selector.Sel.Name] {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Name != slogName {
				return true
			}

			handlers++
			site := rel + ":" + itoa(fset.Position(call.Pos()).Line)

			if !utilsImported {
				offenders = append(offenders, site+": the file does not import internal/utils, so it "+
					"cannot be installing the shared hook")
				return true
			}
			if !installsHook(call, utilsName) {
				offenders = append(offenders, site+": no `ReplaceAttr: "+utilsName+"."+
					canonicalTimestampHook+"` in the handler options")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	// The gate must still have something to guard. Without this half it passes
	// vacuously the day the loggers are deleted or built some other way, which
	// is the drift that matters most.
	if handlers == 0 {
		t.Errorf("no production file constructs a log/slog handler. If logging moved, move this " +
			"gate with it; a gate with nothing to check is a gate that has stopped working")
	}

	sort.Strings(offenders)
	for _, offender := range offenders {
		t.Errorf("%s\nslog stamps records in the machine's LOCAL zone by default, which is neither "+
			"UTC nor Z-suffixed and is therefore not the format SPEC/DATA_FORMATS.md § Dates - "+
			"ISO 8601 with UTC requires of every Groadmap timestamp. Install "+
			"`ReplaceAttr: utils.%s` — the one hook internal/web and internal/graphserve share — "+
			"rather than writing a second expression of the same format (rmp task #386)",
			offender, canonicalTimestampHook)
	}
}

// TestNoProductionCodeLogsThroughTheDefaultLogger asserts that no production file
// calls a log/slog package-level output function (rmp task #403).
//
// Such a call compiles, runs and writes a record, which is exactly why it needs a
// gate rather than a review: the record goes through neither logger this module
// builds, so neither measured timestamp test can see it, and the handler check
// above cannot either, because no handler is constructed at the call site. The
// shape it reintroduces is a diagnostic such as
// `slog.Error("graph checkpoint failed", ...)` written in the machine's local
// zone beside records that are stamped in canonical UTC.
func TestNoProductionCodeLogsThroughTheDefaultLogger(t *testing.T) {
	root := repoRoot(t)

	var offenders []string
	importers := 0

	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(rel, _ string, file *ast.File) {
		slogName, imported := importedAs(file, slogImportPath)
		if !imported {
			return
		}
		importers++

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !slogDefaultLoggerFuncs[selector.Sel.Name] {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Name != slogName {
				return true
			}
			offenders = append(offenders, rel+":"+itoa(fset.Position(call.Pos()).Line)+": "+
				slogName+"."+selector.Sel.Name)
			return true
		})
	}, fset)

	// The gate must still have something to guard, for the reason the handler
	// gate above gives: with no file importing log/slog it would pass vacuously.
	if importers == 0 {
		t.Errorf("no production file imports log/slog. If logging moved, move this gate with it; " +
			"a gate with nothing to check is a gate that has stopped working")
	}

	sort.Strings(offenders)
	for _, offender := range offenders {
		t.Errorf("%s\nthis call writes through log/slog's DEFAULT logger, which hands the record to the "+
			"standard log package: it is stamped in the machine's LOCAL zone and never passes through "+
			"`ReplaceAttr: utils.%s`, so it is not in the format SPEC/DATA_FORMATS.md § Dates - ISO 8601 "+
			"with UTC requires. Log through the logger of the surface that emits the record — the one "+
			"internal/web or internal/graphserve builds with the shared hook (rmp tasks #386 and #403)",
			offender, canonicalTimestampHook)
	}
}

// installsHook reports whether the handler-options literal at this construction
// site sets ReplaceAttr to the shared hook.
//
// Options that are not written as a literal here are reported as NOT installing
// it. That is deliberate and is the conservative direction: this gate cannot
// follow a value built elsewhere, and treating what it cannot read as compliant
// would make it agree with anything.
func installsHook(call *ast.CallExpr, utilsName string) bool {
	for _, arg := range call.Args {
		unary, ok := arg.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			continue
		}
		literal, ok := unary.X.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || key.Name != "ReplaceAttr" {
				continue
			}
			value, ok := pair.Value.(*ast.SelectorExpr)
			if !ok || value.Sel.Name != canonicalTimestampHook {
				return false
			}
			pkg, ok := value.X.(*ast.Ident)
			return ok && pkg.Name == utilsName
		}
	}
	return false
}

// importedAs returns the local name of an import, and whether the file imports
// it at all. It mirrors timeImportName, generalised to a path, and refuses the
// same two forms for the same reasons: a blank import cannot be called through,
// and a dot import would put the identifier in scope unqualified.
func importedAs(file *ast.File, path string) (string, bool) {
	for _, spec := range file.Imports {
		if spec.Path == nil || spec.Path.Value != path {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				return "", false
			}
			return spec.Name.Name, true
		}
		trimmed := strings.Trim(path, `"`)
		if slash := strings.LastIndex(trimmed, "/"); slash >= 0 {
			trimmed = trimmed[slash+1:]
		}
		return trimmed, true
	}
	return "", false
}
