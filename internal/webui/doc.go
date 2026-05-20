// Package webui hosts an embedded HTTP viewer for the live sim.
//
// The server exposes the focused world as JSON at /api/state and
// streams Step events over Server-Sent Events at /api/events. Static
// assets (HTML, CSS, JS, vendored cytoscape.js bootstrap) are baked
// in via go:embed so the binary stays single-file. Browsers render a
// force-directed graph of entities and relationships by default; a
// toggle swaps to a tree view that mirrors the TUI's Creation Tree.
//
// The server attaches to a *sim.Sim via Server.Publish, which is
// shaped to match sim.Sim.Observer. cmd/fiatlux composes the webui
// publisher with the TUI's existing observer so both consume the
// same stream without coupling to each other.
package webui
