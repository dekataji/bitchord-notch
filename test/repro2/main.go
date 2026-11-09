// repro2: which walk widget types actually receive MouseDown when
// genuinely clicked? One big widget of each kind, log on click.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lxn/walk"
)

func logf(format string, args ...interface{}) {
	exe, _ := os.Executable()
	f, err := os.OpenFile(filepath.Join(filepath.Dir(exe), "repro2.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", args...)
}

func brush(c walk.Color) walk.Brush { b, _ := walk.NewSolidColorBrush(c); return b }

func main() {
	logf("repro2 start")
	mw, err := walk.NewMainWindow()
	if err != nil {
		logf("mw err %v", err)
		return
	}
	mw.SetTitle("Repro2")
	mw.SetSize(walk.Size{Width: 460, Height: 460})
	lay := walk.NewVBoxLayout()
	lay.SetMargins(walk.Margins{})
	lay.SetSpacing(4)
	mw.SetLayout(lay)

	comp, _ := walk.NewComposite(mw)
	comp.SetBackground(brush(walk.RGB(200, 60, 60)))
	comp.SetMinMaxSize(walk.Size{Width: 460, Height: 100}, walk.Size{Width: 460, Height: 100})
	comp.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		logf("COMPOSITE down (%d,%d)", x, y)
	})

	lbl, _ := walk.NewLabel(mw)
	lbl.SetText("LABEL click target")
	lbl.SetBackground(brush(walk.RGB(60, 200, 60)))
	lbl.SetMinMaxSize(walk.Size{Width: 460, Height: 100}, walk.Size{Width: 460, Height: 100})
	lbl.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		logf("LABEL down (%d,%d)", x, y)
	})

	cw, _ := walk.NewCustomWidget(mw, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		canvas.FillRectangle(brush(walk.RGB(60, 60, 200)), updateBounds)
		f, _ := walk.NewFont("Segoe UI", 12, 0)
		canvas.DrawText("CUSTOM WIDGET", f, walk.RGB(255, 255, 255), updateBounds, walk.TextCenter)
		return nil
	})
	cw.SetMinMaxSize(walk.Size{Width: 460, Height: 100}, walk.Size{Width: 460, Height: 100})
	cw.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		logf("CUSTOMWIDGET down (%d,%d)", x, y)
	})

	pb, _ := walk.NewPushButton(mw)
	pb.SetText("PUSHBUTTON")
	pb.SetMinMaxSize(walk.Size{Width: 460, Height: 60}, walk.Size{Width: 460, Height: 60})
	pb.Clicked().Attach(func() { logf("PUSHBUTTON clicked") })
	pb.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		logf("PUSHBUTTON down (%d,%d)", x, y)
	})

	mw.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		logf("MAINWINDOW down (%d,%d)", x, y)
	})
	mw.SetVisible(true)
	mw.Run()
}
