/* Groadmap task board narrowing: the search term and the three header filters.
 *
 * The board carries every card of the roadmap, so narrowing it is a DOM
 * operation: the script shows and hides cards, recomputes each column's count,
 * toggles the empty states, and keeps the URL in step. No request is made, and
 * nothing is written anywhere (SPEC/WEB.md § Roadmap Tasks Page, Header search
 * control; Header filter controls; Effect on the board).
 *
 * ONE FILTERING MODEL, NOT TWO. The term and the three filters are not separate
 * mechanisms: each is one criterion, a card is shown when it satisfies EVERY
 * active criterion, and that single conjunction is computed in one place
 * (`matches` below) exactly as the server computes it in one place
 * (web.taskView.matches). Narrowing a criterion can only shrink the shown set,
 * and no criterion ever re-admits a card another excluded (SPEC/WEB.md § Roadmap
 * Tasks Page, How the criteria compose).
 *
 * SERVER AND CLIENT MUST AGREE. The same controls produce the same board whether
 * they were set here or carried in the URL of a cold load, and that equivalence
 * is kept by construction rather than by two implementations happening to match:
 *
 *   - a task's own values are transformed by ONE side only. The server folds the
 *     title once into data-search and writes the type, priority and severity once
 *     into data-type, data-priority and data-severity; this script only reads
 *     them, so it can never fold or spell a task's value differently;
 *   - the reference is rebuilt from data-task-id as "#<id>", exactly as the
 *     server builds it;
 *   - a filter value is never parsed here either: the only values these controls
 *     can hold are the options the server emitted from the TaskType enum and from
 *     the threshold range, so the "is this value accepted?" question the server
 *     answers for a URL parameter cannot arise on this side;
 *   - the term is folded with toLowerCase(), NOT toLocaleLowerCase(): the
 *     locale-sensitive variant would make the same term select different tasks
 *     for different viewers, which the matching rule forbids.
 *
 * SECURITY. The term is text the user typed, echoed back into the page. It is
 * written through textContent only — this file contains no innerHTML, no
 * outerHTML, no insertAdjacentHTML and no document.write — so a term carrying
 * markup renders as visible characters and introduces no element, attribute or
 * script (SPEC/WEB.md § Roadmap Tasks Page, Escaping the term).
 */
