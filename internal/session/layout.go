package session

import "math"

// Direction is how a split arranges its children.
//
// The names describe the arrangement rather than the divider, because
// "horizontal split" means opposite things in different terminal multiplexers
// and the ambiguity reliably puts panes in the wrong place.
type Direction uint8

const (
	// Columns places children side by side, separated by a vertical divider.
	Columns Direction = iota
	// Rows stacks children, separated by a horizontal divider.
	Rows
)

func (d Direction) String() string {
	if d == Rows {
		return "rows"
	}
	return "columns"
}

// Side is a direction to move focus in.
type Side uint8

const (
	Left Side = iota
	Right
	Up
	Down
)

func (s Side) String() string {
	switch s {
	case Left:
		return "left"
	case Right:
		return "right"
	case Up:
		return "up"
	default:
		return "down"
	}
}

// Rect is a region of the screen in character cells.
type Rect struct {
	X, Y, W, H int
}

// Empty reports whether the rect has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// PaneRect is where a pane landed.
type PaneRect struct {
	Pane PaneID
	Rect Rect
}

// node is a leaf holding one pane, or a split holding children.
//
// sizes holds each child's fraction of the split's extent and sums to 1. It is
// kept alongside kids rather than inside them so that a child's share is a
// property of the split that owns it, which is what makes a child collapse
// into its parent without renegotiating anyone else's share.
type node struct {
	pane  PaneID
	dir   Direction
	kids  []*node
	sizes []float64
}

func leaf(p PaneID) *node { return &node{pane: p} }

func (n *node) isLeaf() bool { return len(n.kids) == 0 }

// find locates the leaf holding p, along with its parent and index in it.
// The parent is nil when the leaf is the root.
func (n *node) find(p PaneID) (parent *node, index int, found *node) {
	if n == nil {
		return nil, 0, nil
	}
	if n.isLeaf() {
		if n.pane == p {
			return nil, 0, n
		}
		return nil, 0, nil
	}
	for i, kid := range n.kids {
		if kid.isLeaf() {
			if kid.pane == p {
				return n, i, kid
			}
			continue
		}
		if pp, idx, f := kid.find(p); f != nil {
			return pp, idx, f
		}
	}
	return nil, 0, nil
}

// split divides the pane target, inserting fresh next to it.
//
// When the enclosing split already runs in the requested direction the new
// pane becomes a sibling and takes half of target's share, so splitting twice
// the same way yields three panes in a row rather than nested halves. That is
// what people expect, and it keeps the tree shallow.
func (n *node) split(target, fresh PaneID, dir Direction, before bool) (*node, bool) {
	if n == nil {
		return nil, false
	}
	parent, idx, found := n.find(target)
	if found == nil {
		return n, false
	}

	if parent != nil && parent.dir == dir {
		share := parent.sizes[idx] / 2
		parent.sizes[idx] = share

		at := idx + 1
		if before {
			at = idx
		}
		parent.kids = insertNode(parent.kids, at, leaf(fresh))
		parent.sizes = insertFloat(parent.sizes, at, share)
		return n, true
	}

	// Otherwise the leaf becomes a split of two.
	kids := []*node{leaf(found.pane), leaf(fresh)}
	if before {
		kids[0], kids[1] = kids[1], kids[0]
	}
	replacement := &node{dir: dir, kids: kids, sizes: []float64{0.5, 0.5}}

	if parent == nil {
		return replacement, true
	}
	parent.kids[idx] = replacement
	return n, true
}

// closePane removes a pane and returns the new root, which is nil once the
// last pane is gone.
//
// The closed pane's share is given back to its siblings in proportion to what
// they already had, so closing a pane never reshuffles the panes that remain.
func (n *node) closePane(target PaneID) (*node, bool) {
	if n == nil {
		return nil, false
	}
	if n.isLeaf() {
		if n.pane == target {
			return nil, true
		}
		return n, false
	}

	parent, idx, found := n.find(target)
	if found == nil {
		return n, false
	}

	freed := parent.sizes[idx]
	parent.kids = removeNode(parent.kids, idx)
	parent.sizes = removeFloat(parent.sizes, idx)

	if remaining := 1 - freed; remaining > 0 {
		for i := range parent.sizes {
			parent.sizes[i] /= remaining
		}
	} else {
		even(parent.sizes)
	}

	// A split with one child is no longer a split.
	if len(parent.kids) == 1 {
		return collapse(n, parent), true
	}
	return n, true
}

