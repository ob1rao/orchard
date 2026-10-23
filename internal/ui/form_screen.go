package ui

import (
	"context"

	"github.com/gdamore/tcell/v3"
)

type formField struct{ label, value, hint string }

// formScreen collects a few lines of text. Tab and the arrows move between
// fields; Enter submits from any of them.
type formScreen struct {
	title  string
	action string // what Enter does, for the footer
	fields []formField
	focus  int
	note   string
	err    string
	// submit returns an error to show, or "" once it has dealt with the
	// screen (usually by popping it).
	submit func(a *App, values []string) string
}

func (f *formScreen) typing() bool { return true }

func (f *formScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	field := &f.fields[f.focus]
	switch e.Key() {
	case tcell.KeyEscape:
		a.leaveScreen(ctx)
	case tcell.KeyTab, tcell.KeyDown:
		f.focus = (f.focus + 1) % len(f.fields)
	case tcell.KeyBacktab, tcell.KeyUp:
		f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
	case tcell.KeyEnter:
		values := make([]string, len(f.fields))
		for i, fl := range f.fields {
			values[i] = fl.value
		}
		f.err = f.submit(a, values)
	case tcell.KeyCtrlU:
		field.value = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if r := []rune(field.value); len(r) > 0 {
			field.value = string(r[:len(r)-1])
		}
	case tcell.KeyRune:
		if len(field.value) < 1024 {
			field.value += e.Str()
		}
	}
}

func (f *formScreen) mouse(a *App, ctx context.Context, e *tcell.EventMouse) {
	if rightClick(e) {
		a.leaveScreen(ctx)
		return
	}
	_, y := e.Position()
	if e.Buttons()&tcell.Button1 != 0 {
		if i := (y - 5) / 2; y >= 5 && i < len(f.fields) {
			f.focus = i
		}
	}
}

func (f *formScreen) draw(a *App, w, h int) {
	a.text(2, 2, w-4, f.title, base.Foreground(accent).Bold(true))
	labelWidth := 12
	for i, fl := range f.fields {
		y := 5 + 2*i
		style := base
		value := fl.value
		if i == f.focus {
			style = base.Foreground(accent).Bold(true)
			value += "▏"
		}
		a.text(4, y, labelWidth, fl.label, style)
		a.screen.FillArea(4+labelWidth, y, min(50, w-labelWidth-8), 1, ' ', base.Background(tcell.NewHexColor(0x1b2a3c)))
		a.text(5+labelWidth, y, min(48, w-labelWidth-10), value, base.Background(tcell.NewHexColor(0x1b2a3c)))
		if fl.hint != "" && w > labelWidth+60 {
			a.text(labelWidth+56, y, w-labelWidth-58, fl.hint, base.Foreground(muted))
		}
	}
	y := 6 + 2*len(f.fields)
	if f.note != "" {
		a.text(4, y, w-6, f.note, base.Foreground(muted))
	}
	if f.err != "" {
		a.text(4, y+2, w-6, f.err, base.Foreground(tcell.ColorYellow))
	}
	a.text(2, h-2, w-4, "Tab/↑↓ move between fields · Enter "+f.actionName()+" · Ctrl-U clear field · Esc cancel", base.Foreground(accent))
}

func (f *formScreen) actionName() string {
	if f.action == "" {
		return "save"
	}
	return f.action
}
