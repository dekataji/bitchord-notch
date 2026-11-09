package main

import (
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

// lblBtn is a label styled as a dark button — walk's native buttons
// cannot be recoloured, and the notch is a dark custom surface.
type lblBtn struct {
	box    *walk.Composite
	label  *walk.Label
	active bool
	onHit  func()
}

func newLblBtn(parent walk.Container, text string, font *walk.Font, onHit func()) (*lblBtn, error) {
	box, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	box.SetBackground(brush(colBtn))
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{HNear: 6, VNear: 2, HFar: 6, VFar: 2})
	l.SetSpacing(0)
	box.SetLayout(l)
	lbl, err := walk.NewLabel(box)
	if err != nil {
		return nil, err
	}
	lbl.SetText(text)
	lbl.SetFont(font)
	lbl.SetTextColor(colText)
	lbl.SetTextAlignment(walk.AlignCenter)
	b := &lblBtn{box: box, label: lbl, onHit: onHit}
	hit := func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && b.onHit != nil {
			b.onHit()
		}
	}
	box.MouseDown().Attach(hit)
	lbl.MouseDown().Attach(hit)
	return b, nil
}

func (b *lblBtn) setText(s string) { b.label.SetText(s) }

func (b *lblBtn) setActive(on bool) {
	b.active = on
	if on {
		b.label.SetTextColor(colAmber)
		b.box.SetBackground(brush(colBtnHi))
	} else {
		b.label.SetTextColor(colText)
		b.box.SetBackground(brush(colBtn))
	}
}

func (b *lblBtn) setSize(w, h int) {
	b.box.SetMinMaxSize(walk.Size{Width: w, Height: h}, walk.Size{Width: w, Height: h})
}

// strList is a walk.ListModel over plain strings.
type strList struct{ items []string }

func (m *strList) ItemCount() int              { return len(m.items) }
func (m *strList) Value(index int) interface{} { return m.items[index] }

func dragOn(w walk.Window, hwnd uintptr) {
	w.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		procRelC.Call()
		procSend.Call(hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN, HTCAPTION
	})
}
