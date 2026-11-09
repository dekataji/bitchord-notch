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

// lblBtn is a button built on a native walk.PushButton. (An earlier
// version used a Composite+Label pair with MouseDown handlers: those
// handlers never fired — verified dead both on the user's Windows and
// under Wine — while native BUTTON controls receive clicks reliably.
// The dark custom styling is traded for buttons that work.)
type lblBtn struct {
	box     *walk.PushButton
	font    *walk.Font
	fontB   *walk.Font
	active  bool
	baseTxt string
}

func newLblBtn(parent walk.Container, text string, font *walk.Font, onHit func()) (*lblBtn, error) {
	pb, err := walk.NewPushButton(parent)
	if err != nil {
		return nil, err
	}
	pb.SetText(text)
	pb.SetFont(font)
	b := &lblBtn{box: pb, font: font, baseTxt: text}
	if font != nil {
		if fb, err := walk.NewFont(font.Family(), font.PointSize(), walk.FontBold); err == nil {
			b.fontB = fb
		}
	}
	pb.Clicked().Attach(func() {
		debugLog("button clicked: " + b.baseTxt)
		if onHit != nil {
			onHit()
		}
	})
	return b, nil
}

func (b *lblBtn) setText(s string) {
	b.baseTxt = s
	b.box.SetText(s)
}

// setActive marks the button by bolding it (native buttons cannot be
// recoloured under the Windows theme).
func (b *lblBtn) setActive(on bool) {
	b.active = on
	if on && b.fontB != nil {
		b.box.SetFont(b.fontB)
	} else {
		b.box.SetFont(b.font)
	}
}

func (b *lblBtn) setSize(w, h int) {
	b.box.SetMinMaxSize(walk.Size{Width: w, Height: h}, walk.Size{Width: w, Height: h})
}

func dragOn(w walk.Window, hwnd uintptr) {
	w.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		procRelC.Call()
		procSend.Call(hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN, HTCAPTION
	})
}