// collapse replaces a one-child split with that child, wherever it sits.
func collapse(root, parent *node) *node {
	only := parent.kids[0]
	if parent == root {
		return only
	}
	grand, idx := parentOf(root, parent)
	if grand == nil {
		return root
	}
	grand.kids[idx] = only
	// A collapsed child that is itself a split in the same direction could be
	// merged into the grandparent. It is left nested: merging would silently
	// renumber sibling shares, and the extra level costs nothing.
	return root
}

// parentOf finds the split that holds child.
func parentOf(root, child *node) (*node, int) {
	if root == nil || root.isLeaf() {
		return nil, 0
	}
	for i, kid := range root.kids {
		if kid == child {
			return root, i
		}
		if p, idx := parentOf(kid, child); p != nil {
			return p, idx
		}
	}
	return nil, 0
}

// pathTo returns the chain of splits from the root down to the leaf holding
// target, ending with the leaf. It is nil when the pane is not in this tree.
func (n *node) pathTo(target PaneID) []*node {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		if n.pane == target {
			return []*node{n}
		}
		return nil
	}
	for _, kid := range n.kids {
		if sub := kid.pathTo(target); sub != nil {
			return append([]*node{n}, sub...)
		}
	}
	return nil
}

// minShare is the smallest fraction a pane may be squeezed to. A pane that can
// be dragged to nothing is a pane that can be lost by accident.
const minShare = 0.05

// adjust moves the divider on one side of a pane, taking the space from the
// neighbour across it.
//
// The divider that moves is the one belonging to the nearest enclosing split
// that runs along the right axis and actually has something on that side.
// Walking outwards from the pane is what makes the keys mean "this edge of
// this pane" rather than "some divider somewhere above it".
func (n *node) adjust(target PaneID, side Side, fraction float64) bool {
	if n == nil || fraction == 0 {
		return false
	}
	path := n.pathTo(target)
	if len(path) < 2 {
		return false // a lone pane has no divider to move
	}

	want := Columns
	if side == Up || side == Down {
		want = Rows
	}
	before := side == Left || side == Up

	// From the innermost split outwards.
	for i := len(path) - 2; i >= 0; i-- {
		split := path[i]
		if split.dir != want {
			continue
		}
		idx := indexOf(split.kids, path[i+1])
		if idx < 0 {
			continue
		}
		other := idx + 1
		if before {
			other = idx - 1
		}
		if other < 0 || other >= len(split.kids) {
			continue // nothing on that side at this level; try the next one out
		}

		// Growing the pane takes from the neighbour, and neither may be
		// squeezed out of existence.
		give := fraction
		if give > split.sizes[other]-minShare {
			give = split.sizes[other] - minShare
		}
		if give < minShare-split.sizes[idx] {
			give = minShare - split.sizes[idx]
		}
		if give == 0 {
			return false
		}
		split.sizes[idx] += give
		split.sizes[other] -= give
		return true
	}
	return false
}

func indexOf(kids []*node, want *node) int {
	for i, kid := range kids {
		if kid == want {
			return i
		}
	}
	return -1
}

// panes appends every pane in layout order: left to right, top to bottom.
func (n *node) panes(out []PaneID) []PaneID {
	if n == nil {
		return out
	}
	if n.isLeaf() {
		return append(out, n.pane)
	}
	for _, kid := range n.kids {
		out = kid.panes(out)
	}
	return out
}

func (n *node) count() int {
	if n == nil {
		return 0
	}
	if n.isLeaf() {
		return 1
	}
	total := 0
	for _, kid := range n.kids {
		total += kid.count()
	}
	return total
}

// layout computes where every pane goes inside area.
//
// Children tile their parent exactly: each child's far edge is computed from
// the cumulative fraction and the next child starts there, so rounding cannot
// open a gap or an overlap. The last child is pinned to the parent's edge.
func (n *node) layout(area Rect, out []PaneRect) []PaneRect {
	if n == nil {
		return out
	}
	if n.isLeaf() {
		return append(out, PaneRect{Pane: n.pane, Rect: area})
	}

	total := area.W
	if n.dir == Rows {
		total = area.H
	}

	acc := 0.0
	prev := 0
	for i, kid := range n.kids {
		acc += n.sizes[i]

		edge := total
		if i < len(n.kids)-1 {
			edge = int(math.Round(acc * float64(total)))
		}
		if edge < prev {
			edge = prev
		}
		if edge > total {
			edge = total
		}

		sub := area
		if n.dir == Rows {
			sub.Y = area.Y + prev
			sub.H = edge - prev
		} else {
			sub.X = area.X + prev
			sub.W = edge - prev
		}
		out = kid.layout(sub, out)
		prev = edge
	}
	return out
}

