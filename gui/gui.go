//go:build !nogui

package gui

import (
	"context"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/client/difftui"
	"github.com/pogo-vcs/pogo/protos"
)

const (
	nodeRadius   = 8
	graphPadding = 20
	nodeSpacingX = 40
	nodeSpacingY = 50
)

// Split holds the state for a resizable divider between two panels
type Split struct {
	// Ratio is the proportion of available width for the left panel (0.0 to 1.0)
	Ratio float32
	// Bar is the width of the resize handle
	Bar unit.Dp

	// Internal state for dragging
	drag   bool
	dragID pointer.ID
	dragX  float32
}

type App struct {
	client *client.Client
	theme  *material.Theme

	// Data
	logData        *client.LogData
	diffData       *difftui.DiffData
	selectedFile   int
	selectedChange int

	// Graph bounds (calculated from node positions)
	graphMinX, graphMaxX int
	graphMinY, graphMaxY int

	// Resizable splits
	leftSplit  Split // Between left sidebar and middle sidebar
	rightSplit Split // Between middle sidebar and main content

	// Widgets
	graphScrollState widget.List
	fileListState    widget.List
	diffScrollState  widget.List
	fileClickables   []widget.Clickable
	nodeClicks       []gesture.Click
}

func New(c *client.Client) *App {
	th := material.NewTheme()
	th.Palette.Bg = Base
	th.Palette.Fg = Text
	th.Palette.ContrastBg = Surface1
	th.Palette.ContrastFg = Text

	return &App{
		client:         c,
		theme:          th,
		selectedChange: -1,
		selectedFile:   -1,
		leftSplit: Split{
			Ratio: 0.23, // ~280px out of 1200px default width
			Bar:   unit.Dp(8),
		},
		rightSplit: Split{
			Ratio: 0.30, // ~220px out of remaining space
			Bar:   unit.Dp(8),
		},
	}
}

func (a *App) Run() error {
	// Load initial log data
	logData, err := a.client.GetLogData(50)
	if err != nil {
		return err
	}
	a.logData = logData
	a.calculateGraphBounds()
	a.nodeClicks = make([]gesture.Click, len(logData.Changes))

	// Find the currently checked out change and select it
	for i, change := range a.logData.Changes {
		if change.IsCheckedOut {
			a.selectedChange = i
			break
		}
	}

	// Load diff for selected change
	if a.selectedChange >= 0 {
		if err := a.loadDiffForChange(a.selectedChange); err != nil {
			// Ignore error, just leave diff empty
			a.diffData = nil
		}
	}

	// Window event loop must run in a goroutine, app.Main() blocks on main thread
	go func() {
		w := new(app.Window)
		w.Option(app.Title("Pogo"))
		w.Option(app.Size(unit.Dp(1200), unit.Dp(800)))

		if err := a.loop(w); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()

	// app.Main() must be called on the main goroutine - it blocks until all windows close
	app.Main()

	return nil
}

func (a *App) calculateGraphBounds() {
	if a.logData == nil || len(a.logData.Changes) == 0 {
		return
	}

	a.graphMinX = math.MaxInt
	a.graphMinY = math.MaxInt
	a.graphMaxX = math.MinInt
	a.graphMaxY = math.MinInt

	for _, change := range a.logData.Changes {
		if change.X < a.graphMinX {
			a.graphMinX = change.X
		}
		if change.X > a.graphMaxX {
			a.graphMaxX = change.X
		}
		if change.Y < a.graphMinY {
			a.graphMinY = change.Y
		}
		if change.Y > a.graphMaxY {
			a.graphMaxY = change.Y
		}
	}
}

func (a *App) loadDiffForChange(idx int) error {
	if idx < 0 || idx >= len(a.logData.Changes) {
		return nil
	}

	change := a.logData.Changes[idx]
	changeName := change.Name

	// Compare selected change with its parent (diff shows what this change added)
	diffData, err := a.client.CollectDiff(&changeName, nil, true, false)
	if err != nil {
		return err
	}

	a.diffData = &diffData
	a.fileClickables = make([]widget.Clickable, len(diffData.Files))
	a.selectedFile = -1

	return nil
}

func (a *App) loop(w *app.Window) error {
	var ops op.Ops

	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			// Handle file clicks
			for i := range a.fileClickables {
				if a.fileClickables[i].Clicked(gtx) {
					a.selectedFile = i
				}
			}

			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// Layout draws the resize handle at the given position and handles drag events.
// It returns the new X position after the handle.
func (s *Split) Layout(gtx layout.Context, xPos, height int) int {
	barWidth := gtx.Dp(s.Bar)
	if barWidth < 1 {
		barWidth = gtx.Dp(8)
	}

	// Define the hit area for the handle
	handleRect := image.Rect(xPos, 0, xPos+barWidth, height)

	// Register input events on the handle area
	stack := clip.Rect(handleRect).Push(gtx.Ops)

	// Set the cursor to resize icon when hovering/dragging
	pointer.CursorColResize.Add(gtx.Ops)

	// Declare that we are interested in pointer events
	event.Op(gtx.Ops, s)

	// Process events
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: s,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			break
		}

		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}

		switch e.Kind {
		case pointer.Press:
			if !s.drag {
				s.dragID = e.PointerID
				s.dragX = e.Position.X
				s.drag = true
			}
		case pointer.Drag:
			if s.drag && s.dragID == e.PointerID {
				// Calculate how much we moved
				deltaX := e.Position.X - s.dragX
				s.dragX = e.Position.X

				// Convert pixel delta to ratio delta
				totalWidth := float32(gtx.Constraints.Max.X)
				if totalWidth > 0 {
					deltaRatio := deltaX / totalWidth
					s.Ratio += deltaRatio

					// Clamp ratio to prevent panels from collapsing
					if s.Ratio < 0.1 {
						s.Ratio = 0.1
					}
					if s.Ratio > 0.8 {
						s.Ratio = 0.8
					}
				}
			}
		case pointer.Release, pointer.Cancel:
			if s.drag && s.dragID == e.PointerID {
				s.drag = false
			}
		}
	}
	stack.Pop()

	// Draw the handle visual
	paint.FillShape(gtx.Ops, Surface2, clip.Rect(handleRect).Op())

	return xPos + barWidth
}

