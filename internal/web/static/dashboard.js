/* Groadmap roadmap dashboard: the charts and the status breakdown.
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
 *      each chart with the vendored ApexCharts library into its container when
 *      the container is present. The server renders a container only for a
 *      chart that has data, and the empty state — or, for a KPI card whose
 *      value is 0, nothing — otherwise, so a missing container is not an error:
 *      that chart is simply not drawn.
 *
 * THE DATA BLOCK IS THE ONLY DATA SOURCE. Nothing is fetched, and nothing is
 * read or written anywhere else — no URL parameter, no cookie, no localStorage,
 * sessionStorage, or IndexedDB. The block carries enum values, counts, and dates
 * only (SPEC/DATA_FORMATS.md § Dashboard Chart Data).
 *
 * THE OPTIONS ARE TABLER'S. Every chart takes the inherited font, no parent
 * height offset, no toolbar, and no animation. The two line charts — the burndown
 * and the throughput — take the options Tabler's own chart component applies: a
 * fixed height of 240 pixels — a number, never a percentage, because row-deck
 * stretches the cards of one row to equal height and a percentage height would
 * follow the stretched card body and overflow it — Tabler's grid padding, a
 * dashed grid, unpadded x-axis labels with no axis tooltip, y-axis labels padded
 * by 4, and lines 2px wide and round-capped, the burndown stepped and the
 * throughput straight, because a smoothed curve draws values the data does not
 * hold. The throughput, the one chart with more than one series, shows a bottom
 * legend, offset by 12, with 5px circle markers and an 8px item margin.
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
 * WHOLE-NUMBER COUNT AXES. Every count axis — the vertical axis of the burndown
 * and of the throughput — starts at 0 and is given a maximum and a tick count
 * that put every tick on a whole number, and an integer label formatter, so no
 * axis shows a fractional task and no two tick labels are equal.
 *
 * THE COLOURS ARE TABLER'S. Every series colour is a Tabler colour custom
 * property, `var(--tblr-<colour>)`, never a literal colour value, so the charts
 * take the colours of the vendored distribution; a donut slice takes the solid
 * counterpart of its task type's badge colour, keyed on the type and never on
 * the slice's position, so a type keeps its colour in all three donuts.
 *
 * WITHOUT JAVASCRIPT. The KPI values, the current sprint's lines and legend, the
 * card-tables, the empty states, and each chart's aria-label are all in the
 * served markup; only the drawing and the segment widths need this script.
 */
(function () {
  "use strict";

  /* A share is read only when it is a decimal number with at most two decimal
   * places, the form the server writes. */
  var SHARE = /^[0-9]+(\.[0-9]{1,2})?$/;

  var PRIMARY = "var(--tblr-primary)";
  var GREEN = "var(--tblr-green)";

  /* Tabler's chart height, 15rem, given to ApexCharts as a number. */
  var HEIGHT = 240;

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

  /* The number of intervals a count axis is divided into at most. */
  var MAX_TICKS = 5;

  function sizeSegments() {
    var segments = document.querySelectorAll('[data-role="sprint-breakdown"] [data-percent]');
    for (var i = 0; i < segments.length; i++) {
      var share = segments[i].getAttribute("data-percent");
      if (SHARE.test(share)) {
        segments[i].style.width = share + "%";
      }
    }
  }

  /* baseChart is the chart block every line chart shares, Tabler's own. */
  function baseChart(type) {
    return {
      type: type,
      height: HEIGHT,
      fontFamily: "inherit",
      parentHeightOffset: 0,
      toolbar: { show: false },
      animations: { enabled: false }
    };
  }

  var GRID_PADDING = { top: -20, right: 0, left: -4, bottom: -4 };
  var LEGEND = {
    show: true,
    position: "bottom",
    offsetY: 12,
    markers: { size: 5, strokeWidth: 0, shape: "circle" },
    itemMargin: { horizontal: 8, vertical: 8 }
  };

  /* wholeNumber writes a tick label as an integer. The axis bounds below put
   * every tick on a whole number, so rounding changes no label. */
  function wholeNumber(value) {
    return String(Math.round(value));
  }

  /* countAxis is a vertical axis for counts up to top: it starts at 0 and ends
   * at a multiple of a whole step, with one tick per step, so every tick is a
   * whole number and no two labels are equal. */
  function countAxis(top) {
    var max = Math.max(1, top);
    var step = Math.max(1, Math.ceil(max / MAX_TICKS));
    var ticks = Math.ceil(max / step);
    return {
      min: 0,
      max: step * ticks,
      tickAmount: ticks,
      labels: { padding: 4, formatter: wholeNumber }
    };
  }

  function largest(values) {
    var top = 0;
    for (var i = 0; i < values.length; i++) {
      top = Math.max(top, values[i]);
    }
    return top;
  }

  function xAxis(categories) {
    return { categories: categories, labels: { padding: 0 }, tooltip: { enabled: false } };
  }

  function lineOptions(categories, series, colours, curve, legend, top) {
    return {
      chart: baseChart("line"),
      stroke: { width: 2, lineCap: "round", curve: curve },
      grid: { padding: GRID_PADDING, strokeDashArray: 4 },
      legend: legend,
      series: series,
      colors: colours,
      xaxis: xAxis(categories),
      yaxis: countAxis(top)
    };
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

    var dates = [];
    var remaining = [];
    for (var i = 0; i < data.burndown.length; i++) {
      dates.push(data.burndown[i].date);
      remaining.push(data.burndown[i].tasks_remaining);
    }
    draw("chart-burndown", lineOptions(dates,
      [{ name: "Remaining", data: remaining }],
      [PRIMARY], "stepline", { show: false }, largest(remaining)));

    draw("chart-throughput", lineOptions(data.throughput.weeks, [
      { name: "Created", data: data.throughput.created },
      { name: "Completed", data: data.throughput.completed }
    ], [PRIMARY, GREEN], "straight", LEGEND,
      Math.max(largest(data.throughput.created), largest(data.throughput.completed))));
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
