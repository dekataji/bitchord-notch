// BitChord Notch — a portable, single-binary Windows companion for a
// BitChord phone running the web remote. It floats at the top of the
// screen as a small notch: transport + now playing when collapsed,
// queue management and a synced lyrics viewer when expanded.
//
// It talks to the phone through the VPS relay
// (https://remote.gaspol.men/r/<token>/...) or directly on the LAN.
// Settings live in bitchord-notch.json next to the exe.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"bitchord-notch/client"
)

//go:embed icon.ico
var iconICO []byte

// Design tokens from the Remote page (remote_player.html in the
// BitChord fork): deep blue-black surfaces, mint accent.
var (
	colBG      = walk.RGB(0x0B, 0x0E, 0x14)
	colPanel   = walk.RGB(0x15, 0x1A, 0x23)
	colPanel2  = walk.RGB(0x1C, 0x23, 0x30)
	colBorder  = walk.RGB(0x26, 0x2F, 0x42)
	colHover   = walk.RGB(0x23, 0x2B, 0x3C)
	colText    = walk.RGB(0xE8, 0xEC, 0xF4)
	colMuted   = walk.RGB(0x8B, 0x94, 0xA7)
	colMint    = walk.RGB(0x69, 0xF0, 0xAE)
	colMintHi  = walk.RGB(0x8C, 0xF5, 0xC3)
	colMintInk = walk.RGB(0x06, 0x28, 0x1A)
	colSky     = walk.RGB(0x4F, 0xC3, 0xF7)
	colDanger  = walk.RGB(0xFF, 0x5C, 0x7A)
)

const (
	notchW     = 500
	collapsedH = 58
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	procRelC = user32.NewProc("ReleaseCapture")
	procSend = user32.NewProc("SendMessageW")
	procRgn  = user32.NewProc("SetWindowRgn")
	procCRgn = gdi32.NewProc("CreateRoundRectRgn")

	procCursorPos  = user32.NewProc("GetCursorPos")
	procWindowRect = user32.NewProc("GetWindowRect")
	procGetFocus   = user32.NewProc("GetFocus")
)

type winPoint struct{ X, Y int32 }
type winRect struct{ Left, Top, Right, Bottom int32 }

var debugLog = func() func(string) {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	_, statErr := os.Stat(filepath.Join(dir, "debug.on"))
	if os.Getenv("BITCHORD_NOTCH_DEBUG") == "" && statErr != nil {
		return func(string) {}
	}
	path := filepath.Join(dir, "bitchord-notch.log")
	return func(msg string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
	}
}()

type app struct {
	cfgPath string
	cl      *client.Client
	chips   []*chip

	mw       *walk.MainWindow
	ni       *walk.NotifyIcon
	bar      *barWidgets
	titleLbl *walk.Label
	subLbl   *walk.Label
	timeLbl  *walk.Label

	expBox      *walk.Composite
	tabQueue    *chip
	tabLyr      *chip
	tabSet      *chip
	collapseBtn *chip
	pageBox     *walk.Composite
	tabsRow     *walk.Composite
	seekRowW    *walk.Composite
	ctlRowW     *walk.Composite
	queuePin    int
	lyricsPin   int
	seekW       *seekBar
	progFrac    float64
	volSlider   *walk.Slider
	shufBtn     *chip
	repBtn      *chip

	queuePage *queuePage
	lyrPage   *lyricsPage
	setPage   *settingsPage

	mu          sync.Mutex
	state       *client.State
	errText     string
	errUntil    time.Time
	dispPosMs   int64
	lastTick    time.Time
	activeTab   string
	allowClose  bool
	volSentAt   time.Time
	lyricsReqID int

	// pill / full mode machine
	mode          string // "pill" or "full"
	curBarH       int
	manualOpen    bool
	everConn      bool
	hoverIn       bool
	hoverOutAt    time.Time
	holdFullUntil time.Time
	holdPillUntil time.Time
	seeking       bool
}

func main() {
	debugLog("app start")
	cfgPath := client.DefaultConfigPath()
	cfg := client.LoadConfig(cfgPath)
	a := &app{
		cfgPath:   cfgPath,
		cl:        client.New(cfg),
		activeTab: "queue",
		mode:      "pill",
	}
	if err := a.buildWindow(); err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		os.Exit(1)
	}
	a.buildTray()
	if !cfg.Ready() {
		a.holdFullUntil = time.Now().Add(24 * time.Hour)
		a.manualOpen = true
		a.showTab("settings")
		a.setPage.setStatus("Enter your relay URL, relay token and the remote PIN, then Save.")
	}
	a.evaluateMode()
	go a.pollLoop()
	go a.tickLoop()
	go a.hoverLoop()
	a.mw.Run()
}

