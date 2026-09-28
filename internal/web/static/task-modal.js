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
 * attribute, or a script — with ONE exception, confined to markdownBlock below.
 * A task title, the raw requirement free-text, a completion summary, and every
 * comment body are all text a user wrote through the CLI; the control-character
 * constraint in MODELS.md rejects terminal and bidirectional controls at write
 * time and does NOT reject HTML markup, so it is not a substitute for this rule
 * (SPEC/WEB.md § Task Detail Modal, Client-side rendering is text-only).
 *
 * The exception: the five _html members of the endpoint's response —
 * functional_requirements_html, technical_requirements_html,
 * acceptance_criteria_html, and completion_summary_html on the task, and
 * body_html on each comment — are HTML the server's one Markdown renderer
 * produced, which emits no raw HTML from the source, no author attribute, and no
 * active link to a dangerous URL. markdownBlock inserts one of them, whole and
 * unmodified, through innerHTML into a markdown container; it is the only
 * markup-parsing sink in this file, and nothing but an _html member is ever
 * passed to it. This script never parses Markdown itself (SPEC/WEB.md § Markdown
 * Rendering, rules 2 and 15). The file contains no outerHTML, no
 * insertAdjacentHTML, no document.write, and no eval: containers are emptied with
 * replaceChildren() and built with createElement.
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

  /* formatTimestamp is the ONE function of this script that formats a
   * timestamp (SPEC/WEB.md § Date and Time Display, rule 5). Given a stored
   * value in the canonical format YYYY-MM-DDTHH:mm:ss.sssZ it returns the display
   * form YYYY-MM-DD HH:mm:ss: the stored UTC digits, the fractional seconds
   * truncated and never rounded, with no zone conversion and no locale. For any
   * other value it returns null, and the caller displays the value as stored.
   *
   * It is pure string parsing on purpose: a browser date facility would apply
   * the browser's time zone or locale, which rule 2 admits neither of. The rule
   * is the Go helper's canonicalTimestampDisplay (timestamp.go), check for check,
   * so a value reads the same here as on the server-rendered pages: exact shape
   * in ASCII digits, year 0001 or later, month 01-12, a day within the month's
   * length in the proleptic Gregorian calendar, hour 00-23, minute and second
   * 00-59. */
  function formatTimestamp(value) {
    if (typeof value !== "string" || value.length !== 24) {
      return null;
    }
    for (var i = 0; i < 24; i++) {
      var c = value.charCodeAt(i);
      var ok;
      if (i === 4 || i === 7) {
        ok = c === 45; // "-"
      } else if (i === 10) {
        ok = c === 84; // "T"
      } else if (i === 13 || i === 16) {
        ok = c === 58; // ":"
      } else if (i === 19) {
        ok = c === 46; // "."
      } else if (i === 23) {
        ok = c === 90; // "Z"
      } else {
        ok = c >= 48 && c <= 57;
      }
      if (!ok) {
        return null;
      }
    }
    var year = digitsValue(value, 0, 4);
    var month = digitsValue(value, 5, 7);
    var day = digitsValue(value, 8, 10);
    if (year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) {
      return null;
    }
    if (digitsValue(value, 11, 13) > 23 || digitsValue(value, 14, 16) > 59 || digitsValue(value, 17, 19) > 59) {
      return null;
    }
    return value.substring(0, 10) + " " + value.substring(11, 19);
  }

  /* digitsValue returns the value of the ASCII digits value[start:end], which
   * formatTimestamp has already validated. */
  function digitsValue(value, start, end) {
    var n = 0;
    for (var i = start; i < end; i++) {
      n = n * 10 + (value.charCodeAt(i) - 48);
    }
    return n;
  }

  /* daysInMonth returns the length of month (1-12) of year in the proleptic
   * Gregorian calendar. */
  function daysInMonth(year, month) {
    if (month === 2) {
      return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28;
    }
    if (month === 4 || month === 6 || month === 9 || month === 11) {
      return 30;
    }
    return 31;
  }

  /* timestampNode returns the DOM node that displays one stored timestamp
   * (SPEC/WEB.md § Date and Time Display, rules 6 and 7): a <time> element whose
   * datetime attribute is the stored value verbatim and whose text is the display
   * form; the absent placeholder when the value is unset; or, for a value not in
   * the canonical format, the stored text unchanged with no <time> element. The
   * attribute goes in through setAttribute and the text through textContent, so
   * neither is interpreted as markup. */
  function timestampNode(value) {
    if (value === null || value === undefined || value === "") {
      return document.createTextNode(ABSENT);
    }
    var display = formatTimestamp(value);
    if (display === null) {
      return document.createTextNode(String(value));
    }
    var node = document.createElement("time");
    node.setAttribute("datetime", value);
    node.textContent = display;
    return node;
  }

  /* timestampItem renders a lifecycle timestamp, muted, through timestampNode. */
  function timestampItem(label, value) {
    var item = el("div", "datagrid-item");
    item.appendChild(el("div", "datagrid-title", label));
    var content = el("div", "datagrid-content text-secondary");
    content.appendChild(timestampNode(value));
    item.appendChild(content);
    return item;
  }

  /* commitItem renders one of the two commit hashes. The value is monospaced,
   * because a hash is read character by character when it is compared against a
   * repository, and it is not a link: the modal is read-only and offline and
   * holds no repository URL from which a code-host link could be built
   * (SPEC/WEB.md § Task Detail Modal, Fields shown). An absent hash takes the
   * same placeholder as every other absent field, so a task that has not
   * started and one whose commit_close was cleared by a reopen read alike. */
  function commitItem(label, value) {
    var item = el("div", "datagrid-item");
    item.appendChild(el("div", "datagrid-title", label));
    if (value) {
      item.appendChild(el("div", "datagrid-content font-monospace text-truncate", value));
    } else {
      item.appendChild(el("div", "datagrid-content text-secondary", ABSENT));
    }
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

  /* markdownBlock places one _html member — the server's Markdown renderer's
   * HTML for one field — into a Tabler markdown container, whole and unmodified.
   * It is the ONE markup-parsing sink of this file, and its argument is only ever
   * an _html member of the endpoint's response (see the SECURITY note above;
   * SPEC/WEB.md § Markdown Rendering, rules 13 and 15). */
  function markdownBlock(renderedHTML) {
    var node = el("div", "markdown");
    node.innerHTML = renderedHTML;
    return node;
  }

  /* markdownField renders one of the task's Markdown fields under its label: the
   * field's _html member in its markdown container, or the absent placeholder
   * when the field is empty or null, as every other empty field is presented
   * (SPEC/WEB.md § Task Detail Modal, Fields shown). */
  function markdownField(label, renderedHTML) {
    var block = el("div", "mb-3");
    block.appendChild(el("div", "datagrid-title mb-1", label));
    if (renderedHTML) {
      block.appendChild(markdownBlock(renderedHTML));
    } else {
      block.appendChild(el("div", "text-secondary", ABSENT));
    }
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
      var created = el("span", "text-secondary");
      created.appendChild(timestampNode(comment.created_at));
      meta.appendChild(created);
      if (comment.updated_at) {
        // The edited marker is a label, so it stays outside the <time> element.
        var edited = el("span", "text-secondary", "edited ");
        edited.appendChild(timestampNode(comment.updated_at));
        meta.appendChild(edited);
      }
      body.appendChild(meta);
      body.appendChild(markdownBlock(comment.body_html));

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
    grid.appendChild(commitItem("Commit open", task.commit_open));
    grid.appendChild(commitItem("Commit close", task.commit_close));

    var fragment = document.createDocumentFragment();
    fragment.appendChild(grid);
    fragment.appendChild(markdownField("Functional requirements", task.functional_requirements_html));
    fragment.appendChild(markdownField("Technical requirements", task.technical_requirements_html));
    fragment.appendChild(markdownField("Acceptance criteria", task.acceptance_criteria_html));
    fragment.appendChild(markdownField("Completion summary", task.completion_summary_html));

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
