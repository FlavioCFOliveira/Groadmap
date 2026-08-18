/* Groadmap task board search.
 *
 * The board carries every card of the roadmap, so narrowing it is a DOM
 * operation: the script shows and hides cards, recomputes each column's count,
 * toggles the empty states, and keeps the URL in step. No request is made, and
 * nothing is written anywhere (SPEC/WEB.md § Roadmap Tasks Page, Header search
 * control; Effect on the board).
 *
 * SERVER AND CLIENT MUST AGREE. The same term produces the same board whether it
 * was typed here or carried in the URL of a cold load, and that equivalence is
 * kept by construction rather than by two implementations happening to match:
 *
 *   - the corpus is folded ONCE, by the server, into each card's data-search
 *     attribute, so this script never case-folds task text;
 *   - the reference is rebuilt from data-task-id as "#<id>", exactly as the
 *     server builds it;
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
  var columns = board.querySelectorAll('[data-role="task-board-column"]');

  /* foldTerm normalises what the user typed into the form the matching rule
   * compares with, mirroring the server's foldSearchTerm: surrounding whitespace
   * stripped, folded to lower case, with whitespace inside the term left alone
   * and matched literally. A term that is empty or all whitespace folds to "",
   * which is no term at all and shows every task. */
  function foldTerm(raw) {
    return raw.trim().toLowerCase();
  }

  /* matches applies the one matching rule: the term occurs, as a substring, in
   * the task's title or in its "#<id>" reference. The title arrives already
   * folded from the server; the reference is digits and "#", which no case
   * folding changes. No other field is searched. */
  function matches(card, term) {
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

  /* apply narrows the board to a term and brings everything the board says about
   * itself back into agreement with what it is showing: the cards, the per-column
   * counts, the per-column empty states, and the board's own no-match message. */
  function apply(raw) {
    var term = foldTerm(raw);
    var shown = 0;

    for (var c = 0; c < columns.length; c++) {
      var column = columns[c];
      var cards = column.querySelectorAll(".task-card");
      var visible = 0;

      for (var i = 0; i < cards.length; i++) {
        var show = matches(cards[i], term);
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
      // A column emptied by the search reads exactly like a column the roadmap
      // left empty.
      var columnEmpty = column.querySelector('[data-role="task-board-column-empty"]');
      if (columnEmpty) {
        columnEmpty.hidden = visible > 0;
      }
      shown += visible;
    }

    // The board's own message, shown only when a term is in force and nothing
    // matched it. The term is written as TEXT, never as markup.
    if (boardEmpty) {
      if (boardEmptyTerm) {
        boardEmptyTerm.textContent = raw;
      }
      boardEmpty.hidden = !(term !== "" && shown === 0);
    }
  }

  /* syncURL keeps the address bar showing the board on screen, so a narrowed view
   * is shareable and reloadable. The current history entry is REPLACED rather
   * than a new one pushed: one entry per keystroke would turn the Back button
   * into an undo key for typing. An empty term removes q rather than leaving an
   * empty parameter behind. */
  function syncURL(raw) {
    if (!window.history || !window.history.replaceState) {
      return;
    }
    var url = new URL(window.location.href);
    if (foldTerm(raw) === "") {
      url.searchParams.delete("q");
    } else {
      url.searchParams.set("q", raw);
    }
    window.history.replaceState(window.history.state, "", url.toString());
  }

  input.addEventListener("input", function () {
    apply(input.value);
    syncURL(input.value);
  });

  /* A search input offers a native clear control, which fires "search" rather
   * than "input" in some browsers; both paths end in the same call. */
  input.addEventListener("search", function () {
    apply(input.value);
    syncURL(input.value);
  });

  /* The board arrives already narrowed by the server when the URL carried a term,
   * so nothing is applied on load: re-applying would repeat work the server did
   * and would be the post-load narrowing the specification rules out. */
})();
