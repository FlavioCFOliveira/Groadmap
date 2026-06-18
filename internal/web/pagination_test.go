package web

import (
	"strconv"
	"strings"
	"testing"
)

// fmtItems renders a pageItem sequence into a compact, comparable string so a
// mismatch shows the whole sequence at a glance. A current page is wrapped in
// brackets ("[N]"), an ellipsis is "...", and a plain link is its number.
func fmtItems(items []pageItem) string {
	parts := make([]string, len(items))
	for i, it := range items {
		switch {
		case it.IsEllipsis:
			parts[i] = "..."
		case it.IsCurrent:
			parts[i] = "[" + strconv.Itoa(it.Number) + "]"
		default:
			parts[i] = strconv.Itoa(it.Number)
		}
	}
	return strings.Join(parts, " ")
}

// TestPaginationItems asserts the exact rendered sequence for representative
// cases of the deterministic sliding-window-with-ellipsis contract
// (SPEC/WEB.md § Roadmap Audit Log Page, sliding window with ellipsis).
func TestPaginationItems(t *testing.T) {
	cases := []struct {
		name       string
		current    int
		totalPages int
		want       string // see fmtItems: "[N]" current, "..." ellipsis, "N" link
	}{
		// Single page: only page 1, current, no ellipsis. Chevron-disabled state
		// is asserted at the handler level; here the sequence is just "[1]".
		{"single page", 1, 1, "[1]"},

		// Small totals fit entirely: every number shown, no ellipsis (rule 5).
		{"three pages, current first", 1, 3, "[1] 2 3"},
		{"three pages, current middle", 2, 3, "1 [2] 3"},
		{"five pages, current middle", 3, 5, "1 2 [3] 4 5"},
		{"five pages, current last", 5, 5, "1 2 3 4 [5]"},

		// Many pages, current at the start: anchor 1 inside the window, single
		// trailing ellipsis before the last anchor (rule 3).
		{"twenty pages, current at start", 1, 20, "[1] 2 3 ... 20"},

		// Many pages, current in the middle: window [8..12], an ellipsis on each
		// side between the anchors and the window.
		{"twenty pages, current in middle", 10, 20, "1 ... 8 9 [10] 11 12 ... 20"},

		// Many pages, current at the end: leading ellipsis, window [18..20]
		// reaching the last anchor.
		{"twenty pages, current at end", 20, 20, "1 ... 18 19 [20]"},

		// Single-page gap renders the number directly, NOT an ellipsis (rule 4).
		// current=4, total=20 -> window [2..6]; the gap between anchor 1 and the
		// window is page 1..2 with nothing hidden on the low side, while the high
		// side gap (window..20) is wide -> ellipsis. Construct a one-page gap on
		// the LOW side instead: current=5 -> window [3..7]; gap between anchor 1
		// and window low (3) is exactly page 2 -> rendered directly.
		{"low-side one-page gap renders number", 5, 20, "1 2 3 4 [5] 6 7 ... 20"},

		// One-page gap on the HIGH side: window high must be exactly TotalPages-2
		// so only TotalPages-1 is hidden. total=20, current=16 -> window [14..18];
		// high gap is page 19 only -> rendered directly, low gap (2..13) wide ->
		// ellipsis.
		{"high-side one-page gap renders number", 16, 20, "1 ... 14 15 [16] 17 18 19 20"},

		// Window touching both anchors with no gap at all on a medium total:
		// total=6, current=3 -> window [1..5]; with anchor 6 contiguous, every
		// number shown, no ellipsis.
		{"window spans whole range no ellipsis", 3, 6, "1 2 [3] 4 5 6"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fmtItems(paginationItems(tc.current, tc.totalPages))
			if got != tc.want {
				t.Errorf("paginationItems(%d, %d) = %q, want %q", tc.current, tc.totalPages, got, tc.want)
			}
		})
	}
}

// TestPaginationItemsAnchorsAlwaysPresent asserts the structural invariants that
// must hold for every input in a representative range: page 1 and TotalPages are
// always present, the current page is marked exactly once, ellipses are never
// links, and no two ellipses are adjacent (each gap collapses to one).
func TestPaginationItemsAnchorsAlwaysPresent(t *testing.T) {
	for total := 1; total <= 40; total++ {
		for current := 1; current <= total; current++ {
			items := paginationItems(current, total)

			var sawFirst, sawLast bool
			currentCount := 0
			prevEllipsis := false
			for _, it := range items {
				if it.IsEllipsis {
					if prevEllipsis {
						t.Fatalf("total=%d current=%d: two adjacent ellipses", total, current)
					}
					prevEllipsis = true
					continue
				}
				prevEllipsis = false
				if it.Number == 1 {
					sawFirst = true
				}
				if it.Number == total {
					sawLast = true
				}
				if it.IsCurrent {
					currentCount++
					if it.Number != current {
						t.Fatalf("total=%d current=%d: active item is page %d", total, current, it.Number)
					}
				}
			}
			if !sawFirst {
				t.Errorf("total=%d current=%d: page 1 anchor missing", total, current)
			}
			if !sawLast {
				t.Errorf("total=%d current=%d: last-page anchor missing", total, current)
			}
			if currentCount != 1 {
				t.Errorf("total=%d current=%d: current marked %d times, want 1", total, current, currentCount)
			}
		}
	}
}
