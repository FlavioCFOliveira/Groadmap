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
 *      chart that has data, and the empty state otherwise, so a missing
 *      container is not an error: that chart is simply not drawn.
 *
 * THE DATA BLOCK IS THE ONLY DATA SOURCE. Nothing is fetched, and nothing is
 * read or written anywhere else — no URL parameter, no cookie, no localStorage,
 * sessionStorage, or IndexedDB. The block carries enum values, counts, sprint
 * ids, and dates only (SPEC/DATA_FORMATS.md § Dashboard Chart Data).
 *
 * THE OPTIONS ARE TABLER'S. Every chart takes the options Tabler's own chart
 * component applies: a fixed height of 240 pixels — a number, never a
 * percentage, because row-deck stretches the cards of one row to equal height
 * and a percentage height would follow the stretched card body and overflow it —
 * the inherited font, no parent height offset, no toolbar, no animation, and
 * Tabler's grid padding; on every chart with axes a dashed grid, unpadded x-axis
 * labels with no axis tooltip, and y-axis labels padded by 4; bars 50% wide,
 * stacked, without data labels and without an x-axis border; lines 2px wide and
 * round-capped, the burndown stepped and the throughput straight, because a
 * smoothed curve draws values the data does not hold; a bottom legend, offset by
 * 12, with 5px circle markers and an 8px item margin, on every chart with more
 * than one series and on the donut, whose entries read each status and its count.
 * No option sets an ApexCharts `theme`: the dark styling comes from the vendored
 * Tabler vendors stylesheet, and ApexCharts' own theme would paint over it. The
 * toolbar stays disabled, so no chart offers a download, an export, a zoom, a
 * pan, or a selection control.
 *
 * WHOLE-NUMBER COUNT AXES. Every count axis — the vertical axis of the burndown,
 * the throughput, and the two bar charts — starts at 0 and is given a maximum and
 * a tick count that put every tick on a whole number, and an integer label
 * formatter, so no axis shows a fractional task and no two tick labels are equal.
 *
 * THE COLOURS ARE TABLER'S. Every series colour is a Tabler colour custom
 * property, `var(--tblr-<colour>)`, never a literal colour value, so the charts
 * take the colours of the vendored distribution; the status colours are those of
 * the task status badges.
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
  var SECONDARY = "var(--tblr-secondary)";
  var GREEN = "var(--tblr-green)";

  /* Tabler's chart height, 15rem, given to ApexCharts as a number. */
  var HEIGHT = 240;

  /* The donut's slice colours, in the order of the TaskStatus enum the data
   * block carries: BACKLOG, SPRINT, DOING, TESTING, COMPLETED. */
  var STATUS_COLOURS = [
    SECONDARY,
    "var(--tblr-cyan)",
    "var(--tblr-blue)",
    "var(--tblr-yellow)",
    GREEN
  ];

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

  /* baseChart is the chart block every chart shares, Tabler's own. */
  function baseChart(type, stacked) {
    return {
      type: type,
      height: HEIGHT,
      fontFamily: "inherit",
      parentHeightOffset: 0,
      stacked: stacked,
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

  /* stackTop is the tallest bar of a stacked chart: the largest per-category
   * sum of its series. */
  function stackTop(series) {
    var top = 0;
    for (var i = 0; i < series[0].data.length; i++) {
      var sum = 0;
      for (var s = 0; s < series.length; s++) {
        sum += series[s].data[i];
      }
      top = Math.max(top, sum);
    }
    return top;
  }

  function xAxis(categories, bar) {
    var axis = { categories: categories, labels: { padding: 0 }, tooltip: { enabled: false } };
    if (bar) {
      axis.axisBorder = { show: false };
    }
    return axis;
  }

  function barOptions(categories, series, colours) {
    return {
      chart: baseChart("bar", true),
      plotOptions: { bar: { columnWidth: "50%" } },
      dataLabels: { enabled: false },
      grid: { padding: GRID_PADDING, strokeDashArray: 4 },
      legend: LEGEND,
      series: series,
      colors: colours,
      xaxis: xAxis(categories, true),
      yaxis: countAxis(stackTop(series))
    };
  }

  function lineOptions(categories, series, colours, curve, legend, top) {
    return {
      chart: baseChart("line", false),
      stroke: { width: 2, lineCap: "round", curve: curve },
      grid: { padding: GRID_PADDING, strokeDashArray: 4 },
      legend: legend,
      series: series,
      colors: colours,
      xaxis: xAxis(categories, false),
      yaxis: countAxis(top)
    };
  }

  function donutOptions(labels, counts) {
    return {
      chart: baseChart("donut", false),
      grid: { padding: GRID_PADDING },
      legend: {
        show: LEGEND.show,
        position: LEGEND.position,
        offsetY: LEGEND.offsetY,
        markers: LEGEND.markers,
        itemMargin: LEGEND.itemMargin,
        formatter: function (name, opts) {
          return name + " " + counts[opts.seriesIndex];
        }
      },
      tooltip: { fillSeriesColor: false },
      labels: labels,
      series: counts,
      colors: STATUS_COLOURS
    };
  }

  function sprintLabels(ids) {
    var labels = [];
    for (var i = 0; i < ids.length; i++) {
      labels.push("#" + ids[i]);
    }
    return labels;
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
    var dates = [];
    var remaining = [];
    for (var i = 0; i < data.burndown.length; i++) {
      dates.push(data.burndown[i].date);
      remaining.push(data.burndown[i].tasks_remaining);
    }
    draw("chart-burndown", lineOptions(dates,
      [{ name: "Remaining", data: remaining }],
      [PRIMARY], "stepline", { show: false }, largest(remaining)));

    /* Completed is the first series of both stacked charts: the bottom segment
     * of every bar. */
    draw("chart-tasks-by-type", barOptions(data.tasks_by_type.types, [
      { name: "Completed", data: data.tasks_by_type.completed },
      { name: "Open", data: data.tasks_by_type.open }
    ], [GREEN, SECONDARY]));

    draw("chart-tasks-by-status", donutOptions(
      data.tasks_by_status.statuses, data.tasks_by_status.counts));

    draw("chart-tasks-per-sprint", barOptions(sprintLabels(data.tasks_per_sprint.sprints), [
      { name: "Completed", data: data.tasks_per_sprint.completed },
      { name: "Not completed", data: data.tasks_per_sprint.not_completed }
    ], [GREEN, SECONDARY]));

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
