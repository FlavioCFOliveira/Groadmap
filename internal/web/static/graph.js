/* Groadmap knowledge-graph viewer.
 *
 * Loads the roadmap's nodes and edges from the local graph data endpoint
 * and renders them with the vendored Cytoscape.js (already loaded from
 * /static/cytoscape.min.js). No remote origin is contacted: the only fetch
 * targets this same server's data endpoint (SPEC/WEB.md
 * § Knowledge-Graph Visualisation Library).
 *
 * The visualisation supports pan, pinch/scroll zoom, and tap-to-select on
 * both pointer and touch devices; tapping a node or edge opens a detail
 * panel showing its labels/type and properties, so detail is reachable
 * without a mouse hover (SPEC/WEB.md § Responsive and Mobile-First Design).
 */
(function () {
  "use strict";

  var cyEl = document.getElementById("cy");
  if (!cyEl) {
    return;
  }
  var dataUrl = cyEl.getAttribute("data-graph-url");
  var emptyEl = document.getElementById("empty-graph");
  var panelEl = document.getElementById("detail-panel");
  var panelTitle = document.getElementById("detail-title");
  var panelBody = document.getElementById("detail-body");
  var panelClose = document.getElementById("detail-close");

  function showEmpty() {
    if (emptyEl) {
      emptyEl.hidden = false;
    }
  }

  function hidePanel() {
    if (panelEl) {
      panelEl.hidden = true;
    }
  }

  // Render a label/value pair into the detail panel.
  function addRow(label, value) {
    var dt = document.createElement("dt");
    dt.textContent = label;
    var dd = document.createElement("dd");
    dd.textContent = value;
    panelBody.appendChild(dt);
    panelBody.appendChild(dd);
  }

  function showDetail(title, props) {
    if (!panelEl) {
      return;
    }
    panelTitle.textContent = title;
    panelBody.innerHTML = "";
    var keys = Object.keys(props || {});
    if (keys.length === 0) {
      addRow("properties", "(none)");
    } else {
      keys.sort();
      keys.forEach(function (k) {
        var v = props[k];
        if (v !== null && typeof v === "object") {
          v = JSON.stringify(v);
        }
        addRow(k, String(v));
      });
    }
    panelEl.hidden = false;
  }

  if (panelClose) {
    panelClose.addEventListener("click", hidePanel);
  }

  function buildElements(data) {
    var elements = [];
    var nodeIds = Object.create(null);

    (data.nodes || []).forEach(function (n) {
      var id = String(n.id);
      nodeIds[id] = true;
      var labels = n.labels || [];
      var props = n.properties || {};
      var caption = props.key || props.name || props.path || (labels[0] || "node");
      elements.push({
        group: "nodes",
        data: {
          id: id,
          caption: String(caption),
          labels: labels,
          props: props
        }
      });
    });

    (data.edges || []).forEach(function (e) {
      var s = String(e.startId);
      var t = String(e.endId);
      // Only add an edge whose endpoints are both present in this response.
      if (!nodeIds[s] || !nodeIds[t]) {
        return;
      }
      elements.push({
        group: "edges",
        data: {
          id: "e" + String(e.id),
          source: s,
          target: t,
          etype: e.type || "",
          props: e.properties || {}
        }
      });
    });

    return elements;
  }

  function initGraph(elements) {
    var cy = cytoscape({
      container: cyEl,
      elements: elements,
      // Touch- and small-viewport-friendly: gestures default on; bound the
      // zoom so a pinch cannot lose the graph off-screen.
      minZoom: 0.1,
      maxZoom: 4,
      wheelSensitivity: 0.3,
      autoungrabify: false,
      style: [
        {
          selector: "node",
          style: {
            "background-color": "#2563eb",
            "label": "data(caption)",
            "color": "#1c2330",
            "font-size": "10px",
            "text-valign": "bottom",
            "text-halign": "center",
            "text-margin-y": 4,
            "width": 24,
            "height": 24
          }
        },
        {
          selector: "edge",
          style: {
            "width": 2,
            "line-color": "#94a3b8",
            "target-arrow-color": "#94a3b8",
            "target-arrow-shape": "triangle",
            "curve-style": "bezier",
            "label": "data(etype)",
            "font-size": "8px",
            "color": "#6b7280",
            "text-rotation": "autorotate"
          }
        },
        {
          selector: ":selected",
          style: {
            "background-color": "#f59e0b",
            "line-color": "#f59e0b",
            "target-arrow-color": "#f59e0b"
          }
        }
      ],
      layout: {
        name: "cose",
        animate: false,
        padding: 30
      }
    });

    cy.on("tap", "node", function (evt) {
      var d = evt.target.data();
      var title = (d.labels && d.labels.length ? d.labels.join(", ") : "Node") + " #" + d.id;
      showDetail(title, d.props);
    });

    cy.on("tap", "edge", function (evt) {
      var d = evt.target.data();
      showDetail((d.etype || "Edge"), d.props);
    });

    // Tapping the empty background dismisses the detail panel.
    cy.on("tap", function (evt) {
      if (evt.target === cy) {
        hidePanel();
      }
    });
  }

  fetch(dataUrl, { headers: { Accept: "application/json" } })
    .then(function (resp) {
      if (!resp.ok) {
        throw new Error("graph data request failed: " + resp.status);
      }
      return resp.json();
    })
    .then(function (data) {
      var elements = buildElements(data);
      if (elements.length === 0) {
        showEmpty();
        return;
      }
      initGraph(elements);
    })
    .catch(function () {
      showEmpty();
    });
})();