// ── window construction ─────────────────────────────────────────────

func (a *app) buildWindow() error {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return err
	}
	a.mw = mw
	mw.SetTitle("BitChord Notch")
	mw.SetBackground(brush(colBG))
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{})
	layout.SetSpacing(0)
	mw.SetLayout(layout)
	mw.SetMinMaxSize(walk.Size{Width: notchW, Height: collapsedH},
		walk.Size{Width: notchW, Height: expandedH})

	bar, err := a.buildBar()
	if err != nil {
		return err
	}
	a.bar = bar
	a.titleLbl = bar.title
	a.subLbl = bar.sub
	a.timeLbl = bar.time

	exp, err := a.buildExpanded(mw)
	if err != nil {
		return err
	}
	a.expBox = exp
	exp.SetVisible(false)

	mw.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		a.seeking = false
	})
	a.restyleWindow()
	a.placeTopCenter(collapsedH)
	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !a.allowClose {
			*canceled = true
			mw.Hide()
		}
	})
	mw.SetVisible(true)
	return nil
}

// restyleWindow strips the window chrome, pins it on top and rounds
// the corners, notch-style. Best-effort: if any call is unavailable
// the app still works as a plain small window.
func (a *app) restyleWindow() {
	hwnd := a.mw.Handle()
	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	style &^= win.WS_CAPTION | win.WS_THICKFRAME | win.WS_SYSMENU |
		win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX
	style |= 0x80000000 | 0x10000000 // WS_POPUP | WS_VISIBLE
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_FRAMECHANGED|win.SWP_NOACTIVATE)
	a.roundCorners(collapsedH)
}

func (a *app) roundCorners(h int) {
	rgn, _, _ := procCRgn.Call(
		0, 0, uintptr(notchW+1), uintptr(h+1), 22, 22,
	)
	if rgn != 0 {
		procRgn.Call(uintptr(a.mw.Handle()), rgn, 1)
	}
}

func (a *app) placeTopCenter(h int) {
	sw := int(win.GetSystemMetrics(win.SM_CXSCREEN))
	a.mw.SetBounds(walk.Rectangle{
		X: (sw - notchW) / 2, Y: 0, Width: notchW, Height: h,
	})
}

func (a *app) buildTray() {
	icon := loadIcon()
	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return
	}
	a.ni = ni
	if icon != nil {
		_ = ni.SetIcon(icon)
		_ = a.mw.SetIcon(icon)
	}
	_ = ni.SetToolTip("BitChord Notch")
	menu := ni.ContextMenu()
	addItem := func(text string, fn func()) {
		act := walk.NewAction()
		_ = act.SetText(text)
		act.Triggered().Attach(fn)
		_ = menu.Actions().Add(act)
	}
	addItem("Show / hide", func() { a.toggleVisible() })
	addItem("Expand / collapse", func() { a.setExpanded(a.mode != "full") })
	addItem("Exit", func() {
		a.allowClose = true
		if a.ni != nil {
			a.ni.Dispose()
		}
		a.mw.Close()
	})
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.toggleVisible()
		}
	})
	_ = ni.SetVisible(true)
}

func (a *app) toggleVisible() {
	if a.mw.Visible() {
		a.mw.Hide()
	} else {
		a.mw.SetVisible(true)
		a.mw.Activate()
	}
}

func loadIcon() *walk.Icon {
	dir, err := os.MkdirTemp("", "bcnotch")
	if err != nil {
		return nil
	}
	path := filepath.Join(dir, "icon.ico")
	if err := os.WriteFile(path, iconICO, 0o644); err != nil {
		return nil
	}
	icon, err := walk.NewIconFromFile(path)
	if err != nil {
		return nil
	}
	return icon
}

// ── pill / full mode machine ────────────────────────────────────────
//
// With auto-collapse on (the default), the notch is a slim pill
// while the mouse is elsewhere and opens to the full window when
// hovered. With it off, the chevron / tray toggles it manually.
// Temporary holds let explicit actions (first-run setup, the tray
// toggle) override the hover logic for a while.

