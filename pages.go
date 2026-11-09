package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lxn/walk"

	"bitchord-notch/client"
)

// Layout constants (logical units). The expanded window is
// collapsedH + expandedExtra tall; the page height is what's left
// after the tabs row and the footer, and the list heights are pinned
// exactly — a walk ListBox otherwise grows its minimum height with
// the item count (ensureVisibleItemsHeight) and pushes the footer
// off the bottom of the window, which is what cropped v0.1.1 with a
// real 14-song queue.
const (
	expandedH   = 500
	tabsH       = 34
	seekRowH    = 26
	ctlRowH     = 30
	pageH       = expandedH - collapsedH - tabsH - seekRowH - ctlRowH - 12 - 18
	queueListH  = pageH - 36 // actions row + spacing
	lyricsListH = pageH - 24 // status row + spacing
	stripH      = 4
)

// ── collapsed bar (+ pill strip) ────────────────────────────────────

type barWidgets struct {
	wrap  *walk.Composite
	box   *walk.Composite
	strip *progStrip
	title *walk.Label
	sub   *walk.Label
	prev  *chip
	play  *chip
	next  *chip
	chev  *chip
	time  *walk.Label
}

func (a *app) buildBar() (*barWidgets, error) {
	w := &barWidgets{}
	wrap, err := walk.NewComposite(a.mw)
	if err != nil {
		return nil, err
	}
	w.wrap = wrap
	wl := walk.NewVBoxLayout()
	wl.SetMargins(walk.Margins{})
	wl.SetSpacing(0)
	wrap.SetLayout(wl)
	wrap.SetBackground(brush(colBG))

	box, err := walk.NewComposite(wrap)
	if err != nil {
		return nil, err
	}
	w.box = box
	box.SetBackground(brush(colBG))
	lay := walk.NewHBoxLayout()
	lay.SetMargins(walk.Margins{HNear: 10, VNear: 6, HFar: 10, VFar: 6})
	lay.SetSpacing(6)
	box.SetLayout(lay)
	box.SetMinMaxSize(walk.Size{Width: notchW, Height: collapsedH},
		walk.Size{Width: notchW, Height: collapsedH})

	if w.prev, err = a.newChip(box, "⏮", fontBtn, func() { a.cmd(a.cl.Prev) }); err != nil {
		return nil, err
	}
	w.prev.round = true
	w.prev.setSize(40, 40)
	if w.play, err = a.newChip(box, "▶", fontBtn, func() { a.cmd(a.cl.Toggle) }); err != nil {
		return nil, err
	}
	w.play.primary = true
	w.play.round = true
	w.play.setSize(44, 44)
	if w.next, err = a.newChip(box, "⏭", fontBtn, func() { a.cmd(a.cl.Next) }); err != nil {
		return nil, err
	}
	w.next.round = true
	w.next.setSize(40, 40)

	mid, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	mid.SetMinMaxSize(walk.Size{Width: 40, Height: 44},
		walk.Size{Width: 198, Height: 44})
	ml := walk.NewVBoxLayout()
	ml.SetMargins(walk.Margins{HNear: 4})
	ml.SetSpacing(0)
	mid.SetLayout(ml)
	if w.title, err = walk.NewLabel(mid); err != nil {
		return nil, err
	}
	w.title.SetText("BitChord Notch")
	w.title.SetFont(fontTitle)
	w.title.SetTextColor(colText)
	w.title.SetEllipsisMode(walk.EllipsisEnd)
	if w.sub, err = walk.NewLabel(mid); err != nil {
		return nil, err
	}
	w.sub.SetText("connecting…")
	w.sub.SetFont(fontSub)
	w.sub.SetTextColor(colMuted)

	if w.time, err = walk.NewLabel(box); err != nil {
		return nil, err
	}
	w.time.SetFont(fontSub)
	w.time.SetTextColor(colMuted)
	w.time.SetTextAlignment(walk.AlignFar)
	w.time.SetMinMaxSize(walk.Size{Width: 88, Height: 20},
		walk.Size{Width: 88, Height: 20})

	if w.chev, err = a.newChip(box, "▾", fontBtn, func() { a.setExpanded(true) }); err != nil {
		return nil, err
	}
	w.chev.setSize(36, 44)

	// Pill progress strip: a thin mint bar along the bottom edge.
	if w.strip, err = a.newProgStrip(wrap); err != nil {
		return nil, err
	}

	hwnd := uintptr(a.mw.Handle())
	dragOn(box, hwnd)
	dragOn(mid, hwnd)
	dragOn(w.title, hwnd)
	dragOn(w.sub, hwnd)
	return w, nil
}

