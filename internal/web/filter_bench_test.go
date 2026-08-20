package web

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// These two benchmarks record the measurement behind the clause order of
// taskView.matches (SPEC/WEB.md § Roadmap Tasks Page, How the criteria compose).
//
// The conjunction is over pure predicates, so its evaluation order is free to
// choose and cannot change the verdict. Three of the four clauses are integer or
// string comparisons that allocate nothing; the fourth folds the task's title
// through taskView.SearchText and allocates once per task whenever the title is
// not already lower case. Ordering the cheap clauses first lets a task rejected
// by a threshold skip that fold.
//
// BenchmarkTaskMatches_TermFirst is the order the search shipped with, kept as
// the control: without it the number below would be a figure with nothing to
// compare against, which is not a measurement.
//
// The shape of the corpus matters, so it is stated rather than assumed: 200
// tasks, the roadmap's own order of magnitude, titles in mixed case (so the fold
// really allocates), priorities and severities spread over the whole 0-9 range,
// and types cycled through the enum.

func benchmarkTaskViews(n int) []taskView {
	views := make([]taskView, n)
	for i := range views {
		views[i].ID = i + 1
		views[i].Title = "Cache The Acquirer Settlement Report Number " + string(rune('A'+i%26))
		views[i].Type = models.ValidTaskTypes[i%len(models.ValidTaskTypes)]
		views[i].Priority = i % 10
		views[i].Severity = (i * 3) % 10
	}
	return views
}

// BenchmarkTaskMatches_OrdinalFirst measures the shipped order: the two
// thresholds, then the type, then the term.
func BenchmarkTaskMatches_OrdinalFirst(b *testing.B) {
	views := benchmarkTaskViews(200)
	controls := newBoardControls("cache", models.TypeBug, 7, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range views {
			_ = views[j].matches(controls)
		}
	}
}

// BenchmarkTaskMatches_TermFirst measures the same conjunction with the term
// evaluated first, which is the order the search alone used.
func BenchmarkTaskMatches_TermFirst(b *testing.B) {
	views := benchmarkTaskViews(200)
	controls := newBoardControls("cache", models.TypeBug, 7, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range views {
			view := &views[j]
			_ = view.matchesSearch(controls.folded) &&
				(controls.Type == "" || view.Type == controls.Type) &&
				view.Priority >= controls.Priority &&
				view.Severity >= controls.Severity
		}
	}
}