func (a *App) layout(gtx layout.Context) layout.Dimensions {
	// Fill background
	paint.Fill(gtx.Ops, Base)

	totalWidth := gtx.Constraints.Max.X
	totalHeight := gtx.Constraints.Max.Y

	// Get handle widths
	leftBarWidth := gtx.Dp(a.leftSplit.Bar)
	rightBarWidth := gtx.Dp(a.rightSplit.Bar)

	// Calculate widths based on ratios
	// leftSplit.Ratio determines the left sidebar width as a proportion of total width
	leftSidebarWidth := int(a.leftSplit.Ratio * float32(totalWidth))

	// rightSplit.Ratio determines the middle sidebar width as a proportion of remaining space
	remainingAfterLeft := totalWidth - leftSidebarWidth - leftBarWidth
	middleSidebarWidth := int(a.rightSplit.Ratio * float32(remainingAfterLeft))

	// Main content takes the rest
	mainWidth := remainingAfterLeft - middleSidebarWidth - rightBarWidth

	// Ensure minimum widths
	minWidth := 100
	if leftSidebarWidth < minWidth {
		leftSidebarWidth = minWidth
	}
	if middleSidebarWidth < minWidth {
		middleSidebarWidth = minWidth
	}
	if mainWidth < minWidth {
		mainWidth = minWidth
	}

	// Track current X position
	xPos := 0

	// LEFT SIDEBAR (Changes)
	func() {
		// Clip to sidebar area
		area := clip.Rect{Max: image.Pt(leftSidebarWidth, totalHeight)}.Push(gtx.Ops)
		defer area.Pop()

		// Background
		paint.FillShape(gtx.Ops, Mantle, clip.Rect{Max: image.Pt(leftSidebarWidth, totalHeight)}.Op())

		// Content
		sidebarGtx := gtx
		sidebarGtx.Constraints = layout.Exact(image.Pt(leftSidebarWidth, totalHeight))
		a.layoutChangeSidebar(sidebarGtx, leftSidebarWidth)
	}()
	xPos += leftSidebarWidth

	// HANDLE 1 (between left sidebar and middle sidebar)
	xPos = a.leftSplit.Layout(gtx, xPos, totalHeight)

	// MIDDLE SIDEBAR (Files)
	func() {
		offset := op.Offset(image.Pt(xPos, 0)).Push(gtx.Ops)
		defer offset.Pop()
		area := clip.Rect{Max: image.Pt(middleSidebarWidth, totalHeight)}.Push(gtx.Ops)
		defer area.Pop()

		// Background
		paint.FillShape(gtx.Ops, Mantle, clip.Rect{Max: image.Pt(middleSidebarWidth, totalHeight)}.Op())

		// Content
		sidebarGtx := gtx
		sidebarGtx.Constraints = layout.Exact(image.Pt(middleSidebarWidth, totalHeight))
		a.layoutFileSidebar(sidebarGtx, middleSidebarWidth)
	}()
	xPos += middleSidebarWidth

	// HANDLE 2 (between middle sidebar and main content)
	xPos = a.rightSplit.Layout(gtx, xPos, totalHeight)

	// MAIN CONTENT (Diff view)
	func() {
		offset := op.Offset(image.Pt(xPos, 0)).Push(gtx.Ops)
		defer offset.Pop()
		area := clip.Rect{Max: image.Pt(mainWidth, totalHeight)}.Push(gtx.Ops)
		defer area.Pop()

		mainGtx := gtx
		mainGtx.Constraints = layout.Exact(image.Pt(mainWidth, totalHeight))
		a.layoutDiffView(mainGtx)
	}()

	return layout.Dimensions{Size: image.Pt(totalWidth, totalHeight)}
}