// ── expanded area ───────────────────────────────────────────────────

func (a *app) buildExpanded(parent walk.Container) (*walk.Composite, error) {
	exp, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	exp.SetBackground(brush(colBG))
	lay := walk.NewVBoxLayout()
	lay.SetMargins(walk.Margins{HNear: 10, VNear: 2, HFar: 10, VFar: 10})
	lay.SetSpacing(6)
	exp.SetLayout(lay)
	// No fixed height here: the children pin their own heights, and
	// a fixed minimum would be reserved even while the box is hidden
	// in pill mode, keeping the window tall.
	exp.SetMinMaxSize(walk.Size{Width: notchW, Height: 1},
		walk.Size{Width: notchW, Height: 100000})

	// Tabs row.
	tabs, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	a.tabsRow = tabs
	tl := walk.NewHBoxLayout()
	tl.SetMargins(walk.Margins{})
	tl.SetSpacing(6)
	tabs.SetLayout(tl)
	mkTab := func(text, name string) (*chip, error) {
		c, err := a.newChip(tabs, text, fontTab, func() { a.showTab(name) })
		if err != nil {
			return nil, err
		}
		c.toggle = true
		c.setSize(84, tabsH)
		return c, nil
	}
	if a.tabQueue, err = mkTab("Queue", "queue"); err != nil {
		return nil, err
	}
	if a.tabLyr, err = mkTab("Lyrics", "lyrics"); err != nil {
		return nil, err
	}
	if a.tabSet, err = mkTab("Settings", "settings"); err != nil {
		return nil, err
	}
	spacer, err := walk.NewComposite(tabs)
	if err != nil {
		return nil, err
	}
	_ = tl.SetStretchFactor(spacer, 1)
	if a.collapseBtn, err = a.newChip(tabs, "▴", fontBtn, func() { a.setExpanded(false) }); err != nil {
		return nil, err
	}
	a.collapseBtn.setSize(36, tabsH)

	// Pages.
	a.pageBox, err = walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	pl := walk.NewVBoxLayout()
	pl.SetMargins(walk.Margins{})
	pl.SetSpacing(0)
	a.pageBox.SetLayout(pl)
	a.pageBox.SetMinMaxSize(walk.Size{Width: notchW - 20, Height: pageH},
		walk.Size{Width: notchW - 20, Height: pageH})
	if a.queuePage, err = a.buildQueuePage(a.pageBox); err != nil {
		return nil, err
	}
	if a.lyrPage, err = a.buildLyricsPage(a.pageBox); err != nil {
		return nil, err
	}
	if a.setPage, err = a.buildSettingsPage(a.pageBox); err != nil {
		return nil, err
	}

	// Footer: seek row, then transport extras.
	seekRow, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	a.seekRowW = seekRow
	sl := walk.NewHBoxLayout()
	sl.SetMargins(walk.Margins{})
	sl.SetSpacing(6)
	seekRow.SetLayout(sl)
	back, err := a.newChip(seekRow, "−10s", fontTab, func() {
		a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, a.dispPosMs-10_000) })
	})
	if err != nil {
		return nil, err
	}
	back.setSize(48, seekRowH)
	if a.seekW, err = a.newSeekBar(seekRow); err != nil {
		return nil, err
	}
	fwd, err := a.newChip(seekRow, "+10s", fontTab, func() {
		a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, a.dispPosMs+10_000) })
	})
	if err != nil {
		return nil, err
	}
	fwd.setSize(48, seekRowH)

	ctlRow, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	a.ctlRowW = ctlRow
	cl2 := walk.NewHBoxLayout()
	cl2.SetMargins(walk.Margins{})
	cl2.SetSpacing(6)
	ctlRow.SetLayout(cl2)
	if a.shufBtn, err = a.newChip(ctlRow, "Shuffle", fontTab, func() {
		a.cmd(a.cl.ToggleShuffle)
	}); err != nil {
		return nil, err
	}
	a.shufBtn.toggle = true
	a.shufBtn.setSize(104, ctlRowH)
	if a.repBtn, err = a.newChip(ctlRow, "Repeat: off", fontTab, func() {
		a.cmd(a.cl.CycleRepeat)
	}); err != nil {
		return nil, err
	}
	a.repBtn.toggle = true
	a.repBtn.setSize(110, ctlRowH)
	volLbl, err := walk.NewLabel(ctlRow)
	if err != nil {
		return nil, err
	}
	volLbl.SetText("Vol")
	volLbl.SetFont(fontTab)
	volLbl.SetTextColor(colMuted)
	a.volSlider, err = walk.NewSlider(ctlRow)
	if err != nil {
		return nil, err
	}
	a.volSlider.SetRange(0, 100)
	a.volSlider.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		level := float64(a.volSlider.Value()) / 100
		a.cmd(func(ctx context.Context) error { return a.cl.SetVolume(ctx, level) })
	})

	a.showTab("queue")
	return exp, nil
}