// setExpanded is the manual control: chevron, collapse chip, tray.
func (a *app) setExpanded(on bool) {
	a.manualOpen = on
	if a.cl.Config().AutoCollapseOn() {
		if on {
			a.holdFullUntil = time.Now().Add(30 * time.Second)
			a.holdPillUntil = time.Time{}
		} else {
			a.holdPillUntil = time.Now().Add(30 * time.Second)
			a.holdFullUntil = time.Time{}
		}
	}
	a.evaluateMode()
}

func (a *app) onSettingsSaved() {
	a.holdFullUntil = time.Time{}
	a.evaluateMode()
}

func (a *app) hoverLoop() {
	hwnd := uintptr(a.mw.Handle())
	for {
		var pt winPoint
		var rc winRect
		procCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		procWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		bottom := rc.Bottom
		a.mu.Lock()
		mode, barH := a.mode, a.curBarH
		a.mu.Unlock()
		if mode == "pill" && barH > 0 {
			// The window's frame can be taller than the visible
			// (region-clipped) pill; hover only counts over the
			// pill itself, with a little slack for the frame.
			if b := rc.Top + int32(barH) + 24; b < bottom {
				bottom = b
			}
		}
		inside := pt.X >= rc.Left && pt.X < rc.Right && pt.Y >= rc.Top && pt.Y < bottom
		a.mu.Lock()
		if inside != a.hoverIn {
			a.hoverIn = inside
			if !inside {
				a.hoverOutAt = time.Now()
			}
		}
		a.mu.Unlock()
		a.mw.Synchronize(func() {
			a.pollChipHover(pt)
			a.evaluateMode()
		})
		time.Sleep(250 * time.Millisecond)
	}
}

// interactionGuard reports whether the user is mid-interaction, in
// which case the window must not collapse under them: an active
// seek drag, or typing in a Settings field. (A plain "our window
// has focus" check would be wrong here — a topmost utility keeps
// focus after any click, and auto-collapse would never engage.)
func (a *app) interactionGuard() bool {
	if a.seeking {
		return true
	}
	focus, _, _ := procGetFocus.Call()
	if focus == 0 || a.setPage == nil {
		return false
	}
	for _, ed := range []*walk.LineEdit{
		a.setPage.urlEd, a.setPage.tokEd, a.setPage.pinEd,
		a.setPage.nameEd, a.setPage.delayEd,
	} {
		if ed != nil && focus == uintptr(ed.Handle()) {
			return true
		}
	}
	return false
}

// evaluateMode resolves the current target mode and applies it.
// Runs on the UI thread.
func (a *app) evaluateMode() {
	cfg := a.cl.Config()
	now := time.Now()
	full := false
	switch {
	case now.Before(a.holdFullUntil):
		full = true
	case now.Before(a.holdPillUntil):
		full = false
	case !cfg.AutoCollapseOn():
		full = a.manualOpen
	case !a.everConn:
		full = true
	default:
		a.mu.Lock()
		hover, outAt := a.hoverIn, a.hoverOutAt
		a.mu.Unlock()
		if hover || a.interactionGuard() {
			full = true
		} else if a.mode == "full" && now.Sub(outAt) < cfg.CollapseDelay() {
			full = true // grace period before collapsing
		}
	}
	target := "pill"
	if full {
		target = "full"
	}
	a.applyMode(target, cfg)
}

func (a *app) applyMode(target string, cfg client.Config) {
	auto := cfg.AutoCollapseOn()
	isFull := target == "full"
	// Bar affordances per mode.
	a.bar.chev.setVisible(!isFull && !auto)
	a.collapseBtn.setVisible(isFull && !auto)
	controls := isFull || cfg.PillControlsOn()
	a.bar.prev.setVisible(controls)
	a.bar.play.setVisible(controls)
	a.bar.next.setVisible(controls)
	strip := !isFull && cfg.PillProgressOn()
	a.bar.strip.cw.SetVisible(strip)
	barH := collapsedH
	if strip {
		barH += stripH
	}
	a.curBarH = barH
	a.bar.wrap.SetMinMaxSize(walk.Size{Width: notchW, Height: barH},
		walk.Size{Width: notchW, Height: barH})

	// While in pill mode the page host must not impose its fixed
	// height: walk floors the window size at the layout's minimum,
	// and a 322-tall page box would keep the pill 500 tall.
	if isFull {
		a.pageBox.SetMinMaxSize(walk.Size{Width: notchW - 20, Height: pageH},
			walk.Size{Width: notchW - 20, Height: pageH})
	} else {
		a.pageBox.SetMinMaxSize(walk.Size{Width: notchW - 20, Height: 0},
			walk.Size{Width: notchW - 20, Height: 0})
	}
	if target == a.mode {
		return
	}
	a.mode = target
	if isFull {
		a.expBox.SetVisible(true)
		a.placeTopCenter(expandedH)
		a.roundCorners(expandedH)
	} else {
		a.placeTopCenter(barH)
		a.roundCorners(barH)
		a.expBox.SetVisible(false)
	}
	// Without this, the bar's painted widgets can stay stale after
	// the shrink under some environments.
	win.InvalidateRect(a.mw.Handle(), nil, true)
}