func (a *App) layoutSeparator(gtx layout.Context) layout.Dimensions {
	size := image.Point{X: gtx.Dp(1), Y: gtx.Constraints.Max.Y}
	rect := clip.Rect{Max: size}.Op()
	paint.FillShape(gtx.Ops, Surface1, rect)
	return layout.Dimensions{Size: size}
}

func (a *App) layoutChangeSidebar(gtx layout.Context, width int) layout.Dimensions {
	// Fill sidebar background
	paint.FillShape(gtx.Ops, Mantle, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutSidebarHeader(gtx, "Changes")
		}),
		// Change list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutChangeGraph(gtx)
		}),
	)
}

func (a *App) layoutFileSidebar(gtx layout.Context, width int) layout.Dimensions {
	// Fill sidebar background
	paint.FillShape(gtx.Ops, Mantle, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutSidebarHeader(gtx, "Files")
		}),
		// List
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutFileList(gtx)
		}),
	)
}

func (a *App) layoutSidebarHeader(gtx layout.Context, title string) layout.Dimensions {
	// Background
	size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(36)}
	rect := clip.Rect{Max: size}.Op()
	paint.FillShape(gtx.Ops, Mantle, rect)

	return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(a.theme, unit.Sp(14), title)
		lbl.Color = Subtext0
		lbl.Font.Weight = font.Bold
		return lbl.Layout(gtx)
	})
}

// Transform graph coordinates to screen coordinates
func (a *App) graphToScreen(graphX, graphY int, availableWidth int) (screenX, screenY float32) {
	// X: position based on lane
	graphWidth := a.graphMaxX - a.graphMinX
	if graphWidth == 0 {
		graphWidth = 1
	}

	// Position nodes based on their lane
	xOffset := float32(graphPadding)
	if graphWidth > 0 {
		laneWidth := float32(nodeSpacingX)
		xOffset = float32(graphPadding) + float32(graphX-a.graphMinX)*laneWidth
	}
	screenX = xOffset + float32(nodeRadius)

	// Y: linear mapping with fixed spacing
	screenY = float32(graphPadding) + float32(graphY-a.graphMinY)*float32(nodeSpacingY)/2

	return screenX, screenY
}

// calculateDescriptionX calculates the X position where all descriptions should start
// to ensure they are horizontally aligned
func (a *App) calculateDescriptionX(availableWidth int) float32 {
	if a.logData == nil || len(a.logData.Changes) == 0 {
		return float32(graphPadding)
	}

	// Average character width at unit.Sp(11) font size (approximate)
	const charWidth = 7.0
	const labelPadding = 20.0 // Padding between name and description

	var maxExtent float32 = 0

	for _, change := range a.logData.Changes {
		// Skip placeholder nodes
		if change.UniquePrefix == "" && change.Name == "~" {
			continue
		}

		// Get the node's screen X position
		nodeX, _ := a.graphToScreen(change.X, change.Y, availableWidth)

		// Calculate label start position (same as in drawNodeLabel)
		labelX := nodeX + float32(nodeRadius) + 6

		// Estimate label width based on character count
		nameLen := len(change.UniquePrefix) + len(change.UniqueSuffix)
		labelWidth := float32(nameLen) * charWidth

		// Calculate where this label ends
		labelEnd := labelX + labelWidth

		if labelEnd > maxExtent {
			maxExtent = labelEnd
		}
	}

	return maxExtent + labelPadding
}

