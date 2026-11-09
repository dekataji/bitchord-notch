package main

import (
	"time"

	"github.com/lxn/walk"
)

var (
	fontTitle, _ = walk.NewFont("Segoe UI", 11, walk.FontBold)
	fontSub, _   = walk.NewFont("Segoe UI", 9, 0)
	fontBtn, _   = walk.NewFont("Segoe UI", 12, 0)
	fontTab, _   = walk.NewFont("Segoe UI", 10, 0)
	fontList, _  = walk.NewFont("Segoe UI", 10, 0)
	fontForm, _  = walk.NewFont("Segoe UI", 10, 0)
)

// brush wraps NewSolidColorBrush, dropping the error (a solid brush
// for an RGB colour cannot realistically fail).
func brush(c walk.Color) walk.Brush {
	b, _ := walk.NewSolidColorBrush(c)
	return b
}

func pen(c walk.Color, width int) walk.Pen {
	p, _ := walk.NewCosmeticPen(walk.PenSolid, c)
	_ = width
	return p
}

// ── chips: self-painted buttons on walk.CustomWidget ────────────────
//
// Third time lucky. v0.1.0's Composite+Label buttons were dead
// (their MouseDown events never fire — verified on the user's
// Windows and under Wine: label clicks vanish, composite clicks
// fall through to an ancestor). v0.1.1 used native PushButtons
// (work, but unstyleable). v0.2.0's first attempt dispatched clicks
// from ancestor containers by hit-testing Bounds() — fragile,
// because the ancestors that receive a click and the coordinate
// spaces involved vary by environment.
//
// A walk.CustomWidget, by contrast, provably receives its OWN
// MouseDown with local coordinates (verified under Wine against
// Composite/Label/PushButton controls in test/repro2), and paints
// itself through a Canvas — so a chip is a self-contained painted
// button in the Remote page's design language: panel-2 fill, 1px
// border (sky on hover), mint for the primary play button and for
// active toggles.

type chip struct {
	cw   *walk.CustomWidget
	text string

	font  *walk.Font
	fontB *walk.Font

	primary bool // mint-filled (play, like .tbtn-main)
	round   bool // circular (transport buttons, like .tbtn)
	toggle  bool // active state = mint border + mint bold text
	left    bool // left-aligned text (settings toggles)

	active  bool
	hovered bool
	onHit   func()
}

func (a *app) newChip(parent walk.Container, text string, font *walk.Font, onHit func()) (*chip, error) {
	c := &chip{text: text, font: font, onHit: onHit}
	if font != nil {
		if fb, err := walk.NewFont(font.Family(), font.PointSize(), walk.FontBold); err == nil {
			c.fontB = fb
		}
	}
	cw, err := walk.NewCustomWidget(parent, 0, c.paint)
	if err != nil {
		return nil, err
	}
	c.cw = cw
	cw.SetBackground(brush(colBG))
	cw.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && c.onHit != nil {
			debugLog("chip hit: " + c.text)
			c.onHit()
		}
	})
	a.chips = append(a.chips, c)
	return c, nil
}

