/* Groadmap roadmap dashboard: the KPI donuts and the status breakdown.
 *
 * The page states every number on the server; this script only DRAWS. It does
 * two things and nothing else (SPEC/WEB.md § Roadmap Dashboard Page, Page script;
 * § Dashboard Chart Library):
 *
 *   1. it sets the width of each segment of the current sprint's status
 *      breakdown from the segment's `data-percent` attribute, through the
 *      element's `style.width` property — the CSSOM, which the
 *      Content-Security-Policy permits. The server writes no `style` attribute;
 *   2. it parses the chart data block, the
 *      `<script type="application/json" id="dashboard-data">` element, and draws
 *      each of the three KPI type-breakdown donuts — the page's only charts —
 *      with the vendored ApexCharts library into its container when the
 *      container is present. The server renders a donut's container only when
 *      its KPI card's value is greater than 0, so a missing container is not an
 *      error: that donut is simply not drawn.
 *
 * THE DATA BLOCK IS THE ONLY DATA SOURCE. Nothing is fetched, and nothing is
 * read or written anywhere else — no URL parameter, no cookie, no localStorage,
 * sessionStorage, or IndexedDB. The block carries enum values and counts only
 * (SPEC/DATA_FORMATS.md § Dashboard Chart Data).
 *
 * THE OPTIONS ARE TABLER'S. Every chart takes the inherited font, no parent
 * height offset, no toolbar, and no animation.
 *
 * THE KPI TYPE-BREAKDOWN DONUTS. The Open tasks, Completed, and Backlog cards
 * each carry a donut of their tasks by type, drawn at a fixed 64 by 64 pixels in
 * sparkline mode, with no legend and no slice label. Each donut takes, in the
 * order of `tasks_by_type.types`, the entries of its array greater than 0, one
 * slice per entry, and its tooltip reads `<TYPE>: <n> (<p>%)`, p rounded down.
 *
 * No option sets an ApexCharts `theme`: the dark styling comes from the vendored
 * Tabler vendors stylesheet, and ApexCharts' own theme would paint over it. The
 * toolbar stays disabled, so no chart offers a download, an export, a zoom, a
 * pan, or a selection control.
 *
 * THE COLOURS ARE TABLER'S. Every series colour is a Tabler colour custom
 * property, `var(--tblr-<colour>)`, never a literal colour value, so the donuts
 * take the colours of the vendored distribution; a slice takes the solid
 * counterpart of its task type's badge colour, keyed on the type and never on
 * the slice's position, so a type keeps its colour in all three donuts.
 *
 * WITHOUT JAVASCRIPT. The KPI values, the current sprint's lines and legend, the
 * card-tables, the empty states, and each donut's aria-label are all in the
 * served markup; only the drawing and the segment widths need this script.
 */
(function () {
  "use strict";

  /* A share is read only when it is a decimal number with at most two decimal
   * places, the form the server writes. */
  var SHARE = /^[0-9]+(\.[0-9]{1,2})?$/;

  /* The KPI type-breakdown donut's size, the 4rem square, given to ApexCharts
   * as a number. */
  var DONUT_SIZE = 64;

  /* The slice colour of each task type: the solid counterpart of the type's
   * badge colour (SPEC/WEB.md § Dashboard Chart Library, rule 5). */
  var TYPE_COLOURS = {
    USER_STORY: "var(--tblr-green)",
    TASK: "var(--tblr-blue)",
    BUG: "var(--tblr-red)",
    SUB_TASK: "var(--tblr-azure)",
    EPIC: "var(--tblr-purple)",
    REFACTOR: "var(--tblr-indigo)",
    CHORE: "var(--tblr-secondary)",
    SPIKE: "var(--tblr-yellow)",
    DESIGN_UX: "var(--tblr-pink)",
    IMPROVEMENT: "var(--tblr-teal)"
  };

  function sizeSegments() {
    var segments = document.querySelectorAll('[data-role="sprint-breakdown"] [data-percent]');
    for (var i = 0; i < segments.length; i++) {
      var share = segments[i].getAttribute("data-percent");
      if (SHARE.test(share)) {
        segments[i].style.width = share + "%";
      }
    }
  }

  /* typeDonutOptions is a KPI type-breakdown donut: one slice per type whose
   * count is greater than 0, in the order of types, each in its type's colour.
   * The slices sum to the card's value, the whole of each tooltip percentage. */
  function typeDonutOptions(types, counts) {
    var labels = [];
    var series = [];
    var colours = [];
    var total = 0;
    for (var i = 0; i < types.length; i++) {
      if (counts[i] > 0) {
        labels.push(types[i]);
        series.push(counts[i]);
        colours.push(TYPE_COLOURS[types[i]]);
        total += counts[i];
      }
    }
    return {
      chart: {
        type: "donut",
        height: DONUT_SIZE,
        width: DONUT_SIZE,
        fontFamily: "inherit",
        parentHeightOffset: 0,
        sparkline: { enabled: true },
        toolbar: { show: false },
        animations: { enabled: false }
      },
      legend: { show: false },
      dataLabels: { enabled: false },
      tooltip: {
        fillSeriesColor: false,
        y: {
          formatter: function (value) {
            return value + " (" + Math.floor(value * 100 / total) + "%)";
          }
        }
      },
      labels: labels,
      series: series,
      colors: colours
    };
  }

  /* draw renders one chart into the container of the given id, when the server
   * rendered that container. */
  function draw(id, options) {
    var container = document.getElementById(id);
    if (!container) {
      return;
    }
    new ApexCharts(container, options).render();
  }

  function drawCharts(data) {
    var byType = data.tasks_by_type;
    draw("chart-open-by-type", typeDonutOptions(byType.types, byType.open));
    draw("chart-completed-by-type", typeDonutOptions(byType.types, byType.completed));
    draw("chart-backlog-by-type", typeDonutOptions(byType.types, byType.backlog));
  }

  sizeSegments();

  var block = document.getElementById("dashboard-data");
  if (!block || typeof ApexCharts === "undefined") {
    return;
  }
  var data;
  try {
    data = JSON.parse(block.textContent);
  } catch (e) {
    return;
  }
  drawCharts(data);
}());
