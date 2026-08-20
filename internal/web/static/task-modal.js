/* Groadmap task detail modal.
 *
 * The page carries ONE empty modal shell (the "taskModalShell" sub-template).
 * When the user opens a task, this script fetches that task's fields and
 * comments from the roadmap's task detail endpoint and fills the shell, so the
 * served document carries no task's modal content at all and its size does not
 * grow with the modal content of every task (SPEC/WEB.md § Task Detail Modal,
 * One modal element, filled on demand; § Task Detail Endpoint).
 *
 * SECURITY — the single rule this file exists to keep. The task's values reach
 * the browser as JSON, so the server's html/template contextual auto-escaping no
 * longer stands between a stored value and the page structure: this script is
 * what must not interpret them. EVERY value written into the DOM here goes in
 * through the textContent property, which cannot introduce an element, an
 * attribute, or a script. This file therefore contains no innerHTML, no
 * outerHTML, no insertAdjacentHTML, no document.write, and no eval: containers
 * are emptied with replaceChildren() and built with createElement. A task title,
 * the requirement free-text, a completion summary, the specialists, and every
 * comment body are all text a user wrote through the CLI; the control-character
 * constraint in MODELS.md rejects terminal and bidirectional controls at write
 * time and does NOT reject HTML markup, so it is not a substitute for this rule
 * (SPEC/WEB.md § Task Detail Modal, Client-side rendering is text-only).
 *
 * No remote origin is contacted: the only fetch targets this same server, which
 * the Content-Security-Policy already admits through connect-src 'self'. The
 * file is served from /static/ like every other script, so the policy is
 * unchanged by this feature. The graph page fetches its data the same way.
 */