func (a *App) layoutChangeGraph(gtx layout.Context) layout.Dimensions {
	if a.logData == nil || len(a.logData.Changes) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "No changes")
			lbl.Color = Overlay0
			return lbl.Layout(gtx)
		})
	}

	// Calculate graph dimensions
	graphHeight := graphPadding*2 + (a.graphMaxY-a.graphMinY)*nodeSpacingY/2 + 50
	if graphHeight < gtx.Constraints.Max.Y {
		graphHeight = gtx.Constraints.Max.Y
	}

	// Use scrollable list with single item containing the graph
	a.graphScrollState.Axis = layout.Vertical

	return material.List(a.theme, &a.graphScrollState).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints.Min.Y = graphHeight
		gtx.Constraints.Max.Y = graphHeight
		return a.drawGraphContent(gtx)
	})
}

func (a *App) drawGraphContent(gtx layout.Context) layout.Dimensions {
	availableWidth := gtx.Constraints.Max.X
	availableHeight := gtx.Constraints.Max.Y

	// Calculate aligned description X position
	descriptionX := a.calculateDescriptionX(availableWidth)

	// Build name -> change lookup
	changeByName := make(map[string]*client.LogChangeData)
	for i := range a.logData.Changes {
		changeByName[a.logData.Changes[i].Name] = &a.logData.Changes[i]
	}

	// Draw edges first (behind nodes)
	for _, edge := range a.logData.AdjacencyList {
		childName := edge[0]
		parentName := edge[1]

		childChange := changeByName[childName]
		parentChange := changeByName[parentName]

		if childChange != nil && parentChange != nil {
			x1, y1 := a.graphToScreen(childChange.X, childChange.Y, availableWidth)
			x2, y2 := a.graphToScreen(parentChange.X, parentChange.Y, availableWidth)
			a.drawEdge(gtx, x1, y1, x2, y2)
		}
	}

	// Draw nodes
	for i := range a.logData.Changes {
		change := &a.logData.Changes[i]
		isPlaceholder := change.UniquePrefix == "" && change.Name == "~"

		x, y := a.graphToScreen(change.X, change.Y, availableWidth)
		isSelected := i == a.selectedChange

		// Draw node circle
		a.drawNode(gtx, x, y, isSelected, change.IsCheckedOut, isPlaceholder)

		// Draw label for non-placeholder nodes
		if !isPlaceholder {
			a.drawNodeLabel(gtx, x+float32(nodeRadius)+6, y-6, *change, isSelected, descriptionX)
		}

		// Handle clicks
		if !isPlaceholder {
			nodeRect := image.Rect(
				int(x)-nodeRadius-2,
				int(y)-nodeRadius-2,
				int(x)+150,
				int(y)+nodeRadius+2,
			)
			area := clip.Rect(nodeRect).Push(gtx.Ops)
			a.nodeClicks[i].Add(gtx.Ops)
			for {
				e, ok := a.nodeClicks[i].Update(gtx.Source)
				if !ok {
					break
				}
				if e.Kind == gesture.KindClick {
					if a.selectedChange != i {
						a.selectedChange = i
						_ = a.loadDiffForChange(i)
					}
				}
			}
			area.Pop()
		}
	}

	return layout.Dimensions{Size: image.Pt(availableWidth, availableHeight)}
}

func (a *App) drawEdge(gtx layout.Context, x1, y1, x2, y2 float32) {
	// Draw a line from (x1, y1) to (x2, y2)
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(x1, y1))
	path.LineTo(f32.Pt(x2, y2))

	// Stroke the path
	paint.FillShape(gtx.Ops, Surface2, clip.Stroke{
		Path:  path.End(),
		Width: 2,
	}.Op())
}

