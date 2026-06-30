package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

var containmentKinds = map[string]struct{}{
	"part of":   {},
	"in":        {},
	"inside":    {},
	"within":    {},
	"contains":  {},
	"child of":  {},
	"member of": {},
}

func isContainment(kind string) bool {
	_, ok := containmentKinds[strings.ToLower(strings.TrimSpace(kind))]
	return ok
}

type TreeView struct {
	Roots []TreeNode `json:"roots"`
}

type TreeNode struct {
	ID        world.EntityID `json:"id"`
	TypeLabel string         `json:"type"`
	Name      string         `json:"name,omitempty"`
	Destroyed bool           `json:"destroyed,omitempty"`
	Children  []TreeNode     `json:"children,omitempty"`
}

func BuildTreeView(entities []world.Entity, rels []world.Relationship) TreeView {
	parent := make(map[world.EntityID]world.EntityID, len(entities))
	children := make(map[world.EntityID][]world.EntityID)

	for _, r := range rels {
		if !r.IsAlive() {
			continue
		}
		if !isContainment(r.Kind) {
			continue
		}
		var p, c world.EntityID
		switch strings.ToLower(strings.TrimSpace(r.Kind)) {
		case "contains":
			p, c = r.From, r.To
		default:
			p, c = r.To, r.From
		}
		if _, taken := parent[c]; taken {
			continue
		}
		parent[c] = p
		children[p] = append(children[p], c)
	}

	byID := make(map[world.EntityID]world.Entity, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}

	// Sort children stably by ID for deterministic rendering.
	for k := range children {
		sort.Slice(children[k], func(i, j int) bool { return children[k][i] < children[k][j] })
	}

	var build func(id world.EntityID) TreeNode
	build = func(id world.EntityID) TreeNode {
		e := byID[id]
		n := TreeNode{
			ID:        e.ID,
			TypeLabel: e.TypeLabel,
			Name:      extractName(e.Properties),
			Destroyed: !e.IsAlive(),
		}
		for _, ch := range children[id] {
			n.Children = append(n.Children, build(ch))
		}
		return n
	}

	view := TreeView{}
	// Roots = entities with no recorded parent. Iterate in ID order.
	ids := make([]world.EntityID, 0, len(entities))
	for _, e := range entities {
		ids = append(ids, e.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, hasParent := parent[id]; hasParent {
			continue
		}
		view.Roots = append(view.Roots, build(id))
	}
	return view
}

func extractName(p world.Properties) string {
	if p == nil {
		return ""
	}
	if s, ok := p["name"].(string); ok {
		return s
	}
	return ""
}

func RenderTree(v TreeView, s Styles) string {
	if len(v.Roots) == 0 {
		return s.Faint.Render("(void - nothing has been created yet)")
	}
	var b strings.Builder
	for i, r := range v.Roots {
		writeNode(&b, r, "", true, s)
		if i < len(v.Roots)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeNode(b *strings.Builder, n TreeNode, prefix string, isRoot bool, s Styles) {
	label := nodeLabel(n, s)
	if isRoot {
		b.WriteString(label)
	} else {
		b.WriteString(prefix)
		b.WriteString(label)
	}
	for i, c := range n.Children {
		b.WriteByte('\n')
		last := i == len(n.Children)-1
		branch := "├─ "
		nextPrefix := prefix + "│  "
		if isRoot {
			branch = "├─ "
			nextPrefix = "│  "
		}
		if last {
			branch = "└─ "
			if isRoot {
				nextPrefix = "   "
			} else {
				nextPrefix = prefix + "   "
			}
		}
		childPrefix := prefix
		if isRoot {
			childPrefix = ""
		}
		b.WriteString(childPrefix)
		b.WriteString(branch)
		writeNodeBody(b, c, nextPrefix, s)
	}
}

func writeNodeBody(b *strings.Builder, n TreeNode, prefix string, s Styles) {
	b.WriteString(nodeLabel(n, s))
	for i, c := range n.Children {
		b.WriteByte('\n')
		last := i == len(n.Children)-1
		branch := "├─ "
		nextPrefix := prefix + "│  "
		if last {
			branch = "└─ "
			nextPrefix = prefix + "   "
		}
		b.WriteString(prefix)
		b.WriteString(branch)
		writeNodeBody(b, c, nextPrefix, s)
	}
}

func nodeLabel(n TreeNode, s Styles) string {
	main := n.TypeLabel
	if n.Name != "" {
		main = fmt.Sprintf("%s %q", n.TypeLabel, n.Name)
	}
	meta := s.TreeMeta.Render(fmt.Sprintf("#%d", n.ID))
	if n.Destroyed {
		return s.TreeDeleted.Render(main) + " " + meta
	}
	if len(n.Children) > 0 {
		return s.TreeRoot.Render(main) + " " + meta
	}
	return s.TreeLeaf.Render(main) + " " + meta
}
