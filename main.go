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

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"bitchord-notch/client"
)

//go:embed icon.ico
var iconICO []byte

var (
	colBG      = walk.RGB(16, 18, 20)
	colPanel   = walk.RGB(23, 25, 29)
	colBtn     = walk.RGB(38, 41, 46)
	colBtnHi   = walk.RGB(52, 56, 62)
	colText    = walk.RGB(255, 255, 255)
	colDim     = walk.RGB(154, 160, 166)
	colAmber   = walk.RGB(255, 179, 0)
	colDanger  = walk.RGB(229, 72, 77)
	notchW     = 500
	collapsedH = 58
	expandedH  = 500
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	procRelC = user32.NewProc("ReleaseCapture")
	procSend = user32.NewProc("SendMessageW")
	procRgn  = user32.NewProc("SetWindowRgn")
	procCRgn = gdi32.NewProc("CreateRoundRectRgn")
)

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

	mw       *walk.MainWindow
	ni       *walk.NotifyIcon
	barBox   *walk.Composite
	titleLbl *walk.Label
	subLbl   *walk.Label
	playBtn  *lblBtn
	timeLbl  *walk.Label

	expBox    *walk.Composite
	tabQueue  *lblBtn
	tabLyr    *lblBtn
	tabSet    *lblBtn
	pageBox   *walk.Composite
	progBar   *walk.ProgressBar
	volSlider *walk.Slider
	shufBtn   *lblBtn
	repBtn    *lblBtn

	queuePage *queuePage
	lyrPage   *lyricsPage
	setPage   *settingsPage

	mu          sync.Mutex
	state       *client.State
	errText     string
	errUntil    time.Time
	dispPosMs   int64
	lastTick    time.Time
	expanded    bool
	animating   bool
	activeTab   string
	allowClose  bool
	volSentAt   time.Time
	lyricsReqID int
}

func main() {
	debugLog("app start")
	cfgPath := client.DefaultConfigPath()
	cfg := client.LoadConfig(cfgPath)
	a := &app{
		cfgPath:   cfgPath,
		cl:        client.New(cfg),
		activeTab: "queue",
	}
	if err := a.buildWindow(); err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		os.Exit(1)
	}
	a.buildTray()
	if !cfg.Ready() {
		a.setExpanded(true)
		a.showTab("settings")
		a.setPage.setStatus("Enter your relay URL, relay token and the remote PIN, then Save.")
	}
	go a.pollLoop()
	go a.tickLoop()
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
	a.barBox = bar.box
	a.titleLbl = bar.title
	a.subLbl = bar.sub
	a.playBtn = bar.play
	a.timeLbl = bar.time

	exp, err := a.buildExpanded(mw)
	if err != nil {
		return err
	}
	a.expBox = exp
	exp.SetVisible(false)

	a.mw.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		debugLog(fmt.Sprintf("mw raw mousedown btn=%d at %d,%d", int(button), x, y))
	})
	a.barBox.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		debugLog(fmt.Sprintf("bar raw mousedown btn=%d at %d,%d", int(button), x, y))
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
	addItem("Expand / collapse", func() { a.setExpanded(!a.expanded) })
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

// ── expand / collapse ───────────────────────────────────────────────

func (a *app) setExpanded(on bool) {
	if a.animating || on == a.expanded {
		return
	}
	a.expanded = on
	if on {
		a.expBox.SetVisible(true)
	}
	a.animating = true
	target := collapsedH
	if on {
		target = expandedH
	}
	go func() {
		cur := a.mw.Bounds().Height
		for {
			step := (target - cur) / 3
			if step == 0 {
				step = target - cur
			}
			cur += step
			done := cur == target
			a.mw.Synchronize(func() {
				a.placeTopCenter(cur)
				a.roundCorners(cur)
			})
			if done {
				break
			}
			time.Sleep(14 * time.Millisecond)
		}
		a.mw.Synchronize(func() {
			a.animating = false
			if !a.expanded {
				a.expBox.SetVisible(false)
			}
		})
	}()
}

func (a *app) showTab(name string) {
	a.activeTab = name
	a.queuePage.box.SetVisible(name == "queue")
	a.lyrPage.box.SetVisible(name == "lyrics")
	a.setPage.box.SetVisible(name == "settings")
	for n, b := range map[string]*lblBtn{
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
		a.playBtn.setText("▶")
		return
	}
	if st == nil || st.Current == nil {
		a.titleLbl.SetText("BitChord — nothing playing")
		a.subLbl.SetText("Open a song on the phone")
		a.playBtn.setText("▶")
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
	}
	a.subLbl.SetText(artist)
	if st.Playing {
		a.playBtn.setText("❚❚")
	} else {
		a.playBtn.setText("▶")
	}
	a.updateProgress(a.dispPosMs)
	// Volume slider follows the phone unless the user just moved it.
	if st.Volume != nil && st.VolumeMax != nil && *st.VolumeMax > 0 &&
		time.Since(a.volSentAt) > 2*time.Second {
		a.volSlider.SetValue((*st.Volume) * 100 / (*st.VolumeMax))
	}
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
	if dur > 0 {
		a.progBar.SetRange(0, int(dur/1000))
		p := int(posMs / 1000)
		if p > int(dur/1000) {
			p = int(dur / 1000)
		}
		a.progBar.SetValue(p)
	}
	a.timeLbl.SetText(fmt.Sprintf("%s / %s", fmtMs(posMs), fmtMs(dur)))
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