func (a *App) drawNode(gtx layout.Context, x, y float32, selected, checkedOut, placeholder bool) {
	// Determine colors
	var fillColor, strokeColor color.NRGBA

	if placeholder {
		fillColor = Surface0
		strokeColor = Overlay0
	} else if selected {
		fillColor = Mauve
		strokeColor = Lavender
	} else if checkedOut {
		fillColor = Green
		strokeColor = Teal
	} else {
		fillColor = Surface1
		strokeColor = Overlay1
	}

	radius := float32(nodeRadius)
	ix, iy := int(x), int(y)
	ir := int(radius)

	// Draw filled circle using clip.Ellipse
	ellipse := clip.Ellipse{
		Min: image.Pt(ix-ir, iy-ir),
		Max: image.Pt(ix+ir, iy+ir),
	}

	// Fill
	paint.FillShape(gtx.Ops, fillColor, ellipse.Op(gtx.Ops))

	// Stroke using path
	var strokePath clip.Path
	strokePath.Begin(gtx.Ops)
	const segments = 32
	for i := 0; i <= segments; i++ {
		angle := float32(i) * 2 * math.Pi / segments
		px := x + radius*float32(math.Cos(float64(angle)))
		py := y + radius*float32(math.Sin(float64(angle)))
		if i == 0 {
			strokePath.MoveTo(f32.Pt(px, py))
		} else {
			strokePath.LineTo(f32.Pt(px, py))
		}
	}
	strokePath.Close()

	paint.FillShape(gtx.Ops, strokeColor, clip.Stroke{
		Path:  strokePath.End(),
		Width: 2,
	}.Op())
}

func (a *App) drawNodeLabel(gtx layout.Context, x, y float32, change client.LogChangeData, selected bool, descriptionX float32) {
	// Position the label
	offset := op.Offset(image.Pt(int(x), int(y)-8)).Push(gtx.Ops)

	// Change name colors
	prefixColor := Mauve
	suffixColor := Overlay0
	if selected {
		prefixColor = Lavender
		suffixColor = Subtext0
	}

	// Draw prefix
	lblPrefix := material.Label(a.theme, unit.Sp(11), change.UniquePrefix)
	lblPrefix.Color = prefixColor
	lblPrefix.Font.Weight = font.Medium
	prefixDims := lblPrefix.Layout(gtx)

	// Draw suffix next to prefix
	suffixOffset := op.Offset(image.Pt(prefixDims.Size.X, 0)).Push(gtx.Ops)
	lblSuffix := material.Label(a.theme, unit.Sp(11), change.UniqueSuffix)
	lblSuffix.Color = suffixColor
	lblSuffix.Layout(gtx)
	suffixOffset.Pop()

	offset.Pop()

	// Draw description at aligned position
	descOffset := op.Offset(image.Pt(int(descriptionX), int(y)-8)).Push(gtx.Ops)

	descText := "(no description)"
	if change.Description != nil && *change.Description != "" {
		descText = *change.Description
		// Only use the first line
		if idx := strings.IndexAny(descText, "\n\r"); idx != -1 {
			descText = descText[:idx]
		}
	}

	descColor := Overlay0
	if selected {
		descColor = Subtext0
	}

	lblDesc := material.Label(a.theme, unit.Sp(11), descText)
	lblDesc.Color = descColor
	lblDesc.MaxLines = 1
	lblDesc.Layout(gtx)

	descOffset.Pop()
}

func (a *App) layoutFileList(gtx layout.Context) layout.Dimensions {
	if a.diffData == nil || len(a.diffData.Files) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "No files")
			lbl.Color = Overlay0
			return lbl.Layout(gtx)
		})
	}

	a.fileListState.Axis = layout.Vertical

	return material.List(a.theme, &a.fileListState).Layout(gtx, len(a.diffData.Files), func(gtx layout.Context, i int) layout.Dimensions {
		file := a.diffData.Files[i]
		return a.fileClickables[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return a.layoutFileItem(gtx, file, i == a.selectedFile)
		})
	})
}