// ── queue page ──────────────────────────────────────────────────────

type queuePage struct {
	a       *app
	box     *walk.Composite
	list    *walk.ListBox
	actions *walk.Composite
	songs   []client.Song
	sig     string
}

func (a *app) buildQueuePage(parent walk.Container) (*queuePage, error) {
	q := &queuePage{a: a}
	box, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	q.box = box
	lay := walk.NewVBoxLayout()
	lay.SetMargins(walk.Margins{})
	lay.SetSpacing(6)
	box.SetLayout(lay)
	q.list, err = walk.NewListBox(box)
	if err != nil {
		return nil, err
	}
	q.list.SetFont(fontList)
	q.list.SetBackground(brush(colPanel))
	pinListHeight(q.list, queueListH)
	q.list.ItemActivated().Attach(func() {
		i := q.list.CurrentIndex()
		if i >= 0 {
			a.cmd(func(ctx context.Context) error { return a.cl.PlayIndex(ctx, i) })
		}
	})
	row, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	q.actions = row
	rl := walk.NewHBoxLayout()
	rl.SetMargins(walk.Margins{})
	rl.SetSpacing(6)
	row.SetLayout(rl)
	mk := func(text string, fn func(i int)) (*chip, error) {
		return a.newChip(row, text, fontTab, func() {
			if i := q.list.CurrentIndex(); i >= 0 {
				fn(i)
			}
		})
	}
	play, err := mk("▶ Play", func(i int) {
		a.cmd(func(ctx context.Context) error { return a.cl.PlayIndex(ctx, i) })
	})
	if err != nil {
		return nil, err
	}
	play.setSize(76, 30)
	up, err := mk("↑ Up", func(i int) {
		if i > 0 && i < len(q.songs) {
			id := q.songs[i].VideoID
			a.cmd(func(ctx context.Context) error { return a.cl.Move(ctx, i, i-1, id) })
		}
	})
	if err != nil {
		return nil, err
	}
	up.setSize(64, 30)
	down, err := mk("↓ Down", func(i int) {
		if i >= 0 && i < len(q.songs)-1 {
			id := q.songs[i].VideoID
			a.cmd(func(ctx context.Context) error { return a.cl.Move(ctx, i, i+1, id) })
		}
	})
	if err != nil {
		return nil, err
	}
	down.setSize(76, 30)
	rm, err := mk("✕ Remove", func(i int) {
		if i >= 0 && i < len(q.songs) {
			id := q.songs[i].VideoID
			a.cmd(func(ctx context.Context) error { return a.cl.Remove(ctx, i, id) })
		}
	})
	if err != nil {
		return nil, err
	}
	rm.setSize(92, 30)
	return q, nil
}

