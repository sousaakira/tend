package vt

// Color is a terminal colour packed into 32 bits: an 8-bit kind tag in the
// high byte and the payload below it. Keeping it to a word matters because a
// cell carries two of them and cells are allocated per column × row ×
// scrollback × pane.
type Color uint32

const (
	colorKindDefault uint32 = 0
	colorKindIndexed uint32 = 1
	colorKindRGB     uint32 = 2

	colorKindShift = 24
	colorPayload   = 1<<colorKindShift - 1
)

// DefaultColor is the terminal's own foreground or background, whichever the
// cell uses it for. It is the zero value, so a zeroed Cell is already correct.
const DefaultColor Color = 0

// IndexedColor returns one of the 256 palette entries.
func IndexedColor(i uint8) Color {
	return Color(colorKindIndexed<<colorKindShift | uint32(i))
}

// RGBColor returns a direct colour.
func RGBColor(r, g, b uint8) Color {
	return Color(colorKindRGB<<colorKindShift | uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}

func (c Color) kind() uint32 { return uint32(c) >> colorKindShift }

// IsDefault reports whether c defers to the terminal's own colour.
func (c Color) IsDefault() bool { return c.kind() == colorKindDefault }

// IsIndexed reports whether c is a palette entry.
func (c Color) IsIndexed() bool { return c.kind() == colorKindIndexed }

// IsRGB reports whether c is a direct colour.
func (c Color) IsRGB() bool { return c.kind() == colorKindRGB }

// Index returns the palette entry for an indexed colour, and 0 otherwise.
func (c Color) Index() uint8 {
	if !c.IsIndexed() {
		return 0
	}
	return uint8(uint32(c) & 0xFF)
}

// RGB returns the components of a direct colour, and zeros otherwise.
func (c Color) RGB() (r, g, b uint8) {
	if !c.IsRGB() {
		return 0, 0, 0
	}
	v := uint32(c) & colorPayload
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

// Attr is a set of character attributes.
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrBlink
	AttrReverse
	AttrHidden
	AttrStrike
	// AttrUnderline is set for every underline style; UnderlineStyle says which.
	AttrUnderline
)

// UnderlineStyle distinguishes the underline variants, which are mutually
// exclusive and so do not belong in Attr.
type UnderlineStyle uint8

const (
	UnderlineNone UnderlineStyle = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Style is the presentation of a cell.
type Style struct {
	FG, BG    Color
	Attrs     Attr
	Underline UnderlineStyle
}

// DefaultStyle is the unstyled state, and the zero value.
var DefaultStyle = Style{}

// Has reports whether every attribute in a is set.
func (s Style) Has(a Attr) bool { return s.Attrs&a == a }

// IsDefault reports whether s carries no colour or attribute at all.
func (s Style) IsDefault() bool { return s == DefaultStyle }

// Cell is one character position on the screen.
//
// Width distinguishes the three kinds of position: 1 for a normal cell, 2 for
// the left half of a double-width character, and 0 for the right half, which
// holds no rune of its own. Code that walks a row must honour width-0 cells
// rather than rendering them.
//
// Combining marks are not stored here. They are rare, and a slice header in
// every cell would cost more than they do; Row keeps them in a side table.
type Cell struct {
	R     rune
	Style Style
	Width uint8
	_     [3]uint8 // keep the padding explicit so the size test is meaningful
}

// IsContinuation reports whether c is the right half of a wide character.
func (c Cell) IsContinuation() bool { return c.Width == 0 }

// IsBlank reports whether c holds no visible character.
func (c Cell) IsBlank() bool { return c.R == 0 || c.R == ' ' }

// blankCell returns an empty cell carrying style, which is what erasing with a
// background colour must leave behind.
func blankCell(style Style) Cell {
	return Cell{R: ' ', Style: style, Width: 1}
}
