package client

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/nulab/autog"
	"github.com/nulab/autog/graph"
	"github.com/pogo-vcs/pogo/colors"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/runedrawer"
)

const noDescription = "(no description)"

const timeFormat = time.RFC3339

type LogData struct {
	Changes       []LogChangeData `json:"changes"`
	AdjacencyList [][]string      `json:"adjacency_list"`
}

type LogChangeData struct {
	Name          string    `json:"name"`
	UniquePrefix  string    `json:"unique_prefix"`
	UniqueSuffix  string    `json:"unique_suffix"`
	Description   *string   `json:"description"`
	ConflictFiles []string  `json:"conflict_files"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	IsCheckedOut  bool      `json:"is_checked_out"`
	Bookmarks     []string  `json:"bookmarks"`
	X             int       `json:"x"`
	Y             int       `json:"y"`
}

// ExtractLogData extracts structured data from the log response
func ExtractLogData(response *protos.LogResponse) *LogData {
	if len(response.Changes) == 0 {
		return &LogData{
			Changes:       []LogChangeData{},
			AdjacencyList: [][]string{},
		}
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
	for _, relation := range response.Relations {
		adjacencyList = append(adjacencyList, []string{relation.ChildName, relation.ParentName})
	}

	// Find current change name
	var head string
	if currentChange, ok := changesById[response.CheckedOutChangeId]; ok {
		head = currentChange.Name
	}

	var changes []LogChangeData

	// Calculate positioning using graph layout if we have relations
	if len(adjacencyList) > 0 {
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

		// Create changes array based on ALL nodes from layout (including placeholders)
		for _, n := range layout.Nodes {
			if protoChange, ok := changesByName[n.ID]; ok {
				// Real change from server
				createdAt, _ := time.Parse(time.RFC3339, protoChange.CreatedAt)
				updatedAt, _ := time.Parse(time.RFC3339, protoChange.UpdatedAt)

				changes = append(changes, LogChangeData{
					Name:          protoChange.Name,
					UniquePrefix:  protoChange.UniquePrefix,
					UniqueSuffix:  protoChange.Name[len(protoChange.UniquePrefix):],
					Description:   protoChange.Description,
					ConflictFiles: protoChange.ConflictFiles,
					CreatedAt:     createdAt,
					UpdatedAt:     updatedAt,
					IsCheckedOut:  protoChange.Name == head,
					X:             int(math.Round(n.X)),
					Y:             int(math.Round(n.Y)),
				})
			} else {
				// Placeholder node (like "~")
				changes = append(changes, LogChangeData{
					Name: n.ID,
					X:    int(math.Round(n.X)),
					Y:    int(math.Round(n.Y)),
				})
			}
		}
	} else {
		// No relations, create simple vertical layout
		changes = make([]LogChangeData, len(response.Changes))
		for i, change := range response.Changes {
			createdAt, _ := time.Parse(time.RFC3339, change.CreatedAt)
			updatedAt, _ := time.Parse(time.RFC3339, change.UpdatedAt)

			changes[i] = LogChangeData{
				Name:          change.Name,
				UniquePrefix:  change.UniquePrefix,
				UniqueSuffix:  change.Name[len(change.UniquePrefix):],
				Description:   change.Description,
				ConflictFiles: change.ConflictFiles,
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
				IsCheckedOut:  change.Name == head,
				X:             0,
				Y:             i * 3,
			}
		}
	}

	return &LogData{
		Changes:       changes,
		AdjacencyList: adjacencyList,
	}
}

// FindChangeByPrefix finds a change by name prefix (fuzzy match)
func (data *LogData) FindChangeByPrefix(prefix string) *LogChangeData {
	for i := range data.Changes {
		if strings.HasPrefix(data.Changes[i].Name, prefix) {
			return &data.Changes[i]
		}
	}
	return nil
}

// FindDescendants returns all descendants of a change (children, grandchildren, etc.)
// The adjacency list format is [childName, parentName] pairs
func (data *LogData) FindDescendants(changeName string) []LogChangeData {
	// Build parent -> children map
	childrenOf := make(map[string][]string)
	for _, edge := range data.AdjacencyList {
		childName := edge[0]
		parentName := edge[1]
		childrenOf[parentName] = append(childrenOf[parentName], childName)
	}

	// Build name -> change map for lookup
	changesByName := make(map[string]*LogChangeData)
	for i := range data.Changes {
		changesByName[data.Changes[i].Name] = &data.Changes[i]
	}

	// BFS to find all descendants
	var descendants []LogChangeData
	visited := make(map[string]bool)
	queue := childrenOf[changeName]

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		if change, ok := changesByName[current]; ok {
			descendants = append(descendants, *change)
		}

		// Add children of current to queue
		queue = append(queue, childrenOf[current]...)
	}

	return descendants
}

// RenderLogAsJSON renders the log output as JSON
func RenderLogAsJSON(response *protos.LogResponse) (string, error) {
	data := ExtractLogData(response)
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// RenderLog renders the log output from the server response
func RenderLog(response *protos.LogResponse, coloredOutput bool) string {
	data := ExtractLogData(response)

	if len(data.Changes) == 0 {
		return "(no changes)"
	}

	// Build a map of changes by name for quick lookup
	changesByName := make(map[string]*LogChangeData)
	for i := range data.Changes {
		data.Changes[i].CreatedAt = data.Changes[i].CreatedAt.Local()
		data.Changes[i].UpdatedAt = data.Changes[i].UpdatedAt.Local()
		changesByName[data.Changes[i].Name] = &data.Changes[i]
	}

	// If no relations, just show the changes
	if len(data.AdjacencyList) == 0 {
		var output strings.Builder
		for _, change := range data.Changes {
			if change.IsCheckedOut {
				output.WriteString("● ")
			} else {
				output.WriteString("○ ")
			}

			if coloredOutput {
				output.WriteString(colors.Magenta)
				output.WriteString(change.UniquePrefix)
				output.WriteString(colors.Reset)
				output.WriteString(colors.BrightBlack)
				output.WriteString(change.UniqueSuffix)
				output.WriteString(colors.Reset)
			} else {
				output.WriteString(change.Name)
			}
			if len(change.ConflictFiles) > 0 {
				output.WriteString(" 💥")
			}

			// Add modification time on the same line
			output.WriteString(" ")
			if coloredOutput {
				output.WriteString(colors.BrightBlack)
				output.WriteString(change.UpdatedAt.Format(timeFormat))
				output.WriteString(colors.Reset)
			} else {
				output.WriteString(change.UpdatedAt.Format(timeFormat))
			}
			if len(change.Bookmarks) > 0 {
				output.WriteString(" ")
				for _, bookmark := range change.Bookmarks {
					output.WriteString(" ")
					if coloredOutput {
						output.WriteString(colors.BrightBlack)
						output.WriteString(bookmark)
						output.WriteString(colors.Reset)
					} else {
						output.WriteString(bookmark)
					}
				}
			}
			output.WriteString(" ")

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

	drawer := runedrawer.New()

	// Draw edges using adjacency list and node positions
	changePositions := make(map[string][2]int)
	for _, change := range data.Changes {
		changePositions[change.Name] = [2]int{change.X, change.Y}
	}

	for _, edge := range data.AdjacencyList {
		childName := edge[0]
		parentName := edge[1]

		if childPos, ok := changePositions[childName]; ok {
			if parentPos, ok := changePositions[parentName]; ok {
				spline := runedrawer.Spline{
					{X: childPos[0], Y: childPos[1]},
					{X: parentPos[0], Y: parentPos[1]},
				}
				drawer.DrawSpline(spline)
			}
		}
	}
	drawer.EncodeCorners()

	// Track minimum height for each change for description placement
	changeMinHeight := make(map[string]int)

	// Draw nodes using data from ExtractLogData
	for _, change := range data.Changes {
		x := change.X
		y := change.Y
		if prevY, ok := changeMinHeight[change.Name]; ok {
			changeMinHeight[change.Name] = min(prevY, y)
		} else {
			changeMinHeight[change.Name] = y
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

		if change.IsCheckedOut {
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
				drawer.WriteX(startX+len(change.UniquePrefix), y, colors.BrightBlack, change.UniqueSuffix, colors.Reset)
				// conflict state
				conflictSize := 0
				if len(change.ConflictFiles) > 0 {
					conflictSize = 2
					drawer.Write(startX+len(change.Name)+1, y, "💥")
				}
				// Add modification time on the same line
				drawer.WriteX(startX+len(change.Name)+1+conflictSize, y, colors.BrightBlack, change.UpdatedAt.Format(timeFormat), colors.Reset)
				if change.Description == nil {
					drawer.WriteX(startX, y+1, colors.Green, noDescription, colors.Reset)
				} else {
					drawer.Write(startX, y+1, *change.Description)
				}
			} else {
				drawer.Write(startX, y, changeName)
				// conflict state
				conflictSize := 0
				if len(change.ConflictFiles) > 0 {
					conflictSize = 2
					drawer.Write(startX+len(change.Name)+1, y, "💥")
				}
				// Add modification time on the same line
				drawer.Write(startX+len(change.Name)+1+conflictSize, y, change.UpdatedAt.Format(timeFormat))
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
