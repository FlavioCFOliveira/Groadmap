/* Groadmap knowledge-graph viewer.
 *
 * Loads the roadmap's nodes and edges from the local graph data endpoint and
 * renders them with the vendored D3.js (already loaded from
 * /static/vendor/d3/d3.min.js, with the d3-sankey plugin from
 * /static/vendor/d3/d3-sankey.min.js). No remote origin is contacted: the only
 * fetch targets this same server's data endpoint (SPEC/WEB.md
 * § Knowledge-Graph Visualisation Library).
 *
 * The page offers the D3 gallery "Networks"-section layouts through a dropdown,
 * defaulting to Force-directed. The graph data is fetched once and kept in
 * memory; changing the dropdown re-renders the same data in the chosen layout
 * without a re-fetch (SPEC/WEB.md Functional Requirement 7, Acceptance
 * Criterion 10).
 *
 * Every layout draws into a single SVG inside the graph container, supports
 * pan, pinch/scroll zoom, and tap-to-select on both pointer and touch devices
 * (d3.zoom binds touch gestures). Tapping a node or edge opens a detail panel
 * showing its labels/type and properties, so detail is reachable without a
 * mouse hover; tapping empty background dismisses it (SPEC/WEB.md § Responsive
 * and Mobile-First Design). Layouts that need a constrained data shape (Sankey
 * needs an acyclic graph; bundling needs a hierarchy) degrade gracefully with a
 * read-only in-place message instead of erroring.
 */