// pinListHeight fixes a list box's height so walk's item-count-based
// height adjustment cannot push siblings off the window.
func pinListHeight(lb *walk.ListBox, h int) {
	lb.SetMinMaxSize(walk.Size{Width: 1, Height: h},
		walk.Size{Width: 100000, Height: h})
}

func (q *queuePage) applyState(st *client.State) {
	if st == nil {
		if q.sig != "nil" {
			q.sig = "nil"
			q.songs = nil
			if err := q.list.SetModel([]string{"Nothing playing"}); err != nil {
				debugLog("queue SetModel: " + err.Error())
			}
			pinListHeight(q.list, queueListH)
		}
		return
	}
	var b strings.Builder
	for _, s := range st.Queue {
		b.WriteString(s.VideoID)
		b.WriteByte(',')
	}
	fmt.Fprintf(&b, "|%d", st.Index)
	sig := b.String()
	if sig == q.sig {
		return
	}
	q.sig = sig
	q.songs = st.Queue
	items := make([]string, len(st.Queue))
	for i, s := range st.Queue {
		prefix := "    "
		if i == st.Index {
			prefix = "▶ "
		}
		line := prefix + s.Title + " — " + s.Artist
		if s.DurationText != nil && *s.DurationText != "" {
			line += " · " + *s.DurationText
		}
		if s.QueuedBy != "" {
			line += " · " + s.QueuedBy
		}
		items[i] = line
	}
	if len(items) == 0 {
		items = []string{"Queue is empty"}
	}
	if err := q.list.SetModel(items); err != nil {
		debugLog("queue SetModel: " + err.Error())
	}
	if st.Index >= 0 && st.Index < len(items) {
		q.list.SetCurrentIndex(st.Index)
	}
	pinListHeight(q.list, queueListH)
}

// ── lyrics page ─────────────────────────────────────────────────────

type lyricsPage struct {
	a          *app
	box        *walk.Composite
	status     *walk.Label
	list       *walk.ListBox
	lyrics     *client.Lyrics
	loadedFor  string
	loadingFor string
	curLine    int
}

func (a *app) buildLyricsPage(parent walk.Container) (*lyricsPage, error) {
	p := &lyricsPage{a: a, curLine: -1}
	box, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	p.box = box
	lay := walk.NewVBoxLayout()
	lay.SetMargins(walk.Margins{})
	lay.SetSpacing(6)
	box.SetLayout(lay)
	p.status, err = walk.NewLabel(box)
	if err != nil {
		return nil, err
	}
	p.status.SetText("Lyrics appear here when a song plays.")
	p.status.SetFont(fontSub)
	p.status.SetTextColor(colMuted)
	p.status.SetMinMaxSize(walk.Size{Width: 1, Height: 18},
		walk.Size{Width: 100000, Height: 18})
	p.list, err = walk.NewListBox(box)
	if err != nil {
		return nil, err
	}
	p.list.SetFont(fontList)
	p.list.SetBackground(brush(colPanel))
	pinListHeight(p.list, lyricsListH)
	p.list.ItemActivated().Attach(func() {
		if p.lyrics == nil {
			return
		}
		i := p.list.CurrentIndex()
		if i >= 0 && i < len(p.lyrics.Lines) {
			ms := p.lyrics.Lines[i].At
			a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, ms) })
		}
	})
	return p, nil
}

func (p *lyricsPage) showMessage(msg string) {
	p.lyrics = nil
	p.loadedFor = ""
	p.curLine = -1
	p.status.SetText(msg)
	_ = p.list.SetModel([]string{})
	pinListHeight(p.list, lyricsListH)
}

func (p *lyricsPage) setLyrics(ly *client.Lyrics) {
	p.lyrics = ly
	p.loadedFor = ly.VideoID
	p.curLine = -1
	if !ly.Found {
		p.status.SetText("No lyrics found for this song.")
		_ = p.list.SetModel([]string{})
		pinListHeight(p.list, lyricsListH)
		return
	}
	src := ly.Source
	if src == "" {
		src = "lyrics"
	}
	if ly.Synced {
		src += " · synced — double-click a line to jump"
	} else {
		src += " · not synced"
	}
	p.status.SetText(src)
	items := make([]string, len(ly.Lines))
	for i, l := range ly.Lines {
		items[i] = l.Text
	}
	if err := p.list.SetModel(items); err != nil {
		debugLog("lyrics SetModel: " + err.Error())
	}
	pinListHeight(p.list, lyricsListH)
}

