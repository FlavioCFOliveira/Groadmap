package web

// pageItem is one rendered slot in the numbered pagination bar. The template
// stays declarative by reading these flags instead of computing them: a slot is
// either the current page (IsCurrent), a non-interactive ellipsis (IsEllipsis),
// or a plain page-number link to ?page=<Number>. Number is meaningful only when
// IsEllipsis is false (SPEC/WEB.md § Roadmap Audit Log Page, sliding window with
// ellipsis).
type pageItem struct {
	Number     int
	IsCurrent  bool
	IsEllipsis bool
}

// paginationItems returns the ordered sequence of numbered-bar slots for the
// given 1-based current page and total page count, encoding the deterministic
// sliding-window-with-ellipsis rules of SPEC/WEB.md § Roadmap Audit Log Page:
//
//  1. The bar always shows page 1 and page totalPages.
//  2. It always shows the window [current-2, current+2] clamped to
//     [1, totalPages].
//  3. Each gap between an anchor and the window collapses to a single ellipsis.
//  4. A gap exactly one page wide renders that page number directly, never an
//     ellipsis (an ellipsis never stands in for a single hidden page).
//  5. When the anchors and window already cover every page, every page number
//     is shown and no ellipsis appears.
//
// current is assumed already clamped into [1, totalPages] by the caller
// (loadAudit clamps it); totalPages is assumed at least 1. The function is pure
// and allocation-bounded by totalPages, so it is cheap for the fixed page sizes
// this page produces.
func paginationItems(current, totalPages int) []pageItem {
	if totalPages < 1 {
		totalPages = 1
	}
	if current < 1 {
		current = 1
	}
	if current > totalPages {
		current = totalPages
	}

	// Window bounds: [current-2, current+2] clamped to [1, totalPages]. Page 1
	// and totalPages are anchors, so the rendered span is [1, totalPages] with
	// only the interior gaps (between anchor and window) possibly collapsed.
	windowLow := current - 2
	if windowLow < 1 {
		windowLow = 1
	}
	windowHigh := current + 2
	if windowHigh > totalPages {
		windowHigh = totalPages
	}

	items := make([]pageItem, 0, totalPages)
	for page := 1; page <= totalPages; {
		inSpan := page == 1 || page == totalPages || (page >= windowLow && page <= windowHigh)
		if inSpan {
			items = append(items, pageItem{Number: page, IsCurrent: page == current})
			page++
			continue
		}

		// page is the first hidden page of a gap. Find the next visible page so
		// we know the gap width: a gap of exactly one page is rendered directly
		// (rule 4); a wider gap collapses to a single ellipsis (rule 3).
		next := page + 1
		for next < totalPages && !(next >= windowLow && next <= windowHigh) {
			next++
		}
		if next-page == 1 {
			// Exactly one hidden page: render it directly, no ellipsis.
			items = append(items, pageItem{Number: page, IsCurrent: page == current})
			page++
			continue
		}
		items = append(items, pageItem{IsEllipsis: true})
		page = next
	}

	return items
}
