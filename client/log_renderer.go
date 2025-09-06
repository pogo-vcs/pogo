package client

import (
	"math"
	"strings"

	"github.com/nulab/autog"
	"github.com/nulab/autog/graph"
	"github.com/pogo-vcs/pogo/colors"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/runedrawer"
)

const noDescription = "(no description)"

// RenderLog renders the log output from the server response
func RenderLog(response *protos.LogResponse, coloredOutput bool) string {
	if len(response.Changes) == 0 {
		return "(no changes)"
	}

	// Build a map of changes by ID and name
	changesById := make(map[int64]*protos.LogChange)
	changesByName := make(map[string]*protos.LogChange)
	for _, change := range response.Changes {
		changesById[change.Id] = change
		changesByName[change.Name] = change
	}

	// Build adjacency list
	var adjacencyList [][]string
	childrenSeen := make(map[string]bool)

	for _, relation := range response.Relations {
		adjacencyList = append(adjacencyList, []string{relation.ChildName, relation.ParentName})
		childrenSeen[relation.ChildName] = true
	}

	// Find current change name
	var head string
	if currentChange, ok := changesById[response.CheckedOutChangeId]; ok {
		head = currentChange.Name
	}

	// If no relations, just show the changes
	if len(adjacencyList) == 0 {
		var output strings.Builder
		for _, change := range response.Changes {
			if head == change.Name {
				output.WriteString("● ")
			} else {
				output.WriteString("○ ")
			}

			if coloredOutput {
				output.WriteString(colors.Magenta)
				output.WriteString(change.UniquePrefix)
				output.WriteString(colors.Reset)
				output.WriteString(colors.BrightBlack)
				output.WriteString(change.Name[len(change.UniquePrefix):])
				output.WriteString(colors.Reset)
			} else {
				output.WriteString(change.Name)
			}

			if change.Description != nil {
				output.WriteString("\n  ")
				output.WriteString(*change.Description)
			} else {
				output.WriteString("\n  ")
				if coloredOutput {
					output.WriteString(colors.Green)
					output.WriteString(noDescription)
					output.WriteString(colors.Reset)
				} else {
					output.WriteString(noDescription)
				}
			}
			output.WriteString("\n")
		}
		return output.String()
	}

	// Layout the graph
	src := graph.EdgeSlice(adjacencyList)
	layout := autog.Layout(
		src,
		autog.WithNodeFixedSize(0, 0),
		autog.WithOrdering(autog.OrderingWMedian),
		autog.WithPositioning(autog.PositioningBrandesKoepf),
		autog.WithEdgeRouting(autog.EdgeRoutingOrtho),
		autog.WithNodeVerticalSpacing(2),
		autog.WithNodeSpacing(4),
		autog.WithLayerSpacing(0),
	)

	drawer := runedrawer.New()

	// Draw edges
	for _, e := range layout.Edges {
		var spline runedrawer.Spline
		for _, p := range e.Points {
			spline = append(spline, runedrawer.Point{
				X: int(math.Round(p[0])),
				Y: int(math.Round(p[1])),
			})
		}
		drawer.DrawSpline(spline)
	}
	drawer.EncodeCorners()

	// Track minimum height for each change for description placement
	changeMinHeight := make(map[string]int)

	// Draw nodes
	for _, n := range layout.Nodes {
		x := int(math.Round(n.X))
		y := int(math.Round(n.Y))
		if prevY, ok := changeMinHeight[n.ID]; ok {
			changeMinHeight[n.ID] = min(prevY, y)
		} else {
			changeMinHeight[n.ID] = y
		}

		var (
			prefix string
			suffix string
			sign   string
		)
		if coloredOutput {
			prefix = colors.White
			suffix = colors.Reset
		}
		if n.ID == head {
			sign = "●"
		} else {
			sign = "○"
		}
		drawer.WriteX(x, y, prefix, sign, suffix)
	}

	// Add change names and descriptions
	startX := drawer.Width() + 1

	for changeName, y := range changeMinHeight {
		if changeName == "~" {
			// Root node, render as ~
			if coloredOutput {
				drawer.WriteX(startX, y, colors.BrightBlack, "~", colors.Reset)
			} else {
				drawer.Write(startX, y, "~")
			}
		} else if change, ok := changesByName[changeName]; ok {
			// We have details for this change
			if coloredOutput {
				drawer.WriteX(startX, y, colors.Magenta, change.UniquePrefix, colors.Reset)
				drawer.WriteX(startX+len(change.UniquePrefix), y, colors.BrightBlack, changeName[len(change.UniquePrefix):], colors.Reset)
				if change.Description == nil {
					drawer.WriteX(startX, y+1, colors.Green, noDescription, colors.Reset)
				} else {
					drawer.Write(startX, y+1, *change.Description)
				}
			} else {
				drawer.Write(startX, y, changeName)
				if change.Description == nil {
					drawer.Write(startX, y+1, noDescription)
				} else {
					drawer.Write(startX, y+1, *change.Description)
				}
			}
		} else {
			// Change not in our list, render as ~ (unknown change)
			if coloredOutput {
				drawer.WriteX(startX, y, colors.BrightBlack, "~", colors.Reset)
			} else {
				drawer.Write(startX, y, "~")
			}
		}
	}

	return drawer.String()
}
