/* Groadmap sprint member-tasks board: the column collapse toggles.
 *
 * Each of the board's three column headers carries one toggle that collapses its
 * column to a narrow strip and expands it again. The toggle is PRESENTATION
 * ONLY: it changes how the board is shown and nothing the board shows. No
 * request is made, nothing is read or written anywhere — no URL parameter, no
 * cookie, no localStorage, sessionStorage, or IndexedDB — so every page load
 * presents all three columns expanded, whatever state the reader left them in
 * (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Column collapse).
 *
 * THE STATE LIVES IN THE MARKUP, AND IN FOUR PLACES ONLY. A column's state is
 * changed by setting or removing exactly these, and by nothing else:
 *
 *   - the `hidden` attribute of the column body the toggle names in
 *     aria-controls, which takes the cards, or the empty state, out of both the
 *     display and the accessibility tree;
 *   - the modifier class `task-board__column--collapsed` on the column element,
 *     which static/style.css turns into a 3rem strip with the heading and its
 *     count badge set in the vertical writing mode;
 *   - the toggle's `aria-expanded` and `aria-label`, which state the new state
 *     and name the action the toggle will perform next;
 *   - the toggle's chevron icon class, which points a different way in each
 *     state.
 *
 * This file writes no `style` attribute and no inline style property: the
 * strip's width, its layout, and the rotated heading are declared in the project
 * stylesheet under the modifier class, which is where the Content-Security-Policy
 * and the no-inline-style rule require them to be.
 *
 * WITHOUT JAVASCRIPT. The server renders every toggle in its expanded state and
 * with the `hidden` attribute, so a browser that runs no script shows every
 * column expanded and no toggle that would do nothing when pressed. This script
 * removes that attribute from every toggle when it initialises, and it is the
 * only thing that does.
 *
 * The toggle is a real <button>, so the browser turns a click, a tap, Enter, and
 * Space into the one "click" event handled below, and keyboard focus stays on it
 * through both transitions because the toggle itself is never hidden.
 */
(function () {
  "use strict";

  var COLLAPSED = "task-board__column--collapsed";
  var ICON_EXPANDED = "ti-chevron-left";
  var ICON_COLLAPSED = "ti-chevron-right";
  var LABEL_EXPANDED = "Collapse ";
  var LABEL_COLLAPSED = "Expand ";

  var toggles = document.querySelectorAll('[data-role="task-board-column-toggle"]');

  /* bind wires one toggle to its column. It resolves every element the toggle
   * acts on once, up front, and gives up on a toggle whose column or body cannot
   * be found rather than failing on its first activation: such a toggle stays
   * hidden, which is the served, script-less state of the board. */
  function bind(toggle) {
    var column = toggle.closest('[data-role="task-board-column"]');
    var body = document.getElementById(toggle.getAttribute("aria-controls") || "");
    var icon = toggle.querySelector("i");
    var label = toggle.getAttribute("aria-label") || "";
    if (!column || !body || !icon || label.indexOf(LABEL_EXPANDED) !== 0) {
      return;
    }

    /* The served name is "Collapse <HEADING> column"; what follows the verb is
     * the part both names share, so the expanded name is rebuilt exactly as the
     * server wrote it and the collapsed one differs from it in the verb alone. */
    var subject = label.slice(LABEL_EXPANDED.length);

    toggle.addEventListener("click", function () {
      var collapse = toggle.getAttribute("aria-expanded") === "true";

      body.hidden = collapse;
      column.classList.toggle(COLLAPSED, collapse);
      toggle.setAttribute("aria-expanded", collapse ? "false" : "true");
      toggle.setAttribute("aria-label", (collapse ? LABEL_COLLAPSED : LABEL_EXPANDED) + subject);

      /* The class being dropped is removed BEFORE the other is added, so an
       * expanded icon reads "ti ti-chevron-left" again, exactly as served. */
      icon.classList.remove(collapse ? ICON_EXPANDED : ICON_COLLAPSED);
      icon.classList.add(collapse ? ICON_COLLAPSED : ICON_EXPANDED);
    });

    toggle.hidden = false;
  }

  for (var i = 0; i < toggles.length; i++) {
    bind(toggles[i]);
  }
})();