(function () {
  "use strict";

  var modalEl = document.getElementById("task-modal");
  if (!modalEl) {
    return;
  }

  var basePath = modalEl.getAttribute("data-task-base");
  var refEl = document.getElementById("task-modal-ref");
  var titleEl = document.getElementById("task-modal-title");
  var statusEl = document.getElementById("task-modal-status");
  var loadingEl = document.getElementById("task-modal-loading");
  var errorEl = document.getElementById("task-modal-error");
  var contentEl = document.getElementById("task-modal-content");

  /* The badge colour mappings of SPEC/WEB.md § Status, Priority, and Severity
   * Badge Colours. They are declared as tables rather than as band arithmetic so
   * that they can be compared value by value against the Go helpers in badge.go,
   * which are the single source of truth; internal/web/task_modal_test.go pins
   * every entry of all three against them, so the two sides cannot drift. */
  var STATUS_BADGE = {
    BACKLOG: "bg-secondary-lt",
    SPRINT: "bg-cyan-lt",
    DOING: "bg-blue-lt",
    TESTING: "bg-yellow-lt",
    COMPLETED: "bg-green-lt"
  };
  var PRIORITY_BADGE = [
    "bg-secondary-lt", // 0
    "bg-secondary-lt", // 1
    "bg-secondary-lt", // 2
    "bg-secondary-lt", // 3
    "bg-yellow-lt", // 4
    "bg-yellow-lt", // 5
    "bg-yellow-lt", // 6
    "bg-red-lt", // 7
    "bg-red-lt", // 8
    "bg-red-lt" // 9
  ];
  var SEVERITY_BADGE = [
    "bg-secondary-lt", // 0
    "bg-secondary-lt", // 1
    "bg-secondary-lt", // 2
    "bg-yellow-lt", // 3
    "bg-yellow-lt", // 4
    "bg-yellow-lt", // 5
    "bg-orange-lt", // 6
    "bg-orange-lt", // 7
    "bg-red-lt", // 8
    "bg-red-lt" // 9
  ];
  /* Every comment type renders in the neutral variant; the semantic mapping is
   * deliberately not extended to comment types. */
  var COMMENT_TYPE_BADGE = "bg-secondary-lt";

  /* The placeholder shown where a value is absent, matching what the
   * server-rendered modal used to emit for a null field. */
  var ABSENT = "—";

  /* requestToken orders the responses. Opening task A and then task B before A's
   * response arrives must never fill B's modal with A's data, so a response is
   * applied only when it is still the one being awaited. */
  var requestToken = 0;

  /* el builds one element. The text is assigned through textContent, never as
   * markup — this helper is why every value in this file lands as text. */
  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (text !== undefined && text !== null) {
      node.textContent = String(text);
    }
    return node;
  }

  /* datagridItem renders one label/value pair of the modal's metadata grid. The
   * value may be a string (written as text) or an element built by the caller. */
  function datagridItem(label, value) {
    var item = el("div", "datagrid-item");
    item.appendChild(el("div", "datagrid-title", label));
    var content = el("div", "datagrid-content");
    if (value instanceof Node) {
      content.appendChild(value);
    } else {
      content.textContent = value === null || value === undefined || value === "" ? ABSENT : String(value);
    }
    item.appendChild(content);
    return item;
  }

  /* timestampItem renders a lifecycle timestamp, muted, with the absent
   * placeholder when the value is null. */
  function timestampItem(label, value) {
    var item = el("div", "datagrid-item");
    item.appendChild(el("div", "datagrid-title", label));
    item.appendChild(el("div", "datagrid-content text-secondary", value ? value : ABSENT));
    return item;
  }

  /* idBadges renders a dependency list as reference badges, or the absent
   * placeholder when the list is empty. */
  function idBadges(ids) {
    if (!ids || ids.length === 0) {
      return ABSENT;
    }
    var wrap = el("span", "d-flex flex-wrap gap-1");
    ids.forEach(function (id) {
      wrap.appendChild(el("span", "badge bg-secondary-lt", "#" + String(id)));
    });
    return wrap;
  }

  /* textBlock renders one long free-text field. The task-modal__text class is
   * what preserves the author's line breaks and wraps the text, exactly as the
   * server-rendered modal did (SPEC/WEB.md § Frontend Rules, rule 6). */
  function textBlock(label, value) {
    var block = el("div", "mb-3");
    block.appendChild(el("div", "datagrid-title mb-1", label));
    block.appendChild(el("div", "task-modal__text", value ? value : ABSENT));
    return block;
  }

  /* commentTimeline renders the task's work log as Tabler's Timeline, in the
   * order the endpoint returned it: oldest first. It mirrors the markup of the
   * commentTimeline sub-template, which still renders the sprint's own log. */
  function commentTimeline(comments) {
    var list = el("ul", "timeline");
    comments.forEach(function (comment) {
      var item = el("li", "timeline-event");

      var iconWrap = el("div", "timeline-event-icon");
      iconWrap.appendChild(el("i", "ti ti-message"));
      item.appendChild(iconWrap);

      var card = el("div", "card timeline-event-card");
      var body = el("div", "card-body");

      var meta = el("div", "d-flex flex-wrap align-items-center gap-2 mb-2");
      meta.appendChild(el("span", "badge " + COMMENT_TYPE_BADGE, comment.type));
      meta.appendChild(el("span", "text-secondary", comment.created_at));
      if (comment.updated_at) {
        meta.appendChild(el("span", "text-secondary", "edited " + comment.updated_at));
      }
      body.appendChild(meta);
      body.appendChild(el("div", "task-modal__text", comment.body));

      card.appendChild(body);
      item.appendChild(card);
      list.appendChild(item);
    });
    return list;
  }

  /* reset clears every trace of the previously opened task. It runs before each
   * fetch, so a failure can never leave the previous task's data on display. */
  function reset() {
    refEl.textContent = "";
    titleEl.textContent = "";
    statusEl.textContent = "";
    statusEl.className = "badge ms-auto me-2";
    statusEl.hidden = true;
    errorEl.textContent = "";
    errorEl.hidden = true;
    contentEl.replaceChildren();
    loadingEl.hidden = false;
  }

  /* showError reports a read failure inside the modal. The modal stays open and
   * says what happened: it never goes blank and never shows another task's data
   * (SPEC/WEB.md § Task Detail Modal, Failure is visible in the modal). */
  function showError(taskID) {
    loadingEl.hidden = true;
    contentEl.replaceChildren();
    refEl.textContent = taskID ? "Task #" + String(taskID) : "";
    titleEl.textContent = "Detail unavailable";
    errorEl.textContent =
      "This task's detail could not be loaded. The roadmap is read through the local " +
      "server; check that it is still running and try again.";
    errorEl.hidden = false;
  }

  /* fill renders one task's full field set and its comments into the shell. */
  function fill(data) {
    var task = data.task;
    var comments = data.comments || [];

    loadingEl.hidden = true;
    errorEl.hidden = true;

    refEl.textContent = "Task #" + String(task.id);
    titleEl.textContent = task.title;
    statusEl.textContent = task.status;
    statusEl.className = "badge " + (STATUS_BADGE[task.status] || "bg-secondary-lt") + " ms-auto me-2";
    statusEl.hidden = false;

    var grid = el("div", "datagrid mb-3");
    grid.appendChild(datagridItem("Type", task.type));
    grid.appendChild(
      datagridItem("Priority", el("span", "badge " + (PRIORITY_BADGE[task.priority] || "bg-secondary-lt"), task.priority))
    );
    grid.appendChild(
      datagridItem("Severity", el("span", "badge " + (SEVERITY_BADGE[task.severity] || "bg-secondary-lt"), task.severity))
    );
    grid.appendChild(datagridItem("Specialists", task.specialists));
    grid.appendChild(
      datagridItem("Parent task", task.parent_task_id ? "#" + String(task.parent_task_id) : ABSENT)
    );
    grid.appendChild(datagridItem("Subtasks", String(task.subtask_count)));
    grid.appendChild(datagridItem("Depends on", idBadges(task.depends_on)));
    grid.appendChild(datagridItem("Blocks", idBadges(task.blocks)));
    grid.appendChild(timestampItem("Created", task.created_at));
    grid.appendChild(timestampItem("Started", task.started_at));
    grid.appendChild(timestampItem("Tested", task.tested_at));
    grid.appendChild(timestampItem("Closed", task.closed_at));

    var fragment = document.createDocumentFragment();
    fragment.appendChild(grid);
    fragment.appendChild(textBlock("Functional requirements", task.functional_requirements));
    fragment.appendChild(textBlock("Technical requirements", task.technical_requirements));
    fragment.appendChild(textBlock("Acceptance criteria", task.acceptance_criteria));
    fragment.appendChild(textBlock("Completion summary", task.completion_summary));

    var log = el("div", null);
    log.appendChild(el("div", "datagrid-title mb-1", "Comments"));
    if (comments.length > 0) {
      log.appendChild(commentTimeline(comments));
    } else {
      log.appendChild(el("div", "text-secondary", "No comments have been recorded on this task yet."));
    }
    fragment.appendChild(log);

    contentEl.replaceChildren(fragment);
  }

  /* Bootstrap fires show.bs.modal before the modal appears, carrying the trigger
   * as relatedTarget, so the fetch is driven by the framework's own event rather
   * than by a click handler this script would have to add to every trigger. The
   * trigger is a <button> on every surface, so pointer, touch, Enter and Space
   * all reach this path (SPEC/WEB.md § Task Detail Modal). */
  modalEl.addEventListener("show.bs.modal", function (event) {
    var trigger = event.relatedTarget;
    var taskID = trigger ? trigger.getAttribute("data-task-id") : null;

    reset();

    if (!taskID || !basePath) {
      showError(taskID);
      return;
    }

    requestToken += 1;
    var token = requestToken;

    fetch(basePath + "/" + encodeURIComponent(taskID) + "/data", {
      headers: { Accept: "application/json" }
    })
      .then(function (resp) {
        if (!resp.ok) {
          throw new Error("status " + resp.status);
        }
        return resp.json();
      })
      .then(function (data) {
        // A response for a task the user has since navigated away from is
        // discarded rather than painted over the task now being shown.
        if (token !== requestToken) {
          return;
        }
        if (!data || !data.task) {
          throw new Error("malformed body");
        }
        fill(data);
      })
      .catch(function () {
        if (token !== requestToken) {
          return;
        }
        showError(taskID);
      });
  });
})();