func (a *app) showTab(name string) {
	a.activeTab = name
	a.queuePage.box.SetVisible(name == "queue")
	a.lyrPage.box.SetVisible(name == "lyrics")
	a.setPage.box.SetVisible(name == "settings")
	for n, b := range map[string]*chip{
		"queue": a.tabQueue, "lyrics": a.tabLyr, "settings": a.tabSet,
	} {
		b.setActive(n == name)
	}
	if name == "lyrics" {
		a.maybeFetchLyrics()
	}
}

// ── polling ─────────────────────────────────────────────────────────

func (a *app) pollLoop() {
	for {
		a.pollOnce()
		time.Sleep(1 * time.Second)
	}
}

func (a *app) pollOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	st, err := a.cl.State(ctx)
	cancel()
	a.mu.Lock()
	if err == nil {
		a.state = st
		a.dispPosMs = st.PositionMs
		a.everConn = true
	}
	a.mu.Unlock()
	a.mw.Synchronize(func() { a.applyState(err) })
	if err == nil {
		a.maybeFetchLyrics()
	}
}

func (a *app) tickLoop() {
	a.lastTick = time.Now()
	t := time.NewTicker(250 * time.Millisecond)
	for range t.C {
		now := time.Now()
		dt := now.Sub(a.lastTick)
		a.lastTick = now
		a.mu.Lock()
		st := a.state
		if st != nil && st.Playing && !st.Loading {
			a.dispPosMs += dt.Milliseconds()
			if st.DurationMs > 0 && a.dispPosMs > st.DurationMs {
				a.dispPosMs = st.DurationMs
			}
		}
		pos := a.dispPosMs
		a.mu.Unlock()
		if st != nil {
			a.mw.Synchronize(func() {
				a.updateProgress(pos)
				a.lyrPage.highlight(pos)
			})
		}
	}
}

// applyState refreshes the header/footer from the latest snapshot.
// Runs on the UI thread.
func (a *app) applyState(pollErr error) {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	if pollErr != nil {
		a.titleLbl.SetText("BitChord — not connected")
		a.subLbl.SetText(shortErr(pollErr))
		a.subLbl.SetTextColor(colDanger)
		a.bar.play.setText("▶")
		return
	}
	if st == nil || st.Current == nil {
		a.titleLbl.SetText("BitChord — nothing playing")
		a.subLbl.SetText("Open a song on the phone")
		a.subLbl.SetTextColor(colMuted)
		a.bar.play.setText("▶")
		a.timeLbl.SetText("")
		a.queuePage.applyState(st)
		return
	}
	a.titleLbl.SetText(st.Current.Title)
	artist := st.Current.Artist
	if st.Current.QueuedBy != "" {
		artist += " · queued by " + st.Current.QueuedBy
	}
	if a.errText != "" && time.Now().Before(a.errUntil) {
		artist = "⚠ " + a.errText
		a.subLbl.SetTextColor(colDanger)
	} else {
		a.subLbl.SetTextColor(colMuted)
	}
	a.subLbl.SetText(artist)
	if st.Playing {
		a.bar.play.setText("❚❚")
	} else {
		a.bar.play.setText("▶")
	}
	a.updateProgress(a.dispPosMs)
	// Volume slider follows the phone unless the user just moved it.
	if st.Volume != nil && st.VolumeMax != nil && *st.VolumeMax > 0 &&
		time.Since(a.volSentAt) > 2*time.Second {
		a.volSlider.SetValue((*st.Volume) * 100 / (*st.VolumeMax))
	}
	a.relayout()
	a.shufBtn.setActive(st.Shuffle)
	a.repBtn.setText(repeatLabel(st.Repeat))
	a.repBtn.setActive(st.Repeat != 0)
	a.queuePage.applyState(st)
}