func (c *chip) paint(canvas *walk.Canvas, bounds walk.Rectangle) error {
	bg, fg, border := colPanel2, colText, colBorder
	if c.primary {
		bg, fg, border = colMint, colMintInk, colMint
		if c.hovered {
			bg, border = colMintHi, colMintHi
		}
	} else {
		if c.hovered {
			bg, border = colHover, colSky
		}
		if c.toggle && c.active {
			border, fg = colMint, colMint
		}
	}
	r := walk.Rectangle{X: 0, Y: 0, Width: bounds.Width - 1, Height: bounds.Height - 1}
	if c.round {
		_ = canvas.FillEllipse(brush(bg), r)
		_ = canvas.DrawEllipse(pen(border, 1), r)
	} else {
		ell := walk.Size{Width: 8, Height: 8}
		_ = canvas.FillRoundedRectangle(brush(bg), r, ell)
		_ = canvas.DrawRoundedRectangle(pen(border, 1), r, ell)
	}
	f := c.font
	if c.toggle && c.active && c.fontB != nil {
		f = c.fontB
	}
	tr := r
	format := walk.TextCenter
	if c.left {
		format = walk.TextLeft
		tr.X += 10
	}
	return canvas.DrawText(c.text, f, fg, tr,
		format|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
}

func (c *chip) setText(s string) {
	c.text = s
	c.cw.Invalidate()
}

func (c *chip) setActive(on bool) {
	if c.active != on {
		c.active = on
		c.cw.Invalidate()
	}
}

func (c *chip) setSize(w, h int) {
	c.cw.SetMinMaxSize(walk.Size{Width: w, Height: h},
		walk.Size{Width: w, Height: h})
}

func (c *chip) setVisible(on bool) { c.cw.SetVisible(on) }
func (c *chip) visible() bool      { return c.cw.Visible() }

// setHovered is driven by the app's hover poll (real window rects,
// no walk coordinate maths).
func (c *chip) setHovered(on bool) {
	if c.hovered != on {
		c.hovered = on
		c.cw.Invalidate()
	}
}

// ── seek bar (painted, self-hit-testing) ────────────────────────────

type seekBar struct {
	cw *walk.CustomWidget
	a  *app
}

func (a *app) newSeekBar(parent walk.Container) (*seekBar, error) {
	s := &seekBar{a: a}
	cw, err := walk.NewCustomWidget(parent, 0, s.paint)
	if err != nil {
		return nil, err
	}
	s.cw = cw
	cw.SetBackground(brush(colBG))
	cw.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.seekGesture(x, cw.Size().Width, true)
		}
	})
	cw.MouseMove().Attach(func(x, y int, button walk.MouseButton) {
		if a.seeking {
			a.seekGesture(x, cw.Size().Width, false)
		}
	})
	cw.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		a.seeking = false
	})
	cw.SetMinMaxSize(walk.Size{Width: 260, Height: seekRowH},
		walk.Size{Width: 100000, Height: seekRowH})
	return s, nil
}

func (s *seekBar) paint(canvas *walk.Canvas, bounds walk.Rectangle) error {
	// A 10px track centred in the row-height widget.
	y := (bounds.Height - 10) / 2
	track := walk.Rectangle{X: 0, Y: y, Width: bounds.Width - 1, Height: 10}
	ell := walk.Size{Width: 5, Height: 5}
	_ = canvas.FillRoundedRectangle(brush(colPanel2), track, ell)
	fw := int(s.a.progFrac * float64(track.Width))
	if fw > 0 {
		fill := walk.Rectangle{X: 0, Y: y, Width: fw, Height: 10}
		_ = canvas.FillRoundedRectangle(brush(colMint), fill, ell)
	}
	return nil
}

func (s *seekBar) invalidate() { s.cw.Invalidate() }

// ── pill progress strip (painted, display only) ─────────────────────

type progStrip struct {
	cw *walk.CustomWidget
	a  *app
}

func (a *app) newProgStrip(parent walk.Container) (*progStrip, error) {
	s := &progStrip{a: a}
	cw, err := walk.NewCustomWidget(parent, 0, s.paint)
	if err != nil {
		return nil, err
	}
	s.cw = cw
	cw.SetBackground(brush(colPanel2))
	cw.SetMinMaxSize(walk.Size{Width: notchW, Height: stripH},
		walk.Size{Width: notchW, Height: stripH})
	return s, nil
}

func (s *progStrip) paint(canvas *walk.Canvas, bounds walk.Rectangle) error {
	fw := int(s.a.progFrac * float64(bounds.Width))
	if fw > 0 {
		_ = canvas.FillRectangle(brush(colMint),
			walk.Rectangle{X: 0, Y: 0, Width: fw, Height: bounds.Height})
	}
	return nil
}

func (s *progStrip) invalidate() { s.cw.Invalidate() }

// ── window dragging ─────────────────────────────────────────────────

var lastDragStart time.Time

// dragOn starts a window drag when w is left-clicked. Several
// widgets can report the same physical click in some environments,
// so starts are debounced.
func dragOn(w walk.Window, hwnd uintptr) {
	w.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		if time.Since(lastDragStart) < 400*time.Millisecond {
			return
		}
		lastDragStart = time.Now()
		procRelC.Call()
		procSend.Call(hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN, HTCAPTION
	})
}
