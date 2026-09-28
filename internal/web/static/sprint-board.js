/* Groadmap sprint member-tasks board: the column collapse toggles.
 *
 * Each of the board's three column headers carries one toggle that collapses its
 * column to a narrow strip and expands it again. The toggle is PRESENTATION
 * ONLY: it changes how the board is shown and nothing the board shows. No
 * request is made, nothing is read or written anywhere — no URL parameter, no
 * cookie, no localStorage, sessionStorage, or IndexedDB — so every page load
 * derives each column's initial state again from the tasks alone, whatever state
 * the reader left the columns in (SPEC/WEB.md § Sprint Detail Sub-Template, rule
 * 3, Column collapse).
 *
 * THE INITIAL STATE COMES FROM THE TASK COUNTS. Each column element carries
 * `data-task-count`, the number of member tasks it holds, which is the only
 * input the initial state is derived from. When the script initialises it:
 *
 *   1. removes the `hidden` attribute from every toggle (see WITHOUT
 *      JAVASCRIPT below);
 *   2. reads each column's `data-task-count`; a value that is not a string of
 *      one or more ASCII decimal digits is not read, and its column is left
 *      expanded;
 *   3. when the values read sum to more than 0, collapses every column whose
 *      value reads as 0, through the SAME state change a click on its toggle
 *      performs; when they sum to 0 — an empty sprint — it changes no column, so
 *      the board keeps its three empty states in view.
 *
 * Initialisation reads nothing but the served markup and moves no keyboard
 * focus.
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
 * WITHOUT JAVASCRIPT. The server renders every column expanded and every toggle
 * in its expanded state and with the `hidden` attribute, whatever the sprint
 * holds, so a browser that runs no script shows every column expanded and no
 * toggle that would do nothing when pressed. This script removes that attribute
 * from every toggle when it initialises, and it is the only thing that does; it
 * is also the only thing that applies the initial state.
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

  /* A task count is read only when it is one or more ASCII decimal digits. */
  var COUNT = /^[0-9]+$/;

  var toggles = document.querySelectorAll('[data-role="task-board-column-toggle"]');
  var columns = document.querySelectorAll('[data-role="task-board-column"]');

  /* setCollapsed puts one bound column into the collapsed state (collapse true)
   * or the expanded state (collapse false). It is the ONE state change of this
   * file: the click handler and the initial state both go through it, so a
   * column that starts collapsed is in exactly the state a click produces. */
  function setCollapsed(bound, collapse) {
    bound.body.hidden = collapse;
    bound.column.classList.toggle(COLLAPSED, collapse);
    bound.toggle.setAttribute("aria-expanded", collapse ? "false" : "true");
    bound.toggle.setAttribute("aria-label", (collapse ? LABEL_COLLAPSED : LABEL_EXPANDED) + bound.subject);

    /* The class being dropped is removed BEFORE the other is added, so an
     * expanded icon reads "ti ti-chevron-left" again, exactly as served. */
    bound.icon.classList.remove(collapse ? ICON_EXPANDED : ICON_COLLAPSED);
    bound.icon.classList.add(collapse ? ICON_COLLAPSED : ICON_EXPANDED);
  }

  /* bind wires one toggle to its column and returns what it acts on. It resolves
   * every element the toggle acts on once, up front, and gives up on a toggle
   * whose column or body cannot be found rather than failing on its first
   * activation: such a toggle stays hidden, which is the served, script-less
   * state of the board, and bind returns null for it. */
  function bind(toggle) {
    var column = toggle.closest('[data-role="task-board-column"]');
    var body = document.getElementById(toggle.getAttribute("aria-controls") || "");
    var icon = toggle.querySelector("i");
    var label = toggle.getAttribute("aria-label") || "";
    if (!column || !body || !icon || label.indexOf(LABEL_EXPANDED) !== 0) {
      return null;
    }

    /* The served name is "Collapse <HEADING> column"; what follows the verb is
     * the part both names share, so the expanded name is rebuilt exactly as the
     * server wrote it and the collapsed one differs from it in the verb alone. */
    var bound = {
      toggle: toggle,
      column: column,
      body: body,
      icon: icon,
      subject: label.slice(LABEL_EXPANDED.length)
    };

    toggle.addEventListener("click", function () {
      setCollapsed(bound, toggle.getAttribute("aria-expanded") === "true");
    });

    toggle.hidden = false;
    return bound;
  }

  /* boundFor returns the bound toggle of a column element, or null when the
   * column's toggle could not be bound. */
  function boundFor(bindings, column) {
    for (var b = 0; b < bindings.length; b++) {
      if (bindings[b].column === column) {
        return bindings[b];
      }
    }
    return null;
  }

  /* Step 1: bind every toggle, which removes its hidden attribute. */
  var bindings = [];
  for (var i = 0; i < toggles.length; i++) {
    var bound = bind(toggles[i]);
    if (bound) {
      bindings.push(bound);
    }
  }

  /* Step 2: read each column's task count. An unreadable count is recorded as
   * -1, which neither adds to the sum nor reads as 0. */
  var counts = [];
  var total = 0;
  for (var c = 0; c < columns.length; c++) {
    var value = columns[c].getAttribute("data-task-count");
    var count = value !== null && COUNT.test(value) ? parseInt(value, 10) : -1;
    counts.push(count);
    if (count > 0) {
      total += count;
    }
  }

  /* Step 3: when the board holds a task, start every empty column collapsed. */
  if (total > 0) {
    for (var e = 0; e < columns.length; e++) {
      var empty = counts[e] === 0 ? boundFor(bindings, columns[e]) : null;
      if (empty) {
        setCollapsed(empty, true);
      }
    }
  }
})();
