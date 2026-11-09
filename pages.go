package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"bitchord-notch/client"
)

// ── collapsed bar ───────────────────────────────────────────────────

type barWidgets struct {
	box   *walk.Composite
	title *walk.Label
	sub   *walk.Label
	play  *lblBtn
	time  *walk.Label
}

func (a *app) buildBar() (*barWidgets, error) {
	box, err := walk.NewComposite(a.mw)
	if err != nil {
		return nil, err
	}
	box.SetBackground(brush(colBG))
	lay := walk.NewHBoxLayout()
	lay.SetMargins(walk.Margins{HNear: 10, VNear: 6, HFar: 10, VFar: 6})
	lay.SetSpacing(6)
	box.SetLayout(lay)
	box.SetMinMaxSize(walk.Size{Width: notchW, Height: collapsedH},
		walk.Size{Width: notchW, Height: collapsedH})

	prev, err := newLblBtn(box, "⏮", fontBtn, func() { a.cmd(a.cl.Prev) })
	if err != nil {
		return nil, err
	}
	prev.setSize(40, 44)
	play, err := newLblBtn(box, "▶", fontBtn, func() { a.cmd(a.cl.Toggle) })
	if err != nil {
		return nil, err
	}
	play.setSize(46, 44)
	next, err := newLblBtn(box, "⏭", fontBtn, func() { a.cmd(a.cl.Next) })
	if err != nil {
		return nil, err
	}
	next.setSize(40, 44)

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
	title, err := walk.NewLabel(mid)
	if err != nil {
		return nil, err
	}
	title.SetText("BitChord Notch")
	title.SetFont(fontTitle)
	title.SetTextColor(colText)
	title.SetEllipsisMode(walk.EllipsisEnd)
	sub, err := walk.NewLabel(mid)
	if err != nil {
		return nil, err
	}
	sub.SetText("connecting…")
	sub.SetFont(fontSub)
	sub.SetTextColor(colDim)

	tim, err := walk.NewLabel(box)
	if err != nil {
		return nil, err
	}
	tim.SetFont(fontSub)
	tim.SetTextColor(colDim)
	tim.SetTextAlignment(walk.AlignFar)
	tim.SetMinMaxSize(walk.Size{Width: 88, Height: 20},
		walk.Size{Width: 88, Height: 20})

	chev, err := newLblBtn(box, "▾", fontBtn, func() { a.setExpanded(!a.expanded) })
	if err != nil {
		return nil, err
	}
	chev.setSize(36, 44)

	hwnd := uintptr(a.mw.Handle())
	dragOn(box, hwnd)
	dragOn(mid, hwnd)
	dragOn(title, hwnd)
	dragOn(sub, hwnd)
	return &barWidgets{box: box, title: title, sub: sub, play: play, time: tim}, nil
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

	// Tabs row.
	tabs, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	tl := walk.NewHBoxLayout()
	tl.SetMargins(walk.Margins{})
	tl.SetSpacing(6)
	tabs.SetLayout(tl)
	a.tabQueue, err = newLblBtn(tabs, "Queue", fontTab, func() { a.showTab("queue") })
	if err != nil {
		return nil, err
	}
	a.tabQueue.setSize(84, 34)
	a.tabLyr, err = newLblBtn(tabs, "Lyrics", fontTab, func() { a.showTab("lyrics") })
	if err != nil {
		return nil, err
	}
	a.tabLyr.setSize(84, 34)
	a.tabSet, err = newLblBtn(tabs, "Settings", fontTab, func() { a.showTab("settings") })
	if err != nil {
		return nil, err
	}
	a.tabSet.setSize(84, 34)
	spacer, err := walk.NewComposite(tabs)
	if err != nil {
		return nil, err
	}
	_ = spacer
	collapse, err := newLblBtn(tabs, "▴", fontBtn, func() { a.setExpanded(false) })
	if err != nil {
		return nil, err
	}
	collapse.setSize(36, 34)

	// Pages.
	a.pageBox, err = walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	pl := walk.NewVBoxLayout()
	pl.SetMargins(walk.Margins{})
	pl.SetSpacing(0)
	a.pageBox.SetLayout(pl)
	if a.queuePage, err = a.buildQueuePage(a.pageBox); err != nil {
		return nil, err
	}
	if a.lyrPage, err = a.buildLyricsPage(a.pageBox); err != nil {
		return nil, err
	}
	if a.setPage, err = a.buildSettingsPage(a.pageBox); err != nil {
		return nil, err
	}

	// Footer: seek bar then transport extras.
	seekRow, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	sl := walk.NewHBoxLayout()
	sl.SetMargins(walk.Margins{})
	sl.SetSpacing(6)
	seekRow.SetLayout(sl)
	back, err := newLblBtn(seekRow, "−10s", fontTab, func() {
		a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, a.dispPosMs-10_000) })
	})
	if err != nil {
		return nil, err
	}
	back.setSize(48, 26)
	a.progBar, err = walk.NewProgressBar(seekRow)
	if err != nil {
		return nil, err
	}
	a.progBar.SetRange(0, 1)
	a.progBar.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		a.mu.Lock()
		st := a.state
		a.mu.Unlock()
		if st == nil || st.DurationMs <= 0 {
			return
		}
		w := a.progBar.Size().Width
		if w <= 0 {
			return
		}
		ms := int64(float64(x) / float64(w) * float64(st.DurationMs))
		a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, ms) })
	})
	fwd, err := newLblBtn(seekRow, "+10s", fontTab, func() {
		a.cmd(func(ctx context.Context) error { return a.cl.Seek(ctx, a.dispPosMs+10_000) })
	})
	if err != nil {
		return nil, err
	}
	fwd.setSize(48, 26)

	ctlRow, err := walk.NewComposite(exp)
	if err != nil {
		return nil, err
	}
	cl2 := walk.NewHBoxLayout()
	cl2.SetMargins(walk.Margins{})
	cl2.SetSpacing(6)
	ctlRow.SetLayout(cl2)
	a.shufBtn, err = newLblBtn(ctlRow, "Shuffle", fontTab, func() {
		a.cmd(a.cl.ToggleShuffle)
	})
	if err != nil {
		return nil, err
	}
	a.shufBtn.setSize(104, 30)
	a.repBtn, err = newLblBtn(ctlRow, "Repeat: off", fontTab, func() {
		a.cmd(a.cl.CycleRepeat)
	})
	if err != nil {
		return nil, err
	}
	a.repBtn.setSize(104, 30)
	volLbl, err := walk.NewLabel(ctlRow)
	if err != nil {
		return nil, err
	}
	volLbl.SetText("Vol")
	volLbl.SetFont(fontTab)
	volLbl.SetTextColor(colDim)
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
	a     *app
	box   *walk.Composite
	list  *walk.ListBox
	songs []client.Song
	sig   string
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
	rl := walk.NewHBoxLayout()
	rl.SetMargins(walk.Margins{})
	rl.SetSpacing(6)
	row.SetLayout(rl)
	mk := func(text string, fn func(i int)) (*lblBtn, error) {
		return newLblBtn(row, text, fontTab, func() {
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

func (q *queuePage) applyState(st *client.State) {
	if st == nil {
		if q.sig != "nil" {
			q.sig = "nil"
			q.songs = nil
			if err := q.list.SetModel([]string{"Nothing playing"}); err != nil {
				debugLog("queue SetModel: " + err.Error())
			}
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
	p.status.SetTextColor(colDim)
	p.list, err = walk.NewListBox(box)
	if err != nil {
		return nil, err
	}
	p.list.SetFont(fontList)
	p.list.SetBackground(brush(colPanel))
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
}

func (p *lyricsPage) setLyrics(ly *client.Lyrics) {
	p.lyrics = ly
	p.loadedFor = ly.VideoID
	p.curLine = -1
	if !ly.Found {
		p.status.SetText("No lyrics found for this song.")
		_ = p.list.SetModel([]string{})
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
	a      *app
	box    *walk.Composite
	urlEd  *walk.LineEdit
	tokEd  *walk.LineEdit
	pinEd  *walk.LineEdit
	nameEd *walk.LineEdit
	status *walk.Label
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
	row := func(label string, val string, password bool) (*walk.LineEdit, error) {
		lbl, err := walk.NewLabel(box)
		if err != nil {
			return nil, err
		}
		lbl.SetText(label)
		lbl.SetFont(fontSub)
		lbl.SetTextColor(colDim)
		lbl.SetMinMaxSize(walk.Size{Width: 0, Height: 15},
			walk.Size{Width: 10000, Height: 15})
		ed, err := walk.NewLineEdit(box)
		if err != nil {
			return nil, err
		}
		ed.SetFont(fontForm)
		ed.SetText(val)
		ed.SetMinMaxSize(walk.Size{Width: 0, Height: 27},
			walk.Size{Width: 10000, Height: 27})
		if password {
			ed.SetPasswordMode(true)
		}
		return ed, nil
	}
	if p.urlEd, err = row("Relay URL (or phone LAN address)", cfg.BaseURL, false); err != nil {
		return nil, err
	}
	if p.tokEd, err = row("Relay token (blank for direct LAN mode)", cfg.RelayToken, false); err != nil {
		return nil, err
	}
	if p.pinEd, err = row("Remote PIN", cfg.PIN, true); err != nil {
		return nil, err
	}
	if p.nameEd, err = row("Your name (shown on the phone)", cfg.Name, false); err != nil {
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
	save, err := newLblBtn(saveRow, "Save & connect", fontTab, func() { p.save() })
	if err != nil {
		return nil, err
	}
	save.setSize(140, 32)
	rowSpacer, err := walk.NewComposite(saveRow)
	if err != nil {
		return nil, err
	}
	_ = srl.SetStretchFactor(rowSpacer, 1)
	p.status, err = walk.NewLabel(box)
	if err != nil {
		return nil, err
	}
	p.status.SetFont(fontSub)
	p.status.SetTextColor(colDim)
	p.status.SetMinMaxSize(walk.Size{Width: 0, Height: 15},
		walk.Size{Width: 10000, Height: 15})
	// The form is fixed-height content: a trailing spacer takes any
	// slack in the page instead of the layout stretching the fields.
	spacer, err := walk.NewComposite(box)
	if err != nil {
		return nil, err
	}
	_ = lay.SetStretchFactor(spacer, 1)
	return p, nil
}

func (p *settingsPage) setStatus(s string) { p.status.SetText(s) }

func (p *settingsPage) save() {
	cfg := client.Config{
		BaseURL:    strings.TrimSpace(p.urlEd.Text()),
		RelayToken: strings.TrimSpace(p.tokEd.Text()),
		PIN:        strings.TrimSpace(p.pinEd.Text()),
		Name:       strings.TrimSpace(p.nameEd.Text()),
	}
	p.a.cl.SetConfig(cfg)
	if err := client.SaveConfig(p.a.cfgPath, cfg); err != nil {
		p.setStatus("Could not save settings: " + err.Error())
		return
	}
	p.setStatus("Saved — connecting…")
	go p.a.pollOnce()
}
