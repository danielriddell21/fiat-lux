// fiat-lux web viewer.
//
// Loads /api/state for the snapshot, /api/events for live updates.
// Renders a force-directed graph by default; a Tree toggle swaps to
// the same view the TUI shows.

(function () {
  "use strict";

  const els = {
    world: document.getElementById("world-name"),
    tick: document.getElementById("tick"),
    entityCount: document.getElementById("entity-count"),
    relCount: document.getElementById("rel-count"),
    agentCount: document.getElementById("agent-count"),
    agents: document.getElementById("agents"),
    thought: document.getElementById("thought"),
    tool: document.getElementById("tool"),
    details: document.getElementById("details"),
    detailsCard: document.getElementById("details-card"),
    cy: document.getElementById("cy"),
    tree: document.getElementById("tree"),
    empty: document.getElementById("empty"),
    eventLog: document.getElementById("event-log"),
    conn: document.getElementById("conn"),
    viewBtns: document.querySelectorAll(".view-btn"),
  };

  let cy = null;
  let currentView = "graph";
  let lastState = null;
  let selectedId = null;
  const eventLogMax = 80;

  // ── Cytoscape ──────────────────────────────────────────────────

  function ensureCy() {
    if (cy) return cy;
    if (typeof cytoscape === "undefined") return null;
    cy = cytoscape({
      container: els.cy,
      style: [
        {
          selector: "node",
          style: {
            "background-color": "data(color)",
            "label": "data(label)",
            "color": "#e6edf3",
            "font-size": 11,
            "text-valign": "bottom",
            "text-margin-y": 6,
            "text-outline-color": "#0d1117",
            "text-outline-width": 2,
            "width": "data(size)",
            "height": "data(size)",
            "border-color": "#30363d",
            "border-width": 1,
          },
        },
        {
          selector: "node.destroyed",
          style: { "opacity": 0.35, "border-style": "dashed" },
        },
        {
          selector: "node.agent",
          style: { "shape": "diamond", "border-color": "#f9b042", "border-width": 2 },
        },
        {
          selector: "node:selected",
          style: { "border-color": "#f9b042", "border-width": 3 },
        },
        {
          selector: "edge",
          style: {
            "width": 1.5,
            "line-color": "#4d5560",
            "target-arrow-color": "#4d5560",
            "target-arrow-shape": "triangle",
            "curve-style": "bezier",
            "label": "data(label)",
            "color": "#8b949e",
            "font-size": 9,
            "text-rotation": "autorotate",
            "text-background-color": "#0d1117",
            "text-background-opacity": 0.9,
            "text-background-padding": 2,
          },
        },
      ],
      layout: { name: "cose", animate: false, idealEdgeLength: 90, nodeRepulsion: 4500 },
    });
    cy.on("tap", "node", (e) => {
      selectedId = e.target.id();
      showDetails();
    });
    cy.on("tap", (e) => {
      if (e.target === cy) {
        selectedId = null;
        showDetails();
      }
    });
    return cy;
  }

  // Hash a string to a stable HSL colour so each entity type gets a
  // consistent tint without an explicit palette.
  function colorForType(type) {
    let h = 0;
    for (let i = 0; i < type.length; i++) h = (h * 31 + type.charCodeAt(i)) | 0;
    const hue = ((h % 360) + 360) % 360;
    return `hsl(${hue}, 65%, 60%)`;
  }

  function renderGraph(state) {
    const c = ensureCy();
    if (!c) return;

    const nodes = state.entities.map((e) => {
      const isAgent = e.type === "agent";
      const name = (e.properties && e.properties.name) || "";
      const label = name ? `${e.type} ${name}` : `${e.type} #${e.id}`;
      const size = isAgent ? 36 : 28;
      return {
        group: "nodes",
        data: { id: String(e.id), label, color: colorForType(e.type), size, type: e.type },
        classes: [isAgent ? "agent" : "", e.destroyed_at ? "destroyed" : ""].filter(Boolean).join(" "),
      };
    });

    const edges = state.relationships.map((r) => ({
      group: "edges",
      data: { id: `r${r.id}`, source: String(r.from), target: String(r.to), label: r.kind },
    }));

    // Diff-style update: re-add nodes/edges, removing anything stale.
    c.batch(() => {
      const wantNodes = new Set(nodes.map((n) => n.data.id));
      const wantEdges = new Set(edges.map((e) => e.data.id));
      c.nodes().forEach((n) => { if (!wantNodes.has(n.id())) n.remove(); });
      c.edges().forEach((e) => { if (!wantEdges.has(e.id())) e.remove(); });
      nodes.forEach((n) => {
        const existing = c.getElementById(n.data.id);
        if (existing.length) {
          existing.data(n.data);
          existing.classes(n.classes);
        } else {
          c.add(n);
        }
      });
      edges.forEach((e) => {
        if (!c.getElementById(e.data.id).length) c.add(e);
      });
    });

    if (nodes.length === 0) {
      els.empty.setAttribute("data-show", "");
    } else {
      els.empty.removeAttribute("data-show");
      c.layout({ name: "cose", animate: false, idealEdgeLength: 90, nodeRepulsion: 4500 }).run();
    }
  }

  // ── Tree view (mirrors the TUI's BuildTreeView output) ────────

  function renderTree(state) {
    const lines = [];
    const writeNode = (node, prefix, isLast, isRoot) => {
      const branch = isRoot ? "" : (isLast ? "└─ " : "├─ ");
      const label = (node.name ? `${node.type} "${node.name}"` : node.type) +
        ` #${node.id}` + (node.destroyed ? "  (destroyed)" : "");
      lines.push(prefix + branch + label);
      const childPrefix = isRoot ? "" : prefix + (isLast ? "   " : "│  ");
      const children = node.children || [];
      children.forEach((child, i) => {
        writeNode(child, childPrefix, i === children.length - 1, false);
      });
    };
    const roots = (state.tree && state.tree.roots) || [];
    if (roots.length === 0) {
      els.tree.textContent = "(void — nothing has been created yet)";
      return;
    }
    roots.forEach((r) => writeNode(r, "", true, true));
    els.tree.textContent = lines.join("\n");
  }

  // ── Side rail ──────────────────────────────────────────────────

  function renderAgents(state) {
    els.agents.innerHTML = "";
    state.agents.forEach((a) => {
      const li = document.createElement("li");
      li.className = a.is_creator ? "creator" : "";
      li.innerHTML = `<span class="name">${escapeHTML(a.name)}</span><span class="id">#${a.id}</span>`;
      els.agents.appendChild(li);
    });
  }

  function renderHeader(state) {
    els.world.textContent = state.world;
    els.tick.textContent = state.tick;
    els.entityCount.textContent = state.entities.length;
    els.relCount.textContent = state.relationships.length;
    els.agentCount.textContent = state.agents.length;
  }

  function renderStep(ev) {
    if (!ev) return;
    if (ev.thought) {
      els.thought.textContent = ev.thought;
      els.thought.classList.remove("faint");
    }
    if (ev.tool_name) {
      const result = ev.tool_err
        ? `<span class="err">error: ${escapeHTML(ev.tool_err)}</span>`
        : escapeHTML(ev.tool_result || "");
      els.tool.innerHTML =
        `<span class="name">${escapeHTML(ev.tool_name)}</span>` +
        `<div class="result">${result}</div>`;
    }
  }

  function showDetails() {
    if (!selectedId || !lastState) {
      els.detailsCard.hidden = true;
      return;
    }
    const e = lastState.entities.find((x) => String(x.id) === selectedId);
    if (!e) {
      els.detailsCard.hidden = true;
      return;
    }
    els.detailsCard.hidden = false;
    const props = e.properties && Object.keys(e.properties).length
      ? `<pre class="props">${escapeHTML(JSON.stringify(e.properties, null, 2))}</pre>`
      : "";
    els.details.innerHTML = `
      <dl>
        <dt>id</dt><dd>${e.id}</dd>
        <dt>type</dt><dd>${escapeHTML(e.type)}</dd>
        <dt>born</dt><dd>tick ${e.created_at}</dd>
      </dl>${props}`;
  }

  function appendEvent(ev) {
    if (!ev) return;
    const li = document.createElement("li");
    const tick = ev.tick != null ? ev.tick : "—";
    const kind = ev.tool_name || (ev.skipped ? "skip" : "tick");
    const detail = ev.tool_result || ev.thought || "";
    li.innerHTML =
      `<span class="tick">t=${tick}</span>` +
      `<span class="kind">${escapeHTML(kind)}</span>` +
      escapeHTML(truncate(detail, 120));
    els.eventLog.appendChild(li);
    while (els.eventLog.children.length > eventLogMax) {
      els.eventLog.removeChild(els.eventLog.firstChild);
    }
    els.eventLog.scrollTop = els.eventLog.scrollHeight;
  }

  // ── view toggle ───────────────────────────────────────────────

  function setView(name) {
    currentView = name;
    els.viewBtns.forEach((b) => b.classList.toggle("active", b.dataset.view === name));
    if (name === "graph") {
      els.cy.setAttribute("data-active", "");
      els.tree.removeAttribute("data-active");
      if (lastState) renderGraph(lastState);
    } else {
      els.tree.setAttribute("data-active", "");
      els.cy.removeAttribute("data-active");
      if (lastState) renderTree(lastState);
    }
  }

  els.viewBtns.forEach((b) => b.addEventListener("click", () => setView(b.dataset.view)));

  // ── networking ────────────────────────────────────────────────

  async function fetchState() {
    try {
      const res = await fetch("/api/state");
      if (!res.ok) throw new Error("HTTP " + res.status);
      const state = await res.json();
      lastState = state;
      renderHeader(state);
      renderAgents(state);
      if (currentView === "graph") renderGraph(state); else renderTree(state);
      showDetails();
    } catch (err) {
      console.error("fetch state:", err);
    }
  }

  function connectEvents() {
    const es = new EventSource("/api/events");
    es.addEventListener("open", () => els.conn.setAttribute("data-connected", ""));
    es.addEventListener("error", () => els.conn.removeAttribute("data-connected"));
    const onEvent = (e) => {
      let ev = null;
      try { ev = JSON.parse(e.data); } catch (_) { return; }
      renderStep(ev);
      appendEvent(ev);
      // Refresh snapshot for graph / tree consistency.
      fetchState();
    };
    es.addEventListener("step", onEvent);
    es.addEventListener("tick", onEvent);
  }

  // ── boot ──────────────────────────────────────────────────────

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }
  function truncate(s, n) {
    s = String(s);
    return s.length > n ? s.slice(0, n - 1) + "…" : s;
  }

  // Initial snapshot, then live updates.
  fetchState().then(connectEvents);
})();