func (a *App) layoutFileItem(gtx layout.Context, file difftui.DiffFile, selected bool) layout.Dimensions {
	// Background for selected item
	if selected {
		size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(32)}
		rect := clip.Rect{Max: size}.Op()
		paint.FillShape(gtx.Ops, Surface0, rect)
	}

	return layout.Inset{
		Left:   unit.Dp(12),
		Right:  unit.Dp(12),
		Top:    unit.Dp(6),
		Bottom: unit.Dp(6),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			// Status indicator
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				var statusChar string
				var statusColor = Text
				switch file.Header.Status {
				case protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED:
					statusChar = "A"
					statusColor = Green
				case protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED:
					statusChar = "D"
					statusColor = Red
				case protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED:
					statusChar = "M"
					statusColor = Yellow
				case protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY:
					statusChar = "B"
					statusColor = Peach
				}
				lbl := material.Label(a.theme, unit.Sp(12), statusChar+" ")
				lbl.Color = statusColor
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			}),
			// File path
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(12), file.Header.Path)
				lbl.Color = Text
				return lbl.Layout(gtx)
			}),
		)
	})
}

func (a *App) layoutDiffView(gtx layout.Context) layout.Dimensions {
	// Background
	paint.Fill(gtx.Ops, Base)

	if a.diffData == nil || a.selectedFile < 0 || a.selectedFile >= len(a.diffData.Files) {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(14), "Select a file to view diff")
			lbl.Color = Overlay0
			return lbl.Layout(gtx)
		})
	}

	file := a.diffData.Files[a.selectedFile]

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutDiffHeader(gtx, file)
		}),
		// Diff content
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutDiffContent(gtx, file)
		}),
	)
}

func (a *App) layoutDiffHeader(gtx layout.Context, file difftui.DiffFile) layout.Dimensions {
	// Background
	size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(36)}
	rect := clip.Rect{Max: size}.Op()
	paint.FillShape(gtx.Ops, Mantle, rect)

	return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(a.theme, unit.Sp(13), file.Header.Path)
		lbl.Color = Text
		lbl.Font.Weight = font.Medium
		return lbl.Layout(gtx)
	})
}

func (a *App) layoutDiffContent(gtx layout.Context, file difftui.DiffFile) layout.Dimensions {
	// Collect all lines
	type diffLine struct {
		text     string
		lineType protos.DiffBlockType
	}
	var lines []diffLine

	for _, block := range file.Blocks {
		for _, line := range block.Lines {
			lines = append(lines, diffLine{
				text:     line,
				lineType: block.Type,
			})
		}
	}

	if len(lines) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "Binary file or no changes")
			lbl.Color = Overlay0
			return lbl.Layout(gtx)
		})
	}

	lexer := getLexerForFile(file.Header.Path)
	a.diffScrollState.Axis = layout.Vertical

	return material.List(a.theme, &a.diffScrollState).Layout(gtx, len(lines), func(gtx layout.Context, i int) layout.Dimensions {
		line := lines[i]
		return a.layoutDiffLine(gtx, line.text, line.lineType, lexer)
	})
}

func (a *App) layoutDiffLine(gtx layout.Context, lineText string, lineType protos.DiffBlockType, lexer chroma.Lexer) layout.Dimensions {
	// Determine background and prefix
	var bg = Base
	var prefix string

	switch lineType {
	case protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED:
		bg = DiffAddBg
		prefix = "+"
	case protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED:
		bg = DiffRemoveBg
		prefix = "-"
	case protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED:
		prefix = " "
	case protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA:
		bg = Mantle
		prefix = ""
	}

	// Background
	size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(20)}
	rect := clip.Rect{Max: size}.Op()
	paint.FillShape(gtx.Ops, bg, rect)

	return layout.Inset{
		Left:   unit.Dp(8),
		Right:  unit.Dp(8),
		Top:    unit.Dp(2),
		Bottom: unit.Dp(2),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Replace tabs with spaces
		displayText := strings.ReplaceAll(lineText, "\t", "    ")

		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			// Prefix
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(12), prefix)
				lbl.Font.Typeface = "monospace"
				switch lineType {
				case protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED:
					lbl.Color = Green
				case protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED:
					lbl.Color = Red
				default:
					lbl.Color = Overlay0
				}
				return lbl.Layout(gtx)
			}),
			// Content with syntax highlighting (no wrapping)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return a.layoutHighlightedLine(gtx, displayText, lexer)
			}),
		)
	})
}

