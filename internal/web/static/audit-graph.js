/* Groadmap audit history tree.
 *
 * Draws the audit log's branching paths as a git-style history: one lane per
 * path, one point per audit entry, with the branch and merge points that open
 * and close a sprint's lane (SPEC/WEB.md § Audit History Tree).
 *
 * THE SERVER DECIDED, THIS DRAWS. Which lane an entry belongs to, and whether
 * it opens or merges that lane, are computed on the server and delivered as
 * data-* attributes on the table rows (SPEC/WEB.md § Audit History Paths). This
 * file derives none of it: it neither reads the operation names to guess a
 * branch point nor resolves sprint membership. That keeps one model, on the
 * side that can be tested without a browser.
 *
 * IT READS THE TABLE. The rows already in the document are the source, so the
 * page carries no second, inline copy of the same entries — which it could not
 * carry anyway, the Content-Security-Policy being script-src 'self' with no
 * inline script allowed.
 *
 * ORDER IS REVERSED HERE. The table renders performed_at DESC, most recent
 * first, as specified. A tree must be built oldest first: a lane has to exist
 * before a point can land on it. The reversal is a display concern and lives
 * here, not in the server model, where it would have silently changed the
 * table's own order (SPEC/WEB.md § Audit History Tree, rule 2).
 *
 * SECURITY. Every value written into the drawing is text taken from the
 * server-rendered attributes and passed to the library as a plain string. This
 * file contains no innerHTML, no outerHTML, no insertAdjacentHTML and no
 * document.write.
 */
(function () {
  "use strict";

  var container = document.querySelector('[data-role="audit-graph"]');
  if (!container) {
    return;
  }

  /* The library is vendored and served from /static/, but a page that somehow
   * loaded without it must not throw: the table below is the reading that
   * always works, and it is already in the document. */
  if (typeof GitgraphJS === "undefined" || !GitgraphJS.createGitgraph) {
    return;
  }

  var rows = document.querySelectorAll('[data-role="audit-entry"]');
  if (rows.length === 0) {
    return;
  }

  /* Lane colours come from the vendored Tabler palette rather than from hexes
   * written here, so the tree follows the theme the rest of the page uses: the
   * custom properties are read from the live document, which means the values
   * are the dark theme's wherever the dark theme is what applies. A property
   * that resolves to nothing falls back to the library's own default by being
   * dropped from the list. */
  function themeColor(name, fallback) {
    var value = getComputedStyle(document.documentElement)
      .getPropertyValue(name)
      .trim();
    return value || fallback;
  }

  var laneColors = [
    themeColor("--tblr-blue", "#066fd1"),
    themeColor("--tblr-green", "#2fb344"),
    themeColor("--tblr-purple", "#ae3ec9"),
    themeColor("--tblr-orange", "#f76707"),
    themeColor("--tblr-cyan", "#17a2b8"),
    themeColor("--tblr-pink", "#d6336c"),
  ];
  var textColor = themeColor("--tblr-body-color", "#e5e7eb");

  var template = GitgraphJS.templateExtend(GitgraphJS.TemplateName.Metro, {
    colors: laneColors,
    branch: {
      lineWidth: 3,
      spacing: 46,
      label: { font: "500 12px Inter, sans-serif" },
    },
    commit: {
      spacing: 42,
      dot: { size: 7 },
      message: {
        /* The author and the hash are git's fields, not this log's: an audit
         * entry has no author, and its id is shown in the table beside it. What
         * a point states is the operation, the entity it names, and when it
         * happened. */
        displayAuthor: false,
        displayHash: false,
        color: textColor,
        font: "400 13px Inter, sans-serif",
      },
    },
  });

  var graph = GitgraphJS.createGitgraph(container, {
    template: template,
    responsive: true,
  });

  var mainPath = container.getAttribute("data-main-path") || "roadmap";
  var main = graph.branch(mainPath);
  var lanes = {};
  lanes[mainPath] = main;

  /* laneFor returns the lane an entry belongs to, opening it off the main line
   * the first time it is seen. A lane whose opening entry falls on another page
   * of the log is opened here just the same: the page shows one page of
   * entries, and a lane simply beginning at its first entry on this page is not
   * an error (SPEC/WEB.md § Audit History Paths, rule 4). */
  function laneFor(path, label) {
    if (!lanes[path]) {
      lanes[path] = main.branch({ name: label || path });
    }
    return lanes[path];
  }

  /* Oldest first. Array.prototype.slice turns the static NodeList into an array
   * so it can be reversed without touching the document. */
  var entries = Array.prototype.slice.call(rows).reverse();

  entries.forEach(function (row) {
    var path = row.getAttribute("data-path");
    if (!path) {
      return;
    }

    var lane = laneFor(path, row.getAttribute("data-path-label"));
    var entity = row.getAttribute("data-entity") || "";
    var entityID = row.getAttribute("data-entity-id") || "";

    /* A lane begins at the first point it receives on this page, which is
     * normally the sprint's creation but need not be: a task's operations ride
     * its CURRENT sprint's lane, so an operation recorded before that sprint
     * existed lands there too. The point where the sprint was actually opened
     * therefore carries a mark of its own rather than being implied by the
     * lane's start (SPEC/WEB.md § Audit History Tree, rule 4). */
    var commit = {
      subject: row.getAttribute("data-op") || "",
      body: entity + " #" + entityID + "  ·  " + (row.getAttribute("data-at") || ""),
    };
    if (row.getAttribute("data-opens")) {
      commit.tag = "opened";
    }
    lane.commit(commit);

    /* A sprint closing merges its lane back into the main line. The server
     * marked the entry; this file does not decide which operation merges. */
    if (row.getAttribute("data-merges")) {
      main.merge(lane);
    }
  });
})();
