package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestSortFieldTable_MatchesTheModelCatalogue pins the map the binary actually
// consults for `--sort` to the catalogue every other surface publishes.
//
// `task list --sort` and `backlog list --sort` both test membership in
// validSortFields, an unexported map declared in task_query.go. Three other
// surfaces describe the same set from a different source: the family help, the
// rejection message, and the AI Agent Contract's TaskSort enum — and all three
// of those derive from models.ValidTaskSorts (internal/aihelp/static.go §
// enumValues). Nothing held the two together, so the map could gain or lose a
// field and the contract would keep publishing the old set.
//
// That gap is load-bearing outside this package.
// TestDocumentedFlagValues_ParseAsTheBinaryParsesThem in internal/aihelp judges
// every enum-typed value README.md documents with the check the binary itself
// applies, keyed by enum name. TaskSort is the one enum with no exported parser
// to call, so that gate checks membership in models.ValidTaskSorts instead. This
// test is what makes that substitution sound rather than approximate: with it,
// "models.ValidTaskSorts accepts it" and "the binary accepts it" are the same
// statement.
func TestSortFieldTable_MatchesTheModelCatalogue(t *testing.T) {
	if len(validSortFields) == 0 || len(models.ValidTaskSorts) == 0 {
		t.Fatal("one of the two sort-field tables is empty; a comparison between an empty set and " +
			"anything proves nothing")
	}

	fromMap := make([]string, 0, len(validSortFields))
	for field, ok := range validSortFields {
		if !ok {
			// A key mapped to false is not a member. Reading the map with the
			// one-value form, as both call sites do, would treat it as absent;
			// listing it here as present would make the comparison lie.
			continue
		}
		fromMap = append(fromMap, field)
	}
	sort.Strings(fromMap)

	fromModels := make([]string, 0, len(models.ValidTaskSorts))
	for _, field := range models.ValidTaskSorts {
		fromModels = append(fromModels, string(field))
	}
	sort.Strings(fromModels)

	if strings.Join(fromMap, " ") != strings.Join(fromModels, " ") {
		t.Errorf("the sort fields the binary accepts and the ones every documented surface publishes "+
			"have diverged:\n  validSortFields        : %s\n  models.ValidTaskSorts  : %s\n"+
			"The map in task_query.go is what `task list --sort` and `backlog list --sort` test "+
			"against; models.ValidTaskSorts is what the help text, the rejection message and the AI "+
			"Agent Contract's TaskSort enum are built from. A field in one and not the other is either "+
			"a value the CLI accepts and documents nowhere, or one it publishes and rejects.",
			strings.Join(fromMap, ", "), strings.Join(fromModels, ", "))
	}
}