func (a *App) layoutHighlightedLine(gtx layout.Context, lineText string, lexer chroma.Lexer) layout.Dimensions {
	// Tokenize the line
	iterator, err := lexer.Tokenise(nil, lineText)
	if err != nil {
		// Fallback to plain text (no wrapping)
		lbl := material.Label(a.theme, unit.Sp(12), lineText)
		lbl.Font.Typeface = "monospace"
		lbl.Color = Text
		lbl.MaxLines = 1
		return lbl.Layout(gtx)
	}

	// Render tokens
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.renderTokens(gtx, iterator)
		}),
	)
}

func (a *App) renderTokens(gtx layout.Context, iterator chroma.Iterator) layout.Dimensions {
	var dims layout.Dimensions

	for token := iterator(); token != chroma.EOF; token = iterator() {
		tokenColor := getTokenColor(token.Type)
		lbl := material.Label(a.theme, unit.Sp(12), token.Value)
		lbl.Font.Typeface = "monospace"
		lbl.Color = tokenColor
		lbl.MaxLines = 1

		// Offset by accumulated width
		offset := op.Offset(image.Point{X: dims.Size.X, Y: 0}).Push(gtx.Ops)
		tokenDims := lbl.Layout(gtx)
		offset.Pop()

		dims.Size.X += tokenDims.Size.X
		if tokenDims.Size.Y > dims.Size.Y {
			dims.Size.Y = tokenDims.Size.Y
		}
	}

	return dims
}

func getLexerForFile(path string) chroma.Lexer {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	return chroma.Coalesce(lexer)
}

func getTokenColor(tokenType chroma.TokenType) color.NRGBA {
	// Catppuccin Mocha syntax highlighting
	for tokenType != chroma.Background {
		switch tokenType {
		case chroma.Keyword, chroma.KeywordNamespace, chroma.KeywordConstant,
			chroma.KeywordPseudo, chroma.KeywordReserved:
			return Mauve
		case chroma.KeywordType:
			return Yellow
		case chroma.Name:
			return Text
		case chroma.NameClass, chroma.NameNamespace:
			return Peach
		case chroma.NameFunction, chroma.NameFunctionMagic:
			return Blue
		case chroma.NameBuiltin, chroma.NameBuiltinPseudo:
			return Red
		case chroma.NameVariable, chroma.NameVariableClass, chroma.NameVariableGlobal,
			chroma.NameVariableInstance, chroma.NameVariableMagic:
			return Text
		case chroma.NameConstant:
			return Peach
		case chroma.NameTag:
			return Mauve
		case chroma.NameAttribute:
			return Blue
		case chroma.LiteralString, chroma.LiteralStringAffix, chroma.LiteralStringBacktick,
			chroma.LiteralStringChar, chroma.LiteralStringDelimiter, chroma.LiteralStringDoc,
			chroma.LiteralStringDouble, chroma.LiteralStringEscape, chroma.LiteralStringHeredoc,
			chroma.LiteralStringInterpol, chroma.LiteralStringOther, chroma.LiteralStringRegex,
			chroma.LiteralStringSingle, chroma.LiteralStringSymbol:
			return Green
		case chroma.LiteralNumber, chroma.LiteralNumberBin, chroma.LiteralNumberFloat,
			chroma.LiteralNumberHex, chroma.LiteralNumberInteger, chroma.LiteralNumberIntegerLong,
			chroma.LiteralNumberOct:
			return Peach
		case chroma.Operator, chroma.OperatorWord:
			return Sky
		case chroma.Punctuation:
			return Overlay2
		case chroma.Comment, chroma.CommentSingle, chroma.CommentMultiline,
			chroma.CommentSpecial, chroma.CommentHashbang:
			return Overlay0
		case chroma.CommentPreproc, chroma.CommentPreprocFile:
			return Pink
		case chroma.Generic:
			return Text
		case chroma.GenericDeleted:
			return Red
		case chroma.GenericInserted:
			return Green
		case chroma.GenericHeading, chroma.GenericSubheading:
			return Peach
		case chroma.GenericEmph:
			return Text
		case chroma.GenericStrong:
			return Text
		}
		if tokenType == chroma.Text || tokenType == 0 {
			break
		}
		tokenType = tokenType.Parent()
	}
	return Text
}

// Run starts the GUI with the given client
func Run(ctx context.Context, c *client.Client) error {
	guiApp := New(c)
	return guiApp.Run()
}