// Neighbor finds the pane next to from on the given side.
//
// It works on the computed rectangles rather than on the tree, because what a
// user means by "the pane to the left" is spatial: the tree can nest the two
// panes arbitrarily far apart and they are still neighbours on screen.
// Candidates are ranked by edge distance first, then by how much of the
// perpendicular edge they share, so moving focus across a column of stacked
// panes lands on the one actually beside the cursor.
func Neighbor(rects []PaneRect, from PaneID, side Side) (PaneID, bool) {
	var src Rect
	ok := false
	for _, r := range rects {
		if r.Pane == from {
			src, ok = r.Rect, true
			break
		}
	}
	if !ok {
		return 0, false
	}

	best := PaneID(0)
	bestGap := math.MaxInt
	bestOverlap := -1

	for _, r := range rects {
		if r.Pane == from {
			continue
		}
		c := r.Rect

		var gap, overlap int
		switch side {
		case Left:
			if c.X+c.W > src.X {
				continue
			}
			gap = src.X - (c.X + c.W)
			overlap = span(src.Y, src.H, c.Y, c.H)
		case Right:
			if c.X < src.X+src.W {
				continue
			}
			gap = c.X - (src.X + src.W)
			overlap = span(src.Y, src.H, c.Y, c.H)
		case Up:
			if c.Y+c.H > src.Y {
				continue
			}
			gap = src.Y - (c.Y + c.H)
			overlap = span(src.X, src.W, c.X, c.W)
		case Down:
			if c.Y < src.Y+src.H {
				continue
			}
			gap = c.Y - (src.Y + src.H)
			overlap = span(src.X, src.W, c.X, c.W)
		}
		if overlap <= 0 {
			continue
		}
		if gap < bestGap || (gap == bestGap && overlap > bestOverlap) {
			best, bestGap, bestOverlap = r.Pane, gap, overlap
		}
	}
	return best, best != 0
}

// span returns how much two intervals overlap.
func span(aStart, aLen, bStart, bLen int) int {
	lo := max(aStart, bStart)
	hi := min(aStart+aLen, bStart+bLen)
	return hi - lo
}

// --- slice helpers ---------------------------------------------------------

func insertNode(s []*node, at int, v *node) []*node {
	s = append(s, nil)
	copy(s[at+1:], s[at:])
	s[at] = v
	return s
}

func insertFloat(s []float64, at int, v float64) []float64 {
	s = append(s, 0)
	copy(s[at+1:], s[at:])
	s[at] = v
	return s
}

func removeNode(s []*node, at int) []*node {
	return append(s[:at], s[at+1:]...)
}

func removeFloat(s []float64, at int) []float64 {
	return append(s[:at], s[at+1:]...)
}

func even(sizes []float64) {
	if len(sizes) == 0 {
		return
	}
	share := 1 / float64(len(sizes))
	for i := range sizes {
		sizes[i] = share
	}
}

// SplitRect is one split in a tab's layout: which way it divides, the share
// of its first part, and the area it divides. herdr's pane.layout reports
// these beside the panes, so a plugin can tell how a tab is built and not
// only where its panes ended up.
type SplitRect struct {
	Dir   Direction
	Ratio float64
	Rect  Rect
}

// Splits lists the tab's splits, outermost first, laid out in area.
func (t *Tab) Splits(area Rect) []SplitRect {
	var out []SplitRect
	var walk func(n *node, area Rect)
	walk = func(n *node, area Rect) {
		if n == nil || n.isLeaf() {
			return
		}
		out = append(out, SplitRect{Dir: n.dir, Ratio: n.sizes[0], Rect: area})
		rects := (&node{dir: n.dir, kids: leaves(len(n.kids)), sizes: n.sizes}).layout(area, nil)
		for i, kid := range n.kids {
			walk(kid, rects[i].Rect)
		}
	}
	walk(t.root, area)
	return out
}

// leaves is n placeholder leaves, for laying out one level of a split.
func leaves(n int) []*node {
	out := make([]*node, n)
	for i := range out {
		out[i] = leaf(PaneID(i + 1))
	}
	return out
}