func (p *lyricsPage) highlight(posMs int64) {
	if p.lyrics == nil || !p.lyrics.Synced || len(p.lyrics.Lines) == 0 {
		return
	}
	idx := -1
	for i, l := range p.lyrics.Lines {
		if l.At <= posMs {
			idx = i
		} else {
			break
		}
	}
	if idx >= 0 && idx != p.curLine {
		p.curLine = idx
		p.list.SetCurrentIndex(idx)
	}
}

// ── settings page ───────────────────────────────────────────────────

type settingsPage struct {
	a       *app
	box     *walk.Composite
	urlEd   *walk.LineEdit
	tokEd   *walk.LineEdit
	pinEd   *walk.LineEdit
	nameEd  *walk.LineEdit
	autoTgl *chip
	progTgl *chip
	ctlTgl  *chip
	delayEd *walk.LineEdit
	autoOn  bool
	progOn  bool
	ctlOn   bool
	status  *walk.Label
}

func (a *app) buildSettingsPage(parent walk.Container) (*settingsPage, error) {
	p := &settingsPage{a: a}
	box, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	p.box = box
	lay := walk.NewVBoxLayout()
	lay.SetMargins(walk.Margins{HNear: 2, HFar: 2})
	lay.SetSpacing(6)
	box.SetLayout(lay)
	cfg := a.cl.Config()

	field := func(label string, val string, password bool) (*walk.LineEdit, error) {
		row, err := walk.NewComposite(box)
		if err != nil {
			return nil, err
		}
		rl := walk.NewHBoxLayout()
		rl.SetMargins(walk.Margins{})
		rl.SetSpacing(8)
		row.SetLayout(rl)
		lbl, err := walk.NewLabel(row)
		if err != nil {
			return nil, err
		}
		lbl.SetText(label)
		lbl.SetFont(fontSub)
		lbl.SetTextColor(colMuted)
		lbl.SetMinMaxSize(walk.Size{Width: 168, Height: 27},
			walk.Size{Width: 168, Height: 27})
		ed, err := walk.NewLineEdit(row)
		if err != nil {
			return nil, err
		}
		ed.SetFont(fontForm)
		ed.SetText(val)
		ed.SetMinMaxSize(walk.Size{Width: 1, Height: 27},
			walk.Size{Width: 10000, Height: 27})
		if password {
			ed.SetPasswordMode(true)
		}
		return ed, nil
	}
	if p.urlEd, err = field("Relay URL (or phone LAN)", cfg.BaseURL, false); err != nil {
		return nil, err
	}
	if p.tokEd, err = field("Relay token (blank = LAN)", cfg.RelayToken, false); err != nil {
		return nil, err
	}
	if p.pinEd, err = field("Remote PIN", cfg.PIN, true); err != nil {
		return nil, err
	}
	if p.nameEd, err = field("Your name (on the phone)", cfg.Name, false); err != nil {
		return nil, err
	}

	saveRow, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	srl := walk.NewHBoxLayout()
	srl.SetMargins(walk.Margins{})
	srl.SetSpacing(0)
	saveRow.SetLayout(srl)
	save, err := a.newChip(saveRow, "Save & connect", fontTab, func() { p.save() })
	if err != nil {
		return nil, err
	}
	save.setSize(140, 30)
	if _, err = walk.NewComposite(saveRow); err != nil {
		return nil, err
	}

	p.status, err = walk.NewLabel(box)
	if err != nil {
		return nil, err
	}
	p.status.SetFont(fontSub)
	p.status.SetTextColor(colMuted)
	p.status.SetMinMaxSize(walk.Size{Width: 0, Height: 15},
		walk.Size{Width: 10000, Height: 15})

	// Notch behaviour.
	p.autoOn = cfg.AutoCollapseOn()
	p.progOn = cfg.PillProgressOn()
	p.ctlOn = cfg.PillControlsOn()
	autoRow, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	arl := walk.NewHBoxLayout()
	arl.SetMargins(walk.Margins{})
	arl.SetSpacing(6)
	autoRow.SetLayout(arl)
	if p.autoTgl, err = a.newChip(autoRow, "", fontTab, func() {
		p.autoOn = !p.autoOn
		p.refreshToggles()
	}); err != nil {
		return nil, err
	}
	p.autoTgl.toggle = true
	p.autoTgl.left = true
	p.autoTgl.setSize(300, 24)
	dLbl, err := walk.NewLabel(autoRow)
	if err != nil {
		return nil, err
	}
	dLbl.SetText("delay")
	dLbl.SetFont(fontSub)
	dLbl.SetTextColor(colMuted)
	p.delayEd, err = walk.NewLineEdit(autoRow)
	if err != nil {
		return nil, err
	}
	p.delayEd.SetFont(fontForm)
	delayTxt := "0.6"
	if cfg.CollapseDelayMs > 0 {
		delayTxt = strconv.FormatFloat(float64(cfg.CollapseDelayMs)/1000, 'f', -1, 64)
	}
	p.delayEd.SetText(delayTxt)
	p.delayEd.SetMinMaxSize(walk.Size{Width: 44, Height: 24},
		walk.Size{Width: 44, Height: 24})
	sLbl, err := walk.NewLabel(autoRow)
	if err != nil {
		return nil, err
	}
	sLbl.SetText("s")
	sLbl.SetFont(fontSub)
	sLbl.SetTextColor(colMuted)
	if p.progTgl, err = a.newChip(box, "", fontTab, func() {
		p.progOn = !p.progOn
		p.refreshToggles()
	}); err != nil {
		return nil, err
	}
	p.progTgl.toggle = true
	p.progTgl.left = true
	p.progTgl.setSize(330, 24)
	if p.ctlTgl, err = a.newChip(box, "", fontTab, func() {
		p.ctlOn = !p.ctlOn
		p.refreshToggles()
	}); err != nil {
		return nil, err
	}
	p.ctlTgl.toggle = true
	p.ctlTgl.left = true
	p.ctlTgl.setSize(330, 24)
	p.refreshToggles()

	// Trailing spacer absorbs slack so rows never stretch.
	spacer, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	_ = lay.SetStretchFactor(spacer, 1)
	return p, nil
}