(function () {
  "use strict";

  var graphEl = document.getElementById("graph");
  if (!graphEl || typeof d3 === "undefined") {
    return;
  }

  var dataUrl = graphEl.getAttribute("data-graph-url");
  var emptyEl = document.getElementById("empty-graph");
  var panelEl = document.getElementById("detail-panel");
  var panelTitle = document.getElementById("detail-title");
  var panelBody = document.getElementById("detail-body");
  var panelClose = document.getElementById("detail-close");
  var layoutSelect = document.getElementById("layout-select");

  // Query-bar elements (SPEC/WEB.md § Graph Query Bar). The query box is
  // pre-filled with the default query in the template; the Search button
  // re-fetches the data endpoint with the current query text (q) and the
  // current dropdown value (limit) and re-renders in the selected layout.
  var queryForm = document.getElementById("query-bar");
  var queryInput = document.getElementById("query-input");
  var limitSelect = document.getElementById("limit-select");
  var queryError = document.getElementById("query-error");

  // Labels-sidebar elements (SPEC/WEB.md § Graph Labels Sidebar). The lists are
  // populated client-side from the same fetched graph data; no new request.
  var nodeLabelsList = document.getElementById("node-labels");
  var edgeTypesList = document.getElementById("edge-types");
  var nodeLabelsEmpty = document.getElementById("node-labels-empty");
  var edgeTypesEmpty = document.getElementById("edge-types-empty");
  // Section-total elements (SPEC/WEB.md § Graph Labels Sidebar, rule 11). They
  // show the distinct-node total and the edge total alongside each section
  // header, recomputed from the fetched data on every search.
  var nodeLabelsTotal = document.getElementById("node-labels-total");
  var edgeTypesTotal = document.getElementById("edge-types-total");

  // Collapse/expand control (SPEC/WEB.md § Graph Labels Sidebar, rule 12). The
  // sidebar starts expanded on every page load; collapsing only changes the
  // sidebar's own visibility and the canvas width, never the highlight, layout,
  // search, or detail panel.
  var labelsSidebar = document.getElementById("labels-sidebar");
  var labelsToggle = document.getElementById("labels-toggle");
  var labelsToggleIcon = document.getElementById("labels-toggle-icon");

  // Highlight selection state. Two Sets hold the currently active node labels
  // and edge types; the highlighted set is the union of both (rule 5). They are
  // module-level so the selection survives a layout re-render (rule 8): the new
  // SVG is re-dimmed from this same state after every render.
  var activeLabels = Object.create(null);
  var activeTypes = Object.create(null);
  // Marker attributes carried by every rendered node/edge group so the
  // layout-agnostic applyHighlight()/applyEmphasis() can decide dim vs.
  // highlight without knowing the datum shape of each layout.
  var ATTR_NODE_LABELS = "data-node-labels";
  var ATTR_EDGE_TYPE = "data-edge-type";
  // Identity markers used by neighbor focus (SPEC/WEB.md § Roadmap
  // Knowledge-Graph Page, "Neighbor focus on node selection"). Every rendered
  // node group carries its node id; every rendered edge group carries its two
  // endpoint ids, so applyEmphasis() can dim by the focused node's first-degree
  // (undirected) neighbourhood by reading the DOM alone, the same dim-not-remove
  // mechanism the labels highlight uses.
  var ATTR_NODE_ID = "data-node-id";
  var ATTR_EDGE_SOURCE = "data-edge-source";
  var ATTR_EDGE_TARGET = "data-edge-target";

  // focusedNodeId is the single module-level neighbor-focus state. When non-null
  // a node is focused: applyEmphasis() governs canvas dimming from its
  // first-degree neighbourhood and the labels-sidebar highlight is NOT applied
  // (focus takes precedence; SPEC/WEB.md § Graph Labels Sidebar, rule 8). When
  // null no node is focused and applyEmphasis() delegates to applyHighlight()
  // (the labels state) — or the normal non-dimmed view when nothing is active.
  // It survives a layout re-render so render() can reapply the same focus.
  var focusedNodeId = null;
  // Separator used to pack a node's label list into a single attribute value.
  // The unit separator (U+001F) cannot occur in a label, so a packed value can
  // be split back unambiguously.
  var LABELS_SEP = String.fromCharCode(31);

  // Tabler dark-theme palette (matches the prior styling intent): light node
  // captions, blue node fill, muted slate edges, amber selection accent.
  var COLOR_NODE = "#4299e1";
  var COLOR_CAPTION = "#c8d3e0";
  var COLOR_EDGE = "#56627a";
  var COLOR_EDGE_LABEL = "#8a97ab";
  var COLOR_ACCENT = "#f59f00";

  // graphModel holds the in-memory model built once from the data endpoint and
  // re-rendered on every dropdown change. nodes/links are rebuilt per render so
  // a layout that mutates them (the force simulation sets x/y; sankey rewrites
  // source/target into objects) never corrupts the next layout.
  var graphModel = null;

  function showEmpty() {
    if (emptyEl) {
      emptyEl.hidden = false;
    }
  }

  function hideEmpty() {
    if (emptyEl) {
      emptyEl.hidden = true;
    }
  }

  function hidePanel() {
    if (panelEl) {
      panelEl.hidden = true;
    }
  }

  // ---- Labels sidebar: inventory, rendering, and highlight -----------------

  // hasActiveSelection reports whether any node-label or edge-type entry is
  // currently toggled on. When nothing is active the canvas shows its normal,
  // non-dimmed view (SPEC/WEB.md § Graph Labels Sidebar, rule 6).
  function hasActiveSelection() {
    return Object.keys(activeLabels).length > 0 || Object.keys(activeTypes).length > 0;
  }

  // tagNodes / tagEdges stamp every rendered node/edge group with a marker
  // attribute carrying its labels (packed) or its type, so applyHighlight() can
  // decide dim vs. highlight by reading the DOM alone — independent of the
  // datum shape each layout binds. labelsOf/typeOf extract those from the bound
  // datum.
  function tagNodes(selection, labelsOf, idOf) {
    selection.attr(ATTR_NODE_LABELS, function (d) {
      var labels = labelsOf(d) || [];
      return labels.join(LABELS_SEP);
    });
    // Stamp the node id too, so neighbor focus can dim by neighbourhood from the
    // DOM. idOf is supplied by every layout because each binds a different datum
    // shape (force/arc/sankey/patents: n.id; bundling: leaf.data.name; chord:
    // model.nodes[g.index].id); a missing accessor leaves the attribute empty.
    selection.attr(ATTR_NODE_ID, function (d) {
      return idOf ? String(idOf(d)) : "";
    });
  }

  function tagEdges(selection, typeOf, endpointsOf) {
    selection.attr(ATTR_EDGE_TYPE, function (d) {
      return typeOf(d) || "";
    });
    // Stamp the edge endpoints so neighbor focus can decide whether an edge is
    // incident to the focused node. endpointsOf returns {source, target} as node
    // ids; a missing accessor leaves both empty and the edge is treated as not
    // incident to any node (it dims under focus).
    selection.attr(ATTR_EDGE_SOURCE, function (d) {
      var e = endpointsOf ? endpointsOf(d) : null;
      return e ? String(e.source) : "";
    });
    selection.attr(ATTR_EDGE_TARGET, function (d) {
      var e = endpointsOf ? endpointsOf(d) : null;
      return e ? String(e.target) : "";
    });
  }

  // linkEndpoints resolves a force/arc/sankey/patents link datum's endpoint ids
  // regardless of whether the layout has rewritten source/target into node
  // objects (the force simulation and sankey do) or left them as string ids.
  function linkEndpoints(l) {
    var s = (l.source && typeof l.source === "object") ? l.source.id : l.source;
    var t = (l.target && typeof l.target === "object") ? l.target.id : l.target;
    return { source: s, target: t };
  }

  // neighborSet computes the first-degree, UNDIRECTED neighbourhood of the
  // focused node from the in-memory model (SPEC/WEB.md § Roadmap Knowledge-Graph
  // Page, "Neighbor focus on node selection"; Acceptance Criterion 54). It walks
  // the model's links by their startId/endId (mapped to source/target in
  // buildModel) and includes every node reachable by one edge in EITHER
  // direction — the target of an outgoing edge or the source of an incoming
  // edge. The returned set always contains the focused node itself. Computed
  // entirely client-side from already-fetched data; no request, no write.
  function neighborSet(nodeId) {
    var set = Object.create(null);
    set[nodeId] = true;
    if (!graphModel) {
      return set;
    }
    graphModel.links.forEach(function (l) {
      var ep = linkEndpoints(l);
      var s = String(ep.source);
      var t = String(ep.target);
      if (s === nodeId) {
        set[t] = true;
      } else if (t === nodeId) {
        set[s] = true;
      }
    });
    return set;
  }

  // applyHighlight re-scans the rendered SVG and toggles the .is-dimmed class on
  // every tagged node and edge element according to the current selection. It is
  // called after every render so a layout change re-applies the same highlight
  // (rule 8). With no active selection every element is un-dimmed (rule 6).
  function applyHighlight() {
    var svg = graphEl.querySelector("svg.graph-svg");
    if (!svg) {
      return;
    }
    var active = hasActiveSelection();

    var nodes = svg.querySelectorAll("[" + ATTR_NODE_LABELS + "]");
    var i;
    for (i = 0; i < nodes.length; i++) {
      var packed = nodes[i].getAttribute(ATTR_NODE_LABELS);
      var matched = false;
      if (active && packed !== "") {
        var labels = packed.split(LABELS_SEP);
        for (var j = 0; j < labels.length; j++) {
          if (activeLabels[labels[j]]) {
            matched = true;
            break;
          }
        }
      }
      // A node matches when it carries any active label. When a selection is
      // active and the node matches none of it, the node is dimmed.
      setDimmed(nodes[i], active && !matched);
    }

    var edges = svg.querySelectorAll("[" + ATTR_EDGE_TYPE + "]");
    for (i = 0; i < edges.length; i++) {
      var etype = edges[i].getAttribute(ATTR_EDGE_TYPE);
      var ematched = active && etype !== "" && !!activeTypes[etype];
      setDimmed(edges[i], active && !ematched);
    }
  }

  function setDimmed(el, dimmed) {
    if (dimmed) {
      el.classList.add("is-dimmed");
    } else {
      el.classList.remove("is-dimmed");
    }
  }

  // applyFocusDimming dims the canvas by the focused node's first-degree,
  // undirected neighbourhood: the focused node and its neighbours stay
  // emphasised, every other node is dimmed; an edge stays emphasised only when
  // it is incident to the focused node (one of its endpoints is the focused
  // node), every other edge is dimmed. It reuses the same .is-dimmed
  // dim-not-remove mechanism the labels highlight uses, so no element is added
  // or removed (SPEC/WEB.md § Roadmap Knowledge-Graph Page, "Neighbor focus on
  // node selection"; Acceptance Criterion 54).
  function applyFocusDimming(svg) {
    var keep = neighborSet(focusedNodeId);

    var nodes = svg.querySelectorAll("[" + ATTR_NODE_ID + "]");
    var i;
    for (i = 0; i < nodes.length; i++) {
      var id = nodes[i].getAttribute(ATTR_NODE_ID);
      // A node with no id tag (should not happen for a focused render) dims so
      // the emphasis stays restricted to the resolvable neighbourhood.
      setDimmed(nodes[i], !(id !== "" && keep[id]));
    }

    var edges = svg.querySelectorAll("[" + ATTR_EDGE_SOURCE + "]");
    for (i = 0; i < edges.length; i++) {
      var s = edges[i].getAttribute(ATTR_EDGE_SOURCE);
      var t = edges[i].getAttribute(ATTR_EDGE_TARGET);
      var incident = (s === focusedNodeId) || (t === focusedNodeId);
      setDimmed(edges[i], !incident);
    }
  }

  // applyEmphasis is the SINGLE source of truth for canvas dimming. Neighbor
  // focus takes precedence over the labels-sidebar highlight (SPEC/WEB.md
  // § Graph Labels Sidebar, rule 8; Acceptance Criterion 56): when a node is
  // focused it governs the dimming and the labels selection is NOT applied to
  // the canvas; otherwise it delegates to applyHighlight() (the labels state),
  // which itself shows the normal non-dimmed view when no entry is active. Every
  // render-time re-application and every focus/background/panel handler routes
  // through here, so there is exactly one dimming decision path.
  function applyEmphasis() {
    var svg = graphEl.querySelector("svg.graph-svg");
    if (!svg) {
      return;
    }
    if (focusedNodeId !== null) {
      applyFocusDimming(svg);
    } else {
      applyHighlight();
    }
  }

  // focusNode enters (or moves) neighbor focus onto a node and reapplies the
  // emphasis. Selecting a different node while one is focused moves the focus to
  // the new node without an intervening clear (Acceptance Criterion 55).
  function focusNode(nodeId) {
    focusedNodeId = String(nodeId);
    applyEmphasis();
  }

  // clearFocus leaves neighbor focus and restores the prior view: when the focus
  // is cleared the canvas returns to the labels-sidebar highlight state if any
  // entry is still active, otherwise to the normal non-dimmed view — both handled
  // by applyEmphasis() delegating to applyHighlight() once focusedNodeId is null
  // (Acceptance Criterion 55). It does not itself close the panel; callers that
  // implement a clear gesture pair it with hidePanel().
  function clearFocus() {
    if (focusedNodeId === null) {
      return;
    }
    focusedNodeId = null;
    applyEmphasis();
  }

  // onNodeSelected is the shared node-click handler every layout wires. It opens
  // the node's detail panel and drives neighbor focus through ONE consistent
  // rule: re-selecting the already-focused node is a clear gesture (close panel +
  // clear focus together); selecting any other node moves the focus to it and
  // opens its detail. This keeps the detail panel and the neighbor-focus emphasis
  // opened and cleared together (SPEC/WEB.md § Roadmap Knowledge-Graph Page,
  // "Clearing neighbor focus"; Acceptance Criterion 55).
  function onNodeSelected(node) {
    var id = String(node.id);
    if (focusedNodeId === id) {
      hidePanel();
      clearFocus();
      return;
    }
    showDetail(nodeTitle(node), node.props);
    focusNode(id);
  }

  // dismissSelection is the shared clear gesture for an empty-canvas tap and for
  // closing the detail panel: it closes the panel and clears the neighbor focus
  // together (Acceptance Criterion 55).
  function dismissSelection() {
    hidePanel();
    clearFocus();
  }

  // buildInventory derives the node-label and edge-type inventories with counts
  // from the in-memory model (rule 1, rule 10), plus the two section totals
  // (rule 11). A node counts towards each of its labels; a node with no label
  // contributes to no per-label entry but still counts towards nodeTotal.
  // Returns the two sorted entry arrays and the two section totals:
  //   - nodeTotal is the count of DISTINCT nodes in the model (the model's
  //     nodes are already deduplicated by id when built; see buildModel and the
  //     endpoint's per-id dedup, Acceptance Criterion 49). It is the
  //     distinct-node count, NOT the sum of per-label counts: a multi-label
  //     node inflates the per-label sum but counts once here, and an unlabelled
  //     node counts here while appearing in no per-label entry.
  //   - typeTotal is the count of edges. Every edge has exactly one type, so it
  //     equals the sum of the per-type counts.
  // Each entry array is sorted by name in ascending, case-sensitive code-point
  // order (rule 2).
  function buildInventory(model) {
    var labelCounts = Object.create(null);
    model.nodes.forEach(function (n) {
      (n.labels || []).forEach(function (label) {
        labelCounts[label] = (labelCounts[label] || 0) + 1;
      });
    });

    var typeCounts = Object.create(null);
    model.links.forEach(function (l) {
      var t = l.etype || "";
      if (t === "") {
        return;
      }
      typeCounts[t] = (typeCounts[t] || 0) + 1;
    });

    return {
      labels: toSortedEntries(labelCounts),
      types: toSortedEntries(typeCounts),
      nodeTotal: model.nodes.length,
      typeTotal: model.links.length
    };
  }

  // toSortedEntries turns a {name: count} map into an array of {name, count}
  // sorted by name in ascending code-point order, deterministic per rule 2.
  function toSortedEntries(counts) {
    return Object.keys(counts)
      .sort(function (a, b) {
        return a < b ? -1 : (a > b ? 1 : 0);
      })
      .map(function (name) {
        return { name: name, count: counts[name] };
      });
  }

  // renderSidebar builds the two sidebar sections from the inventory. Each entry
  // is a toggle button (touch-friendly, rule 9) that flips its selection and
  // re-applies the highlight. An empty section shows a muted empty-state (rule 3).
  function renderSidebar(model) {
    var inv = buildInventory(model);
    // Section totals (rule 11): the distinct-node total and the edge total are
    // shown alongside the headers, kept distinct from the per-entry sums. In an
    // empty graph both totals render as 0 (rule 3).
    if (nodeLabelsTotal) {
      nodeLabelsTotal.textContent = String(inv.nodeTotal);
    }
    if (edgeTypesTotal) {
      edgeTypesTotal.textContent = String(inv.typeTotal);
    }
    fillSection(nodeLabelsList, nodeLabelsEmpty, inv.labels, activeLabels);
    fillSection(edgeTypesList, edgeTypesEmpty, inv.types, activeTypes);
  }

  function fillSection(listEl, emptyEl2, entries, activeSet) {
    if (!listEl) {
      return;
    }
    listEl.innerHTML = "";
    if (entries.length === 0) {
      if (emptyEl2) {
        emptyEl2.hidden = false;
      }
      return;
    }
    if (emptyEl2) {
      emptyEl2.hidden = true;
    }
    entries.forEach(function (entry) {
      var li = document.createElement("li");
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "labels-sidebar__item";
      if (activeSet[entry.name]) {
        btn.classList.add("is-active");
      }
      btn.setAttribute("aria-pressed", activeSet[entry.name] ? "true" : "false");

      var nameSpan = document.createElement("span");
      nameSpan.className = "labels-sidebar__name";
      // textContent never interprets the value as HTML, so a label or type that
      // contains markup characters cannot alter the page structure.
      nameSpan.textContent = entry.name;

      var countSpan = document.createElement("span");
      countSpan.className = "labels-sidebar__count badge bg-blue-lt";
      countSpan.textContent = String(entry.count);

      btn.appendChild(nameSpan);
      btn.appendChild(countSpan);

      btn.addEventListener("click", function () {
        if (activeSet[entry.name]) {
          delete activeSet[entry.name];
          btn.classList.remove("is-active");
          btn.setAttribute("aria-pressed", "false");
        } else {
          activeSet[entry.name] = true;
          btn.classList.add("is-active");
          btn.setAttribute("aria-pressed", "true");
        }
        applyHighlight();
      });

      li.appendChild(btn);
      listEl.appendChild(li);
    });
  }

  // Render a label/value pair into the detail panel.
  function addRow(label, value) {
    var dt = document.createElement("dt");
    dt.textContent = label;
    var dd = document.createElement("dd");
    // A property value may be multi-line free-text the user authored through the
    // CLI; this class preserves its source line breaks rather than letting HTML
    // collapse them (SPEC/WEB.md § Frontend Rules, rule 6). Value is assigned via
    // textContent, so it is never interpreted as HTML.
    dd.className = "detail-panel__value";
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
    // Closing the detail panel is a clear gesture: it closes the panel and
    // clears the neighbor focus together (SPEC/WEB.md § Roadmap Knowledge-Graph
    // Page, "Clearing neighbor focus"; Acceptance Criterion 55).
    panelClose.addEventListener("click", dismissSelection);
  }

  // buildModel derives the D3 model from the data endpoint's JSON. nodes carry
  // a caption (props.key||name||path||first label||"node"); links keep only
  // those whose both endpoints exist in the node set (preserving the prior
  // buildElements filtering logic).
  function buildModel(data) {
    var nodes = [];
    var byId = Object.create(null);

    (data.nodes || []).forEach(function (n) {
      var id = String(n.id);
      var labels = n.labels || [];
      var props = n.properties || {};
      var caption = props.key || props.name || props.path || (labels[0] || "node");
      var node = {
        id: id,
        caption: String(caption),
        labels: labels,
        props: props
      };
      byId[id] = node;
      nodes.push(node);
    });

    var links = [];
    (data.edges || []).forEach(function (e) {
      var s = String(e.startId);
      var t = String(e.endId);
      if (!byId[s] || !byId[t]) {
        return;
      }
      links.push({
        id: "e" + String(e.id),
        source: s,
        target: t,
        etype: e.type || "",
        props: e.properties || {}
      });
    });

    return { nodes: nodes, links: links };
  }

  // freshModel returns a deep-enough copy of the in-memory model so each render
  // starts from pristine source/target ids and unset x/y, regardless of how the
  // previous layout mutated its own working copy.
  function freshModel() {
    return {
      nodes: graphModel.nodes.map(function (n) {
        return { id: n.id, caption: n.caption, labels: n.labels, props: n.props };
      }),
      links: graphModel.links.map(function (l) {
        return { id: l.id, source: l.source, target: l.target, etype: l.etype, props: l.props };
      })
    };
  }

  function nodeTitle(d) {
    var label = (d.labels && d.labels.length) ? d.labels.join(", ") : "Node";
    return label + " #" + d.id;
  }

  // clearGraph removes any rendered SVG and any in-place degradation message,
  // so each render starts from a blank container.
  function clearGraph() {
    d3.select(graphEl).selectAll("svg").remove();
    d3.select(graphEl).selectAll(".graph-message").remove();
  }

  // showMessage renders a clear, read-only in-place message inside the graph
  // area (used for graceful degradation). It triggers no write and no
  // navigation (SPEC/WEB.md § Knowledge-Graph Visualisation Library, rule 5).
  function showMessage(text) {
    clearGraph();
    hidePanel();
    var wrap = d3.select(graphEl)
      .append("div")
      .attr("class", "graph-message");
    wrap.append("p")
      .attr("class", "graph-message__text text-secondary")
      .text(text);
  }

  // dims returns the current container dimensions, with sane fallbacks so the
  // first render before layout settles still produces a usable viewBox.
  function dims() {
    var w = graphEl.clientWidth || 800;
    var h = graphEl.clientHeight || 600;
    return { width: w, height: h };
  }

  // newSvg appends a sized SVG plus a root <g> that carries pan/zoom, wires
  // d3.zoom (touch- and wheel-friendly), and dismisses the detail panel when
  // empty background is tapped. It returns { svg, root }.
  function newSvg(width, height) {
    var svg = d3.select(graphEl)
      .append("svg")
      .attr("class", "graph-svg")
      .attr("width", "100%")
      .attr("height", "100%")
      .attr("viewBox", [0, 0, width, height])
      .attr("preserveAspectRatio", "xMidYMid meet");

    var root = svg.append("g");

    var zoom = d3.zoom()
      .scaleExtent([0.1, 8])
      .on("zoom", function (event) {
        root.attr("transform", event.transform);
      });
    svg.call(zoom);

    // Tapping empty background (the SVG itself, not a node/edge) is a clear
    // gesture: it dismisses the detail panel and clears the neighbor focus
    // together (SPEC/WEB.md § Roadmap Knowledge-Graph Page, "Clearing neighbor
    // focus"; Acceptance Criterion 55).
    svg.on("click", function (event) {
      if (event.target === svg.node()) {
        dismissSelection();
      }
    });

    return { svg: svg, root: root };
  }

  // arrowMarker defines a directed-edge arrowhead in <defs> and returns the
  // marker url. Each layout that draws directed edges references it.
  function arrowMarker(svg, color) {
    var id = "arrow-" + color.replace("#", "");
    svg.append("defs")
      .append("marker")
      .attr("id", id)
      .attr("viewBox", "0 -5 10 10")
      .attr("refX", 18)
      .attr("refY", 0)
      .attr("markerWidth", 6)
      .attr("markerHeight", 6)
      .attr("orient", "auto")
      .append("path")
      .attr("d", "M0,-5L10,0L0,5")
      .attr("fill", color);
    return "url(#" + id + ")";
  }

  // ---- Force-directed and Disjoint force-directed --------------------------

  // renderForce draws a force-directed (or, when disjoint=true, a disjoint
  // force-directed) layout: forceLink by id, forceManyBody charge, and either
  // forceCenter (connected) or forceX/forceY (disjoint, so components stay
  // on-screen, per the D3 "Disjoint force-directed graph" example). Nodes are
  // draggable; the whole graph pans/zooms.
  function renderForce(disjoint) {
    var d = dims();
    var model = freshModel();
    var made = newSvg(d.width, d.height);
    var svg = made.svg;
    var root = made.root;
    var marker = arrowMarker(svg, COLOR_EDGE);

    var link = root.append("g")
      .attr("stroke", COLOR_EDGE)
      .attr("stroke-width", 1.5)
      .selectAll("line")
      .data(model.links)
      .join("line")
      .attr("marker-end", marker)
      .style("cursor", "pointer")
      .on("click", function (event, l) {
        event.stopPropagation();
        showDetail(l.etype || "Edge", l.props);
      });
    tagEdges(link, function (l) { return l.etype; }, linkEndpoints);

    var node = root.append("g")
      .selectAll("g")
      .data(model.nodes)
      .join("g")
      .style("cursor", "pointer")
      .on("click", function (event, n) {
        event.stopPropagation();
        onNodeSelected(n);
      });
    tagNodes(node, function (n) { return n.labels; }, function (n) { return n.id; });

    node.append("circle")
      .attr("r", 8)
      .attr("fill", COLOR_NODE);

    node.append("text")
      .text(function (n) { return n.caption; })
      .attr("x", 11)
      .attr("y", 4)
      .attr("font-size", "10px")
      .attr("fill", COLOR_CAPTION);

    var sim = d3.forceSimulation(model.nodes)
      .force("link", d3.forceLink(model.links).id(function (n) { return n.id; }).distance(80))
      .force("charge", d3.forceManyBody().strength(-220));

    if (disjoint) {
      // Replace the centring force with positioning forces so disconnected
      // components are each pulled toward the centre and stay on-screen.
      sim.force("x", d3.forceX(d.width / 2).strength(0.06))
        .force("y", d3.forceY(d.height / 2).strength(0.06));
    } else {
      sim.force("center", d3.forceCenter(d.width / 2, d.height / 2));
    }

    sim.on("tick", function () {
      link
        .attr("x1", function (l) { return l.source.x; })
        .attr("y1", function (l) { return l.source.y; })
        .attr("x2", function (l) { return l.target.x; })
        .attr("y2", function (l) { return l.target.y; });
      node.attr("transform", function (n) { return "translate(" + n.x + "," + n.y + ")"; });
    });

    var drag = d3.drag()
      .on("start", function (event, n) {
        if (!event.active) { sim.alphaTarget(0.3).restart(); }
        n.fx = n.x;
        n.fy = n.y;
      })
      .on("drag", function (event, n) {
        n.fx = event.x;
        n.fy = event.y;
      })
      .on("end", function (event, n) {
        if (!event.active) { sim.alphaTarget(0); }
        n.fx = null;
        n.fy = null;
      });
    node.call(drag);
  }

  // ---- Arc diagram ---------------------------------------------------------

  // renderArc lays nodes along a horizontal baseline in a stable order (by
  // caption then id) and draws each link as a semicircular arc above the
  // baseline. Node labels are written vertically below each point. Pan/zoom is
  // available through the shared root <g>.
  function renderArc() {
    var d = dims();
    var model = freshModel();
    var made = newSvg(d.width, d.height);
    var svg = made.svg;
    var root = made.root;
    var marker = arrowMarker(svg, COLOR_EDGE);

    var ordered = model.nodes.slice().sort(function (a, b) {
      if (a.caption === b.caption) {
        return a.id < b.id ? -1 : (a.id > b.id ? 1 : 0);
      }
      return a.caption < b.caption ? -1 : 1;
    });

    var margin = 40;
    var baselineY = d.height * 0.7;
    var step = ordered.length > 1
      ? (d.width - 2 * margin) / (ordered.length - 1)
      : 0;
    var posById = Object.create(null);
    ordered.forEach(function (n, i) {
      n.ax = margin + i * step;
      posById[n.id] = n;
    });

    var arcLink = root.append("g")
      .attr("fill", "none")
      .attr("stroke", COLOR_EDGE)
      .attr("stroke-width", 1.5)
      .selectAll("path")
      .data(model.links)
      .join("path")
      .attr("marker-end", marker)
      .attr("d", function (l) {
        var s = posById[String(typeof l.source === "object" ? l.source.id : l.source)];
        var t = posById[String(typeof l.target === "object" ? l.target.id : l.target)];
        if (!s || !t) { return null; }
        var x1 = s.ax;
        var x2 = t.ax;
        var r = Math.abs(x2 - x1) / 2;
        // Arc above the baseline; sweep direction by left-to-right ordering.
        var sweep = x1 < x2 ? 1 : 0;
        return "M" + x1 + "," + baselineY + " A" + r + "," + r + " 0 0," + sweep + " " + x2 + "," + baselineY;
      })
      .style("cursor", "pointer")
      .on("click", function (event, l) {
        event.stopPropagation();
        showDetail(l.etype || "Edge", l.props);
      });
    tagEdges(arcLink, function (l) { return l.etype; }, linkEndpoints);

    var node = root.append("g")
      .selectAll("g")
      .data(ordered)
      .join("g")
      .attr("transform", function (n) { return "translate(" + n.ax + "," + baselineY + ")"; })
      .style("cursor", "pointer")
      .on("click", function (event, n) {
        event.stopPropagation();
        onNodeSelected(n);
      });
    tagNodes(node, function (n) { return n.labels; }, function (n) { return n.id; });

    node.append("circle")
      .attr("r", 6)
      .attr("fill", COLOR_NODE);

    node.append("text")
      .text(function (n) { return n.caption; })
      .attr("transform", "rotate(45)")
      .attr("x", 8)
      .attr("y", 4)
      .attr("font-size", "10px")
      .attr("fill", COLOR_CAPTION);
  }

  // ---- Sankey diagram ------------------------------------------------------

  // renderSankey computes a Sankey layout with d3-sankey (node value default 1)
  // and draws node rects plus horizontal links. d3-sankey throws on a cyclic
  // graph ("circular link"); the computation is wrapped in try/catch and a
  // failure degrades gracefully with an in-place read-only message, leaving the
  // dropdown usable (SPEC/WEB.md § Knowledge-Graph Visualisation Library).
  function renderSankey() {
    if (typeof d3.sankey !== "function") {
      showMessage("The Sankey layout library is unavailable.");
      return;
    }
    var d = dims();
    var model = freshModel();

    if (model.nodes.length === 0 || model.links.length === 0) {
      showMessage(
        "The Sankey layout needs links between nodes; this roadmap's graph has none. Choose another layout."
      );
      return;
    }

    // d3-sankey resolves source/target by node index; map our string ids to
    // indices and give every node a default value of 1.
    var indexById = Object.create(null);
    var nodes = model.nodes.map(function (n, i) {
      indexById[n.id] = i;
      return { id: n.id, caption: n.caption, labels: n.labels, props: n.props };
    });
    var links = model.links.map(function (l) {
      return {
        id: l.id,
        etype: l.etype,
        props: l.props,
        source: indexById[String(l.source)],
        target: indexById[String(l.target)],
        value: 1
      };
    });

    var made = newSvg(d.width, d.height);
    var svg = made.svg;
    var root = made.root;

    var sankey = d3.sankey()
      .nodeWidth(16)
      .nodePadding(12)
      .extent([[8, 8], [d.width - 8, d.height - 8]]);

    var graph;
    try {
      graph = sankey({
        nodes: nodes.map(function (n) { return Object.assign({}, n); }),
        links: links.map(function (l) { return Object.assign({}, l); })
      });
    } catch (err) {
      showMessage(
        "The Sankey layout needs an acyclic graph; this roadmap's graph contains cycles. Choose another layout."
      );
      return;
    }

    var sankeyLink = root.append("g")
      .attr("fill", "none")
      .attr("stroke", COLOR_EDGE)
      .attr("stroke-opacity", 0.5)
      .selectAll("path")
      .data(graph.links)
      .join("path")
      .attr("d", d3.sankeyLinkHorizontal())
      .attr("stroke-width", function (l) { return Math.max(1, l.width); })
      .style("cursor", "pointer")
      .on("click", function (event, l) {
        event.stopPropagation();
        showDetail(l.etype || "Edge", l.props);
      });
    // After the sankey layout runs, each link's source/target are node objects
    // carrying our id; linkEndpoints resolves them.
    tagEdges(sankeyLink, function (l) { return l.etype; }, linkEndpoints);

    var node = root.append("g")
      .selectAll("g")
      .data(graph.nodes)
      .join("g")
      .style("cursor", "pointer")
      .on("click", function (event, n) {
        event.stopPropagation();
        onNodeSelected(n);
      });
    tagNodes(node, function (n) { return n.labels; }, function (n) { return n.id; });

    node.append("rect")
      .attr("x", function (n) { return n.x0; })
      .attr("y", function (n) { return n.y0; })
      .attr("height", function (n) { return Math.max(1, n.y1 - n.y0); })
      .attr("width", function (n) { return n.x1 - n.x0; })
      .attr("fill", COLOR_NODE);

    node.append("text")
      .text(function (n) { return n.caption; })
      .attr("x", function (n) { return n.x0 < d.width / 2 ? n.x1 + 6 : n.x0 - 6; })
      .attr("y", function (n) { return (n.y0 + n.y1) / 2; })
      .attr("dy", "0.35em")
      .attr("text-anchor", function (n) { return n.x0 < d.width / 2 ? "start" : "end"; })
      .attr("font-size", "10px")
      .attr("fill", COLOR_CAPTION);
  }

  // ---- Hierarchical edge bundling ------------------------------------------

  // renderBundling groups nodes under their primary label (root -> label group
  // -> node), lays the leaves out on a radial cluster, and bundles the graph's
  // links between leaves with a radial line + curveBundle. If there is no usable
  // structure (no nodes, or no links to bundle) it degrades gracefully with an
  // in-place message.
  function renderBundling() {
    var d = dims();
    var model = freshModel();

    if (model.nodes.length === 0) {
      showMessage("This roadmap's graph has no nodes to lay out. Choose another layout.");
      return;
    }
    if (model.links.length === 0) {
      showMessage(
        "Hierarchical edge bundling needs links between nodes to bundle; this roadmap's graph has none. Choose another layout."
      );
      return;
    }

    // Build a 3-level hierarchy: a synthetic root, one group per primary label,
    // and the nodes as leaves. The leaf name is the node id so links resolve.
    var groups = Object.create(null);
    model.nodes.forEach(function (n) {
      var group = (n.labels && n.labels.length) ? n.labels[0] : "node";
      if (!groups[group]) {
        groups[group] = [];
      }
      groups[group].push(n);
    });

    var children = Object.keys(groups).sort().map(function (group) {
      return {
        name: group,
        children: groups[group].map(function (n) {
          return { name: n.id, caption: n.caption, labels: n.labels, props: n.props };
        })
      };
    });
    var rootData = { name: "root", children: children };

    var size = Math.min(d.width, d.height);
    var radius = size / 2 - 60;
    if (radius <= 0) {
      showMessage("The graph area is too small to render the bundling layout.");
      return;
    }

    var made = newSvg(d.width, d.height);
    var root = made.root;
    root.attr("transform", "translate(" + (d.width / 2) + "," + (d.height / 2) + ")");

    var hierarchy = d3.hierarchy(rootData);
    var cluster = d3.cluster().size([2 * Math.PI, radius]);
    cluster(hierarchy);

    // Index leaves by their node id so links can find their endpoints.
    var leafById = Object.create(null);
    hierarchy.leaves().forEach(function (leaf) {
      leafById[leaf.data.name] = leaf;
    });

    var bundledLinks = [];
    model.links.forEach(function (l) {
      var s = leafById[String(l.source)];
      var t = leafById[String(l.target)];
      if (s && t) {
        bundledLinks.push({ source: s, target: t, etype: l.etype, props: l.props });
      }
    });

    if (bundledLinks.length === 0) {
      showMessage(
        "Hierarchical edge bundling found no links it could bundle in this roadmap's graph. Choose another layout."
      );
      return;
    }

    var line = d3.lineRadial()
      .curve(d3.curveBundle.beta(0.85))
      .radius(function (n) { return n.y; })
      .angle(function (n) { return n.x; });

    var bundleLink = root.append("g")
      .attr("fill", "none")
      .attr("stroke", COLOR_EDGE)
      .attr("stroke-opacity", 0.6)
      .selectAll("path")
      .data(bundledLinks)
      .join("path")
      .attr("d", function (l) { return line(l.source.path(l.target)); })
      .style("cursor", "pointer")
      .on("click", function (event, l) {
        event.stopPropagation();
        showDetail(l.etype || "Edge", l.props);
      });
    // Bundled-link endpoints are hierarchy leaves whose node id is leaf.data.name.
    tagEdges(bundleLink, function (l) { return l.etype; }, function (l) {
      return { source: l.source.data.name, target: l.target.data.name };
    });

    var leaf = root.append("g")
      .selectAll("g")
      .data(hierarchy.leaves())
      .join("g")
      .attr("transform", function (n) {
        return "rotate(" + (n.x * 180 / Math.PI - 90) + ") translate(" + n.y + ",0)";
      })
      .style("cursor", "pointer")
      .on("click", function (event, n) {
        event.stopPropagation();
        onNodeSelected({ id: n.data.name, labels: n.data.labels, props: n.data.props });
      });
    tagNodes(leaf, function (n) { return n.data.labels; }, function (n) { return n.data.name; });

    leaf.append("circle")
      .attr("r", 4)
      .attr("fill", COLOR_NODE);

    leaf.append("text")
      .attr("dy", "0.31em")
      .attr("x", function (n) { return n.x < Math.PI ? 8 : -8; })
      .attr("text-anchor", function (n) { return n.x < Math.PI ? "start" : "end"; })
      .attr("transform", function (n) { return n.x >= Math.PI ? "rotate(180)" : null; })
      .text(function (n) { return n.data.caption; })
      .attr("font-size", "9px")
      .attr("fill", COLOR_CAPTION);
  }

  // ---- Mobile patent suits -------------------------------------------------

  // categoryColor maps a stable set of category keys (edge types, node labels)
  // to a repeatable palette so the same relationship type always draws in the
  // same colour within a render. d3.schemeCategory10 is part of the vendored
  // bundle; a small built-in fallback keeps the layouts working even if a
  // future bundle trims the scheme.
  function categoryColor(keys) {
    var palette = (d3.schemeCategory10 && d3.schemeCategory10.length)
      ? d3.schemeCategory10
      : ["#4299e1", "#f59f00", "#2fb344", "#d6336c", "#ae3ec9",
         "#1098ad", "#f76707", "#74b816", "#e8590c", "#4263eb"];
    var sorted = keys.slice().sort();
    var byKey = Object.create(null);
    sorted.forEach(function (k, i) {
      byKey[k] = palette[i % palette.length];
    });
    return function (key) {
      return byKey[key] || COLOR_NODE;
    };
  }

  // typedArrowMarker defines one arrowhead marker per colour so a directed link
  // and its head share the relationship type's colour. Markers are de-duplicated
  // by colour within an SVG via a registry keyed on the colour string.
  function typedArrowMarker(svg, registry, color) {
    var key = color.replace("#", "");
    if (registry[key]) {
      return registry[key];
    }
    var id = "arrow-" + key;
    svg.append("defs")
      .append("marker")
      .attr("id", id)
      .attr("viewBox", "0 -5 10 10")
      .attr("refX", 18)
      .attr("refY", 0)
      .attr("markerWidth", 6)
      .attr("markerHeight", 6)
      .attr("orient", "auto")
      .append("path")
      .attr("d", "M0,-5L10,0L0,5")
      .attr("fill", color);
    var url = "url(#" + id + ")";
    registry[key] = url;
    return url;
  }

  // renderPatents draws a DIRECTED force-directed layout in the style of the D3
  // "Mobile patent suits" example: curved directed links carrying an arrowhead,
  // coloured (link and marker alike) by relationship type, with the type labelled
  // along the link; nodes are draggable and captioned. Pan/zoom and tap-to-inspect
  // work as in the other force layouts. Empty graphs degrade gracefully.
  function renderPatents() {
    var d = dims();
    var model = freshModel();

    if (model.nodes.length === 0) {
      showMessage("This roadmap's graph has no nodes to lay out. Choose another layout.");
      return;
    }

    var types = [];
    var seenType = Object.create(null);
    model.links.forEach(function (l) {
      var t = l.etype || "RELATED";
      if (!seenType[t]) {
        seenType[t] = true;
        types.push(t);
      }
    });
    var colorOf = categoryColor(types);

    var made = newSvg(d.width, d.height);
    var svg = made.svg;
    var root = made.root;
    var markers = Object.create(null);

    // One curved path per link, coloured and arrow-headed by its type.
    var link = root.append("g")
      .attr("fill", "none")
      .attr("stroke-width", 1.5)
      .selectAll("path")
      .data(model.links)
      .join("path")
      .attr("stroke", function (l) { return colorOf(l.etype || "RELATED"); })
      .attr("marker-end", function (l) {
        return typedArrowMarker(svg, markers, colorOf(l.etype || "RELATED"));
      })
      .style("cursor", "pointer")
      .on("click", function (event, l) {
        event.stopPropagation();
        showDetail(l.etype || "Edge", l.props);
      });
    tagEdges(link, function (l) { return l.etype; }, linkEndpoints);

    // Per-link type label, drawn on a hidden mid-link reference path so the text
    // sits beside the curve. The label colour matches the link.
    var linkLabel = root.append("g")
      .selectAll("text")
      .data(model.links)
      .join("text")
      .attr("font-size", "9px")
      .attr("fill", function (l) { return colorOf(l.etype || "RELATED"); })
      .attr("text-anchor", "middle")
      .style("pointer-events", "none")
      .text(function (l) { return l.etype || ""; });

    var node = root.append("g")
      .selectAll("g")
      .data(model.nodes)
      .join("g")
      .style("cursor", "pointer")
      .on("click", function (event, n) {
        event.stopPropagation();
        onNodeSelected(n);
      });
    tagNodes(node, function (n) { return n.labels; }, function (n) { return n.id; });

    node.append("circle")
      .attr("r", 7)
      .attr("fill", COLOR_NODE);

    node.append("text")
      .text(function (n) { return n.caption; })
      .attr("x", 11)
      .attr("y", 4)
      .attr("font-size", "10px")
      .attr("fill", COLOR_CAPTION);

    var sim = d3.forceSimulation(model.nodes)
      .force("link", d3.forceLink(model.links).id(function (n) { return n.id; }).distance(110))
      .force("charge", d3.forceManyBody().strength(-300))
      .force("x", d3.forceX(d.width / 2).strength(0.05))
      .force("y", d3.forceY(d.height / 2).strength(0.05));

    // linkArc draws a quadratic curve between endpoints (the "patent suits"
    // curve), the curvature derived from the chord length so parallel edges
    // between the same pair stay distinguishable.
    function linkArc(l) {
      var sx = l.source.x, sy = l.source.y, tx = l.target.x, ty = l.target.y;
      var dx = tx - sx, dy = ty - sy;
      var dr = Math.sqrt(dx * dx + dy * dy) * 1.4;
      return "M" + sx + "," + sy + "A" + dr + "," + dr + " 0 0,1 " + tx + "," + ty;
    }

    sim.on("tick", function () {
      link.attr("d", linkArc);
      linkLabel
        .attr("x", function (l) { return (l.source.x + l.target.x) / 2; })
        .attr("y", function (l) { return (l.source.y + l.target.y) / 2; });
      node.attr("transform", function (n) { return "translate(" + n.x + "," + n.y + ")"; });
    });

    var drag = d3.drag()
      .on("start", function (event, n) {
        if (!event.active) { sim.alphaTarget(0.3).restart(); }
        n.fx = n.x;
        n.fy = n.y;
      })
      .on("drag", function (event, n) {
        n.fx = event.x;
        n.fy = event.y;
      })
      .on("end", function (event, n) {
        if (!event.active) { sim.alphaTarget(0); }
        n.fx = null;
        n.fy = null;
      });
    node.call(drag);
  }

  // ---- Chord diagram variants ----------------------------------------------

  // CHORD_MAX_NODES caps the matrix size for the chord layouts. A chord diagram
  // builds an N×N matrix and is unreadable past a few dozen groups; beyond the
  // cap the layout degrades gracefully with a message that states the cap (it
  // does not silently truncate) (SPEC/WEB.md § Knowledge-Graph Visualisation
  // Library, rule 5).
  var CHORD_MAX_NODES = 60;

  // renderChord draws one of the three chord variants from an adjacency matrix
  // over the nodes:
  //   directed=false                -> Chord diagram (undirected matrix, plain ribbons)
  //   directed=true, bySource=false -> Directed chord diagram (directed matrix, arrow ribbons)
  //   directed=true, bySource=true  -> Chord dependency diagram (as directed, ribbons
  //                                    coloured by their SOURCE group)
  // Group arcs are coloured per node/label and captioned around the rim; tapping
  // a group opens that node's detail, tapping a ribbon opens the relationship
  // detail when it resolves to a single edge.
  function renderChord(directed, bySource) {
    var d = dims();
    var model = freshModel();

    if (model.nodes.length === 0) {
      showMessage("This roadmap's graph has no nodes to lay out. Choose another layout.");
      return;
    }
    if (model.links.length === 0) {
      showMessage(
        "A chord diagram needs links between nodes to draw chords; this roadmap's graph has none. Choose another layout."
      );
      return;
    }
    if (model.nodes.length > CHORD_MAX_NODES) {
      showMessage(
        "A chord diagram is only legible for up to " + CHORD_MAX_NODES +
        " nodes; this graph has " + model.nodes.length +
        ". Choose another layout to see the whole graph."
      );
      return;
    }

    var n = model.nodes.length;
    var indexById = Object.create(null);
    model.nodes.forEach(function (node, i) { indexById[node.id] = i; });

    // matrix[i][j] counts links from i to j. Undirected variant increments both
    // directions; directed variants increment only source->target. edgeAt holds
    // one representative edge per (i,j) cell so a tapped ribbon can resolve to a
    // relationship's detail (when the cell carries a single edge).
    var matrix = [];
    var edgeAt = [];
    var k;
    for (k = 0; k < n; k++) {
      matrix.push(new Array(n).fill(0));
      edgeAt.push(new Array(n).fill(null));
    }

    var anyLink = false;
    model.links.forEach(function (l) {
      var s = indexById[String(l.source)];
      var t = indexById[String(l.target)];
      if (s === undefined || t === undefined) {
        return;
      }
      anyLink = true;
      matrix[s][t] += 1;
      edgeAt[s][t] = edgeAt[s][t] || l;
      if (!directed) {
        matrix[t][s] += 1;
        edgeAt[t][s] = edgeAt[t][s] || l;
      }
    });

    if (!anyLink) {
      showMessage(
        "A chord diagram needs links whose endpoints are both in the graph; none resolved here. Choose another layout."
      );
      return;
    }

    var size = Math.min(d.width, d.height);
    var outerRadius = size / 2 - 90;
    var innerRadius = outerRadius - 16;
    if (innerRadius <= 0) {
      showMessage("The graph area is too small to render the chord layout.");
      return;
    }

    // d3.chordDirected ships with the vendored bundle; when present it lays out a
    // directed matrix so each ribbon's arrow encodes source->target. The fallback
    // (d3.chord with sortSubgroups) still draws an asymmetric matrix correctly.
    var chordLayout;
    if (directed && typeof d3.chordDirected === "function") {
      chordLayout = d3.chordDirected()
        .padAngle(0.04)
        .sortSubgroups(d3.descending)
        .sortChords(d3.descending);
    } else {
      chordLayout = d3.chord()
        .padAngle(0.04)
        .sortSubgroups(d3.descending);
    }
    var chords = chordLayout(matrix);

    var made = newSvg(d.width, d.height);
    var root = made.root;
    root.attr("transform", "translate(" + (d.width / 2) + "," + (d.height / 2) + ")");

    var colorOf = categoryColor(model.nodes.map(function (node) { return node.id; }));
    function groupColor(i) { return colorOf(model.nodes[i].id); }

    var arc = d3.arc().innerRadius(innerRadius).outerRadius(outerRadius);
    // ribbonArrow draws the directional ribbon (arrow at the target); plain
    // ribbon for the undirected diagram. Both are in the vendored d3-chord.
    var ribbon = (directed && typeof d3.ribbonArrow === "function")
      ? d3.ribbonArrow().radius(innerRadius)
      : d3.ribbon().radius(innerRadius);

    // Group arcs around the rim, coloured per node, tap -> node detail.
    var group = root.append("g")
      .selectAll("g")
      .data(chords.groups)
      .join("g");
    // Each group <g> stands for one node; tag it with that node's labels and id
    // so the sidebar highlight and neighbor focus dim/undim chord groups like any
    // other node element.
    tagNodes(group,
      function (g) { return model.nodes[g.index].labels; },
      function (g) { return model.nodes[g.index].id; });

    group.append("path")
      .attr("fill", function (g) { return groupColor(g.index); })
      .attr("stroke", "#1a2233")
      .attr("d", arc)
      .style("cursor", "pointer")
      .on("click", function (event, g) {
        event.stopPropagation();
        onNodeSelected(model.nodes[g.index]);
      });

    // Group captions around the rim, rotated to follow the circle.
    group.append("text")
      .each(function (g) { g.angle = (g.startAngle + g.endAngle) / 2; })
      .attr("dy", "0.35em")
      .attr("transform", function (g) {
        return "rotate(" + (g.angle * 180 / Math.PI - 90) + ")" +
          " translate(" + (outerRadius + 6) + ")" +
          (g.angle > Math.PI ? " rotate(180)" : "");
      })
      .attr("text-anchor", function (g) { return g.angle > Math.PI ? "end" : "start"; })
      .attr("font-size", "9px")
      .attr("fill", COLOR_CAPTION)
      .text(function (g) { return model.nodes[g.index].caption; });

    // Ribbons. The dependency variant colours each ribbon by its SOURCE group so
    // dependencies fan out from each node; otherwise ribbons take the target's
    // colour. Tap a ribbon -> the representative edge's detail when resolvable.
    var ribbons = root.append("g")
      .attr("fill-opacity", 0.72)
      .selectAll("path")
      .data(chords)
      .join("path")
      .attr("d", ribbon)
      .attr("fill", function (c) {
        return bySource ? groupColor(c.source.index) : groupColor(c.target.index);
      })
      .attr("stroke", "#1a2233")
      .attr("stroke-opacity", 0.3)
      .style("cursor", "pointer")
      .on("click", function (event, c) {
        event.stopPropagation();
        var edge = edgeAt[c.source.index][c.target.index];
        if (edge) {
          showDetail(edge.etype || "Edge", edge.props);
        } else {
          var from = model.nodes[c.source.index];
          var to = model.nodes[c.target.index];
          showDetail("Relationship", { from: from.caption, to: to.caption });
        }
      });
    // Tag each ribbon with the relationship type of its representative edge so
    // the edge-type highlight dims/undims chord ribbons consistently, and with
    // its source/target group node ids so neighbor focus treats a ribbon touching
    // the focused node's group as incident.
    tagEdges(ribbons,
      function (c) {
        var edge = edgeAt[c.source.index][c.target.index];
        return edge ? edge.etype : "";
      },
      function (c) {
        return {
          source: model.nodes[c.source.index].id,
          target: model.nodes[c.target.index].id
        };
      });
  }

  // COLOR_EDGE_LABEL is referenced by the patents layout's per-link captions; the
  // accent constant is mirrored by the sidebar's active-entry style in CSS.
  void COLOR_ACCENT;
  void COLOR_EDGE_LABEL;

  // render dispatches to the chosen layout renderer. It always clears the SVG
  // and any prior message first, and hides the detail panel so a stale
  // selection does not linger across a layout change.
  function render(layout) {
    clearGraph();
    hidePanel();
    hideEmpty();

    switch (layout) {
      case "disjoint":
        renderForce(true);
        break;
      case "patents":
        renderPatents();
        break;
      case "arc":
        renderArc();
        break;
      case "sankey":
        renderSankey();
        break;
      case "bundling":
        renderBundling();
        break;
      case "chord":
        renderChord(false, false);
        break;
      case "chord-directed":
        renderChord(true, false);
        break;
      case "chord-dependency":
        renderChord(true, true);
        break;
      case "force":
      default:
        renderForce(false);
        break;
    }

    // Re-apply the current emphasis to the freshly rendered SVG so a layout
    // change preserves it (SPEC/WEB.md § Graph Labels Sidebar, rule 8;
    // Acceptance Criterion 56). applyEmphasis() is the single dimming decision
    // path: if a node is focused it reapplies the neighbor focus (same node,
    // neighbours, and incident edges) to the re-rendered layout; otherwise it
    // reapplies the labels-sidebar highlight. Each layout has stamped its
    // node/edge elements with the marker and identity attributes synchronously
    // above, so the tags are in place by now.
    applyEmphasis();
  }

  if (layoutSelect) {
    layoutSelect.addEventListener("change", function () {
      if (graphModel) {
        render(layoutSelect.value);
      }
    });
  }

  // Re-render the current layout on resize so the visualisation stays fitted to
  // the viewport (touch- and small-viewport-friendly). Debounced to avoid
  // thrashing during a continuous resize.
  var resizeTimer = null;
  window.addEventListener("resize", function () {
    if (!graphModel) {
      return;
    }
    if (resizeTimer) {
      clearTimeout(resizeTimer);
    }
    resizeTimer = setTimeout(function () {
      render(layoutSelect ? layoutSelect.value : "patents");
    }, 200);
  });

  // ---- Labels sidebar: collapse / expand -----------------------------------

  // setSidebarCollapsed toggles the sidebar between expanded and collapsed
  // (SPEC/WEB.md § Graph Labels Sidebar, rule 12; Acceptance Criterion 52).
  // Collapsing changes ONLY the sidebar's own visibility and the canvas width:
  // it adds a class the stylesheet uses to contract the column (leaving just the
  // toggle icon visible) and to widen the canvas, swaps the toggle icon between
  // the collapse and expand glyphs, and updates the control's accessible state.
  // It never touches the highlight selection (activeLabels/activeTypes are left
  // intact), never re-renders or re-fetches the graph model, and never opens or
  // closes the detail panel — so an active highlight survives a collapse/expand
  // cycle untouched. Because the graph SVG is sized in CSS percentages of the
  // canvas region (see style.css .graph / .graph-svg), the canvas width change
  // reflows the SVG without a re-render; the debounced window resize handler
  // already keeps the layout fitted, and we trigger one render afterwards so the
  // force layouts re-fit to the new width while preserving the (untouched)
  // highlight via render()'s applyHighlight() call.
  function setSidebarCollapsed(collapsed) {
    if (!labelsSidebar || !labelsToggle) {
      return;
    }
    if (collapsed) {
      labelsSidebar.classList.add("is-collapsed");
    } else {
      labelsSidebar.classList.remove("is-collapsed");
    }
    labelsToggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    labelsToggle.setAttribute("title", collapsed ? "Expand labels" : "Collapse labels");
    labelsToggle.setAttribute("aria-label", collapsed ? "Expand labels" : "Collapse labels");
    if (labelsToggleIcon) {
      labelsToggleIcon.className = collapsed
        ? "ti ti-layout-sidebar-left-expand"
        : "ti ti-layout-sidebar-left-collapse";
    }
    // Re-fit the current layout to the new canvas width. This re-render keeps
    // the active highlight (render() re-applies it) and does not change the
    // layout, run a search, or alter the detail panel state.
    if (graphModel && graphModel.nodes.length > 0) {
      render(layoutSelect ? layoutSelect.value : "patents");
    }
  }

  if (labelsToggle) {
    labelsToggle.addEventListener("click", function () {
      // Toggle on the live DOM state so the control is a single toggle: collapse
      // an expanded sidebar, expand a collapsed one. Default state is expanded
      // (the template renders no is-collapsed class), so the first tap collapses.
      var nowCollapsed = !labelsSidebar.classList.contains("is-collapsed");
      setSidebarCollapsed(nowCollapsed);
    });
  }

  // ---- Query bar: search, fetch, and in-place error surfacing --------------

  // showQueryError displays a clear, read-only, in-place message in the query
  // bar and leaves the graph already shown in place; it triggers no write and no
  // navigation (SPEC/WEB.md § Query-Bar Error Handling, rule 4). The three
  // distinct messages — not read-only, invalid limit, query failed to execute —
  // come straight from the endpoint's classified error so the user knows what to
  // fix (rules 1-3).
  function showQueryError(message) {
    if (!queryError) {
      return;
    }
    queryError.textContent = message;
    queryError.hidden = false;
  }

  function clearQueryError() {
    if (queryError) {
      queryError.hidden = true;
      queryError.textContent = "";
    }
  }

  // buildDataUrl appends the current query box text (q) and the current limit
  // dropdown value (limit) as URL query parameters. The request stays GET-only
  // with no body (SPEC/WEB.md § Graph Query Bar, rule 4; § Graph Data Endpoint).
  function buildDataUrl() {
    var params = [];
    if (queryInput) {
      params.push("q=" + encodeURIComponent(queryInput.value));
    }
    if (limitSelect) {
      params.push("limit=" + encodeURIComponent(limitSelect.value));
    }
    if (params.length === 0) {
      return dataUrl;
    }
    return dataUrl + (dataUrl.indexOf("?") === -1 ? "?" : "&") + params.join("&");
  }

  // applyData renders a freshly fetched graph payload: it rebuilds the in-memory
  // model, recomputes the labels-sidebar inventory and counts from the new
  // result (so the sidebar always reflects the graph currently shown — Graph
  // Query Bar rule 8 and Labels Sidebar rule 10), and re-renders the graph in
  // the currently selected layout (or shows the empty state when there are no
  // nodes).
  function applyData(data) {
    graphModel = buildModel(data);
    // A search fetches a new graph, so any prior neighbor focus is discarded and
    // the new result renders in its labels-sidebar highlight state if any entry
    // is active, otherwise in the normal view (SPEC/WEB.md § Roadmap
    // Knowledge-Graph Page, "Neighbor focus coexists with ... the query bar";
    // Acceptance Criterion 56). Clearing the focus directly (not through
    // dismissSelection) avoids a redundant applyEmphasis() before the re-render,
    // which itself reapplies the emphasis through render().
    focusedNodeId = null;
    renderSidebar(graphModel);
    if (graphModel.nodes.length === 0) {
      hidePanel();
      showEmpty();
      clearGraph();
      return;
    }
    hideEmpty();
    render(layoutSelect ? layoutSelect.value : "patents");
  }

  // runSearch fetches the data endpoint with the current query and limit, then
  // renders the result or surfaces a distinct in-place error. On page load it
  // performs this same fetch once with the default query and default limit
  // (SPEC/WEB.md § Graph Query Bar, rule 4). A failure is non-fatal: the page
  // does not crash and the graph already shown (if any) is left in place.
  function runSearch() {
    clearQueryError();
    fetch(buildDataUrl(), { headers: { Accept: "application/json" } })
      .then(function (resp) {
        // The endpoint returns a structured JSON error with HTTP 400 for a
        // classified query-bar failure (not read-only, invalid limit, or
        // execution failure). Parse the body either way so the distinct message
        // can be shown; only a non-OK, non-400 status is treated as a hard
        // failure.
        return resp.json().then(function (body) {
          return { ok: resp.ok, status: resp.status, body: body };
        });
      })
      .then(function (res) {
        if (res.ok) {
          applyData(res.body);
          return;
        }
        // A classified query-bar error carries { error, kind }. Show the
        // server-provided message in place; the graph already shown stays.
        if (res.body && res.body.error) {
          showQueryError(res.body.error);
          return;
        }
        showQueryError("The graph could not be loaded (status " + res.status + ").");
      })
      .catch(function () {
        showQueryError("The graph could not be loaded. Check the query and try again.");
      });
  }

  if (queryForm) {
    queryForm.addEventListener("submit", function (event) {
      // Keep the request GET-only and stay on the page: prevent the form's
      // default navigation and drive the fetch ourselves (no POST, no reload).
      event.preventDefault();
      runSearch();
    });
  }

  // Keyboard accelerator: Ctrl+Enter (and Cmd+Enter on macOS) in the focused
  // query box triggers the search exactly as the Search button does
  // (SPEC/WEB.md § Graph Query Bar, rule 5; Acceptance Criterion 53). It reuses
  // the existing search path rather than duplicating it: requestSubmit() fires
  // the form's submit event identically to clicking the type="submit" Search
  // button, so the same handler runs the same validation, fetch, and re-render.
  // Plain Enter is left untouched, so it still inserts a newline and the user
  // can compose a multi-line query freely.
  if (queryInput && queryForm) {
    queryInput.addEventListener("keydown", function (event) {
      if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
        event.preventDefault();
        if (typeof queryForm.requestSubmit === "function") {
          queryForm.requestSubmit();
        } else {
          // Fallback for the rare engine without requestSubmit: drive the same
          // search trigger the submit handler uses.
          runSearch();
        }
      }
    });
  }

  // Initial load performs the same fetch with the default query and limit.
  runSearch();
})();