(function () {
  "use strict";

  var input = document.querySelector('[data-role="task-search"]');
  var board = document.querySelector('[data-role="task-board"]');
  if (!input || !board) {
    return;
  }

  var boardEmpty = document.querySelector('[data-role="task-search-empty"]');
  var boardEmptyTerm = document.querySelector('[data-role="task-search-term"]');
  var boardEmptyTermPhrase = document.querySelector('[data-role="task-search-term-phrase"]');
  var columns = board.querySelectorAll('[data-role="task-board-column"]');

  /* equals is the type filter's comparison: a task matches when its type IS the
   * selected TaskType value. Both sides are the enum's own spelling — the card's
   * from the server, the control's from the option set the server emitted — so
   * the comparison is exact and needs no case folding, exactly as `rmp task list
   * -y` compares. */
  function equals(cardValue, filterValue) {
    return cardValue === filterValue;
  }

  /* atLeast is the priority and severity filters' comparison: a task matches when
   * its value is greater than or equal to the selected threshold, which is the
   * meaning `rmp task list -p` and `--severity` already carry. Both operands are
   * the server's own digit strings. */
  function atLeast(cardValue, filterValue) {
    return Number(cardValue) >= Number(filterValue);
  }

  /* The three filters in ONE table: the URL parameter each travels in, the
   * control that sets it, the card attribute it reads, and how it compares.
   * Nothing else in this file knows how many dimensions there are, so a further
   * one is a row here and no change anywhere else — which is the same property
   * the server's single conjunction has. */
  var filters = [
    {
      param: "type",
      control: document.querySelector('[data-role="task-filter-type"]'),
      attribute: "data-type",
      compare: equals
    },
    {
      param: "priority",
      control: document.querySelector('[data-role="task-filter-priority"]'),
      attribute: "data-priority",
      compare: atLeast
    },
    {
      param: "severity",
      control: document.querySelector('[data-role="task-filter-severity"]'),
      attribute: "data-severity",
      compare: atLeast
    }
  ];

  /* foldTerm normalises what the user typed into the form the matching rule
   * compares with, mirroring the server's foldSearchTerm: surrounding whitespace
   * stripped, folded to lower case, with whitespace inside the term left alone
   * and matched literally. A term that is empty or all whitespace folds to "",
   * which is no term at all and shows every task. */
  function foldTerm(raw) {
    return raw.trim().toLowerCase();
  }

  /* controls reads the four header controls into the one shape the rest of this
   * file works with: the raw term (echoed back as text), the folded term, and one
   * value per filter, "" meaning that dimension carries no filter — the same
   * meaning the parameter's absence has in the URL and the zero value has on the
   * server. */
  function controls() {
    var values = [];
    for (var i = 0; i < filters.length; i++) {
      values.push(filters[i].control ? filters[i].control.value : "");
    }
    return { raw: input.value, term: foldTerm(input.value), filters: values };
  }

  /* active reports whether any control is narrowing the board, which is what
   * separates a board narrowed to nothing — which says so — from a roadmap that
   * holds no task at all, which does not. */
  function active(state) {
    if (state.term !== "") {
      return true;
    }
    for (var i = 0; i < state.filters.length; i++) {
      if (state.filters[i] !== "") {
        return true;
      }
    }
    return false;
  }

  /* matchesTerm applies the search criterion: the term occurs, as a substring, in
   * the task's title or in its "#<id>" reference. The title arrives already
   * folded from the server; the reference is digits and "#", which no case
   * folding changes. No other field is searched. */
  function matchesTerm(card, term) {
    if (term === "") {
      return true;
    }
    var title = card.getAttribute("data-search") || "";
    if (title.indexOf(term) !== -1) {
      return true;
    }
    var reference = "#" + (card.getAttribute("data-task-id") || "");
    return reference.indexOf(term) !== -1;
  }

  /* matches is the board's ONE verdict on one card: shown when it satisfies EVERY
   * active criterion. An inactive filter is skipped rather than compared, which
   * is what makes a board with no active control show every task. */
  function matches(card, state) {
    if (!matchesTerm(card, state.term)) {
      return false;
    }
    for (var i = 0; i < filters.length; i++) {
      var value = state.filters[i];
      if (value === "") {
        continue;
      }
      if (!filters[i].compare(card.getAttribute(filters[i].attribute) || "", value)) {
        return false;
      }
    }
    return true;
  }

  /* apply narrows the board to the current controls and brings everything the
   * board says about itself back into agreement with what it is showing: the
   * cards, the per-column counts, the per-column empty states, and the board's own
   * no-match message. */
  function apply(state) {
    var shown = 0;

    for (var c = 0; c < columns.length; c++) {
      var column = columns[c];
      var cards = column.querySelectorAll(".task-card");
      var visible = 0;

      for (var i = 0; i < cards.length; i++) {
        var show = matches(cards[i], state);
        cards[i].hidden = !show;
        if (show) {
          visible++;
        }
      }

      // The count states what the column is showing, never the unnarrowed total.
      var badge = column.querySelector(".card-title .badge");
      if (badge) {
        badge.textContent = String(visible);
      }
      // A column emptied by the controls reads exactly like a column the roadmap
      // left empty.
      var columnEmpty = column.querySelector('[data-role="task-board-column-empty"]');
      if (columnEmpty) {
        columnEmpty.hidden = visible > 0;
      }
      shown += visible;
    }

    // The board's own message, shown only when a control is in force and nothing
    // matched the conjunction of all of them. ONE message covers the term and the
    // filters together; the phrase naming the term is shown only when there is a
    // term, and the term itself is written as TEXT, never as markup.
    if (boardEmpty) {
      if (boardEmptyTerm) {
        boardEmptyTerm.textContent = state.raw;
      }
      if (boardEmptyTermPhrase) {
        boardEmptyTermPhrase.hidden = state.term === "";
      }
      boardEmpty.hidden = !(active(state) && shown === 0);
    }
  }

  /* syncURL keeps the address bar showing the board on screen, so a narrowed view
   * is shareable and reloadable. The current history entry is REPLACED rather
   * than a new one pushed: one entry per keystroke, or one per dropdown change,
   * would turn the Back button into an undo key for narrowing. A control on its
   * no-filter option — an empty term, a dropdown on "Any ..." — REMOVES its
   * parameter rather than leaving an empty one behind, so clearing every control
   * leaves the bare page URL. */
  function syncURL(state) {
    if (!window.history || !window.history.replaceState) {
      return;
    }
    var url = new URL(window.location.href);
    if (state.term === "") {
      url.searchParams.delete("q");
    } else {
      url.searchParams.set("q", state.raw);
    }
    for (var i = 0; i < filters.length; i++) {
      if (state.filters[i] === "") {
        url.searchParams.delete(filters[i].param);
      } else {
        url.searchParams.set(filters[i].param, state.filters[i]);
      }
    }
    window.history.replaceState(window.history.state, "", url.toString());
  }

  /* narrow is the single entry point every control ends in, so the four controls
   * cannot drift into four behaviours. */
  function narrow() {
    var state = controls();
    apply(state);
    syncURL(state);
  }

  input.addEventListener("input", narrow);

  /* A search input offers a native clear control, which fires "search" rather
   * than "input" in some browsers; both paths end in the same call. */
  input.addEventListener("search", narrow);

  for (var f = 0; f < filters.length; f++) {
    if (filters[f].control) {
      filters[f].control.addEventListener("change", narrow);
    }
  }

  /* The board arrives already narrowed by the server when the URL carried any of
   * the four values, so nothing is applied on load: re-applying would repeat work
   * the server did and would be the post-load narrowing the specification rules
   * out. */
})();