func (p *settingsPage) refreshToggles() {
	set := func(c *chip, on bool, text string) {
		prefix := "○  "
		if on {
			prefix = "●  "
		}
		c.setText(prefix + text)
		c.setActive(on)
	}
	set(p.autoTgl, p.autoOn, "Auto-collapse on mouse leave")
	set(p.progTgl, p.progOn, "Pill: show the progress bar")
	set(p.ctlTgl, p.ctlOn, "Pill: show transport controls")
}

func (p *settingsPage) setStatus(s string) { p.status.SetText(s) }

func (p *settingsPage) save() {
	delayMs := 600
	if v, err := strconv.ParseFloat(strings.TrimSpace(p.delayEd.Text()), 64); err == nil && v >= 0 && v <= 5 {
		delayMs = int(v * 1000)
	}
	cfg := client.Config{
		BaseURL:         strings.TrimSpace(p.urlEd.Text()),
		RelayToken:      strings.TrimSpace(p.tokEd.Text()),
		PIN:             strings.TrimSpace(p.pinEd.Text()),
		Name:            strings.TrimSpace(p.nameEd.Text()),
		AutoCollapse:    &p.autoOn,
		PillProgress:    &p.progOn,
		PillControls:    &p.ctlOn,
		CollapseDelayMs: delayMs,
	}
	p.a.cl.SetConfig(cfg)
	if err := client.SaveConfig(p.a.cfgPath, cfg); err != nil {
		p.setStatus("Could not save settings: " + err.Error())
		return
	}
	p.setStatus("Saved — connecting…")
	p.a.onSettingsSaved()
	go p.a.pollOnce()
}
