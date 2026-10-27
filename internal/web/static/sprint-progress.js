/* Groadmap sprint page: the Sprint progress card's gauge.
 *
 * The page states every number on the server; this script only DRAWS. It parses
 * the chart data block, the
 * `<script type="application/json" id="sprint-progress-data">` element, and draws
 * the Sprint progress card's gauge with the vendored ApexCharts library into its
 * container, `#chart-sprint-progress` (SPEC/WEB.md § Sprint Detail Sub-Template,
 * rule 7; § Dashboard Chart Library). It does nothing else: the member-tasks
 * board belongs to static/sprint-board.js alone.
 *
 * THE DATA BLOCK IS THE ONLY DATA SOURCE. Nothing is fetched, and nothing is
 * read or written anywhere else — no URL parameter, no cookie, no localStorage,
 * sessionStorage, or IndexedDB. The block carries enum values and counts only
 * (SPEC/DATA_FORMATS.md § Sprint Progress Chart Data).
 *
 * THE OPTIONS ARE TABLER'S. Every chart takes the inherited font, no parent
 * height offset, no toolbar, and no animation; the gauge takes the options of
 * the radial chart of Tabler's "Active Users" card (ChartRadial.astro): a
 * horseshoe from -120 to 120 degrees, a 70% hollow with a 15 margin, a fixed
 * height of 200, and round line caps. Beyond that model, which draws one series,
 * it draws three, one per board column, and shows a total at its centre.
 *
 * THE CENTRE. While no arc is hovered, the centre shows `COMPLETED` and the
 * CLOSED series' percentage, rounded down on the server. While an arc is
 * hovered, it shows that series' label and the group's task COUNT, not its
 * percentage. ApexCharts hands the value formatter the hovered arc's value
 * alone, so the hovered series' index is recorded by the dataPointMouseEnter
 * event, which ApexCharts fires on the arc before it prints the centre labels.
 *
 * No option sets an ApexCharts `theme`: the dark styling comes from the vendored
 * Tabler vendors stylesheet, which also strokes the gauge's track in
 * var(--tblr-border-color), and ApexCharts' own theme would paint over it. The
 * toolbar stays disabled, so the chart offers no download, export, zoom, pan, or
 * selection control.
 *
 * THE COLOURS ARE TABLER'S. Every series colour is a Tabler colour custom
 * property, `var(--tblr-<colour>)`, never a literal colour value: each arc takes
 * the solid counterpart of the badge colour of its column's canonical status,
 * the colour of the column's count badge and of its legend dot, keyed on the
 * column heading and never on the arc's position.
 *
 * WITHOUT JAVASCRIPT. The subheader, the total, the legend with its three
 * counts, and the gauge's aria-label are all in the served markup; only the
 * drawing of the gauge needs this script.
 */
(function () {
  "use strict";

  /* The gauge's height, given to ApexCharts as a number. */
  var GAUGE_HEIGHT = 200;

  /* The heading of the column whose percentage the centre shows. */
  var COMPLETED_COLUMN = "CLOSED";

  /* The label of the centre while no arc is hovered. */
  var COMPLETED_LABEL = "COMPLETED";

  /* The arc colour of each board column: the solid counterpart of the badge
   * colour of the column's canonical status — SPRINT's bg-cyan-lt, DOING's
   * bg-blue-lt, and COMPLETED's bg-green-lt (SPEC/WEB.md § Dashboard Chart
   * Library, rule 5). */
  var COLUMN_COLOURS = {
    WAITING: "var(--tblr-cyan)",
    DOING: "var(--tblr-blue)",
    CLOSED: "var(--tblr-green)"
  };

  /* gaugeOptions is the Sprint progress gauge: one series per board column, in
   * the order of columns, each valued at its percentage and coloured by its
   * column. */
  function gaugeOptions(byColumn) {
    var columns = byColumn.columns;
    var counts = byColumn.counts;
    var colours = [];
    for (var i = 0; i < columns.length; i++) {
      colours.push(COLUMN_COLOURS[columns[i]]);
    }
    var completed = byColumn.percents[columns.indexOf(COMPLETED_COLUMN)];
    var hovered = -1;
    return {
      chart: {
        type: "radialBar",
        height: GAUGE_HEIGHT,
        fontFamily: "inherit",
        parentHeightOffset: 0,
        toolbar: { show: false },
        animations: { enabled: false },
        events: {
          dataPointMouseEnter: function (event, chartContext, opts) {
            hovered = opts.dataPointIndex;
          },
          dataPointMouseLeave: function () {
            hovered = -1;
          }
        }
      },
      plotOptions: {
        radialBar: {
          startAngle: -120,
          endAngle: 120,
          hollow: { margin: 15, size: "70%" },
          dataLabels: {
            name: {
              show: true,
              offsetY: -20,
              fontSize: "14px",
              fontFamily: "inherit",
              fontWeight: 400,
              color: "var(--tblr-secondary)"
            },
            value: {
              show: true,
              fontSize: "30px",
              fontWeight: 700,
              fontFamily: "inherit",
              offsetY: 0,
              color: "var(--tblr-body-color)",
              formatter: function (value) {
                if (hovered >= 0 && hovered < counts.length) {
                  return String(counts[hovered]);
                }
                return String(value);
              }
            },
            total: {
              show: true,
              label: COMPLETED_LABEL,
              formatter: function () {
                return completed + "%";
              }
            }
          }
        }
      },
      stroke: { lineCap: "round" },
      labels: columns.slice(),
      series: byColumn.percents.slice(),
      colors: colours
    };
  }

  var container = document.getElementById("chart-sprint-progress");
  var block = document.getElementById("sprint-progress-data");
  if (!container || !block || typeof ApexCharts === "undefined") {
    return;
  }
  var data;
  try {
    data = JSON.parse(block.textContent);
  } catch (e) {
    return;
  }
  new ApexCharts(container, gaugeOptions(data.tasks_by_column)).render();
}());
