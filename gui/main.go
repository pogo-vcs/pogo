package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/pogo-vcs/pogo/client"
	"github.com/sqweek/dialog"
)

// Define our routes
type ViewID int

const (
	HomeView ViewID = iota
	RepositoryView
)

// Router manages state and navigation
type Router struct {
	theme      *material.Theme
	current    ViewID
	pogoClient *client.Client
	ctx        context.Context
	cancel     context.CancelFunc

	// Persistent state for widgets
	homeBtn  widget.Clickable
	openBtn  widget.Clickable
	repoBtns []*widget.Clickable
}

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("Pogo"), app.Size(unit.Dp(700), unit.Dp(600)))

		if err := run(w); err != nil {
			log.Fatal(err)
		}
		time.Sleep(time.Second)
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	theme := material.NewTheme()
	theme.Bg = Base
	theme.Fg = Text
	theme.ContrastBg = Surface1
	theme.ContrastFg = Text
	theme.Palette.Fg = Text
	theme.Palette.Bg = Base
	theme.Palette.ContrastBg = Surface1
	theme.Palette.ContrastFg = Text

	router := &Router{
		theme:   theme,
		current: HomeView,
	}
	defer router.Close()
	router.ctx, router.cancel = context.WithCancel(context.Background())
	defer router.Close()

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			// Logic: Check for navigation clicks
			if router.homeBtn.Clicked(gtx) {
				router.current = HomeView
			} else if router.openBtn.Clicked(gtx) {
				go func() {
					if repoDir, err := dialog.Directory().
						Title("Open Repository").
						SetStartDir(homeDir).
						Browse(); err == nil {
						router.openRecentRepository(repoDir)
					} else {
						_, _ = fmt.Fprintln(os.Stderr, err)
					}
				}()
			} else {
				for i := range router.repoBtns {
					if router.repoBtns[i].Clicked(gtx) {
						go router.openRecentRepository(preferences.RecentRepositories[i])
					}
				}
			}

			paint.FillShape(gtx.Ops, router.theme.Palette.Bg, clip.Rect{
				Max: gtx.Constraints.Max,
			}.Op())

			// Render the current view
			router.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (r *Router) Close() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.pogoClient != nil {
		r.pogoClient.Close()
		r.pogoClient = nil
	}
	r.ctx = nil
}

func (r *Router) openRecentRepository(s string) {
	if r.pogoClient != nil {
		r.pogoClient.Close()
		r.pogoClient = nil
	}
	var err error
	r.pogoClient, err = client.OpenFromFile(r.ctx, s)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		dialog.Message("Could not open repository %s: %s", s, err).Error()
		return
	}
	notifyOpenRepository(s)
	r.current = RepositoryView
}

// layout acts as the central switchboard
func (r *Router) layout(gtx layout.Context) layout.Dimensions {
	switch r.current {
	case RepositoryView:
		return r.drawRepository(gtx)
	default:
		return r.drawHome(gtx)
	}
}

func (r *Router) drawHome(gtx layout.Context) layout.Dimensions {
	recentRepos := make([]layout.FlexChild, 2*(1+len(preferences.RecentRepositories)))
	if len(r.repoBtns) != len(preferences.RecentRepositories) {
		r.repoBtns = make([]*widget.Clickable, len(preferences.RecentRepositories))
		for i := range r.repoBtns {
			r.repoBtns[i] = new(widget.Clickable)
		}
	}
	for i, repo := range preferences.RecentRepositories {
		display := repo
		if rel, ok := strings.CutPrefix(repo, homeDir); ok {
			display = "~" + rel
		}
		recentRepos[i*2] = layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout)
		recentRepos[i*2+1] = layout.Rigid(material.Button(r.theme, r.repoBtns[i], display).Layout)
	}
	recentRepos[len(recentRepos)-2] = layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout)
	recentRepos[len(recentRepos)-1] = layout.Rigid(material.Button(r.theme, &r.openBtn, "Open Repository").Layout)

	return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEvenly, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, material.H1(r.theme, "Pogo").Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEvenly, Alignment: layout.Middle}.Layout(gtx,
					recentRepos...,
				)
			})
		}),
	)
}

func (r *Router) drawRepository(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.Subtitle1(r.theme, r.pogoClient.Location).Layout),
	)
}