func (a *app) updateProgress(posMs int64) {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	if st == nil {
		return
	}
	dur := st.DurationMs
	frac := 0.0
	if dur > 0 {
		frac = float64(posMs) / float64(dur)
		if frac > 1 {
			frac = 1
		}
	}
	a.progFrac = frac
	if a.seekW != nil {
		a.seekW.invalidate()
	}
	if a.bar != nil && a.bar.strip.cw.Visible() {
		a.bar.strip.invalidate()
	}
	a.timeLbl.SetText(fmt.Sprintf("%s / %s", fmtMs(posMs), fmtMs(dur)))
}

// seekGesture handles clicks and drags on the painted seek bar;
// x is in the bar's own coordinates.
func (a *app) seekGesture(x, width int, down bool) {
	if width <= 0 {
		return
	}
	frac := float64(x) / float64(width)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	if st == nil || st.DurationMs <= 0 {
		return
	}
	if down {
		a.seeking = true
	} else if !a.seeking {
		return
	}
	ms := int64(frac * float64(st.DurationMs))
	a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, ms) })
}

// pollChipHover refreshes chip hover states from the real cursor
// position and each chip's real window rectangle.
func (a *app) pollChipHover(pt winPoint) {
	for _, c := range a.chips {
		if !c.cw.Visible() {
			c.setHovered(false)
			continue
		}
		var rc winRect
		procWindowRect.Call(uintptr(c.cw.Handle()), uintptr(unsafe.Pointer(&rc)))
		inside := pt.X >= rc.Left && pt.X < rc.Right && pt.Y >= rc.Top && pt.Y < rc.Bottom
		c.setHovered(inside)
	}
}

// ── commands (fire-and-forget from UI handlers) ─────────────────────

func (a *app) cmd(fn func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			debugLog("command failed: " + err.Error())
			a.mu.Lock()
			a.errText = shortErr(err)
			a.errUntil = time.Now().Add(4 * time.Second)
			a.mu.Unlock()
			a.mw.Synchronize(func() {
				a.subLbl.SetText("⚠ " + shortErr(err))
			})
		}
		// Reflect the change quickly instead of waiting for the poll.
		time.Sleep(250 * time.Millisecond)
		a.pollOnce()
	}()
}

func (a *app) maybeFetchLyrics() {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	if st == nil || st.Current == nil {
		return
	}
	vid := st.Current.VideoID
	if a.lyrPage.loadedFor == vid || a.lyrPage.loadingFor == vid {
		return
	}
	a.lyricsReqID++
	req := a.lyricsReqID
	a.lyrPage.loadingFor = vid
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		ly, err := a.cl.Lyrics(ctx)
		cancel()
		a.mw.Synchronize(func() {
			if req != a.lyricsReqID {
				return
			}
			a.lyrPage.loadingFor = ""
			if err != nil {
				a.lyrPage.showMessage("Lyrics unavailable: " + shortErr(err))
				return
			}
			a.lyrPage.setLyrics(ly)
		})
	}()
}

func fmtMs(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 && strings.HasPrefix(s, "http ") {
		s = s[i+2:]
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func repeatLabel(r int) string {
	switch r {
	case 1:
		return "Repeat: all"
	case 2:
		return "Repeat: one"
	default:
		return "Repeat: off"
	}
}

// relayout pins the two list boxes to the space actually left over
// by the fixed rows at the current DPI, instead of trusting the
// design-time arithmetic — at 150%+ scaling the rows grow a little
// and a statically-sized list pushes the footer off the window.
func (a *app) relayout() {
	if a.mode != "full" || a.tabsRow == nil {
		return
	}
	expH := a.expBox.Size().Height
	if expH < 200 {
		return
	}
	fixed := a.tabsRow.Size().Height + a.seekRowW.Size().Height +
		a.ctlRowW.Size().Height + 12 + 18 // exp margins + spacing
	pageAvail := expH - fixed
	if pageAvail < 120 {
		return
	}
	if a.queuePage != nil && a.queuePage.actions != nil {
		if h := pageAvail - a.queuePage.actions.Size().Height - 6; h > 60 && absDiff(h, a.queuePin) > 2 {
			a.queuePin = h
			pinListHeight(a.queuePage.list, h)
		}
	}
	if a.lyrPage != nil {
		if h := pageAvail - 24; h > 60 && absDiff(h, a.lyricsPin) > 2 {
			a.lyricsPin = h
			pinListHeight(a.lyrPage.list, h)
		}
	}
}

func absDiff(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}
