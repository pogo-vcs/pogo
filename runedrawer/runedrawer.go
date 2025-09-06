package runedrawer

import (
	"slices"
	"strings"
)

func New() *RuneDrawer {
	return &RuneDrawer{}
}

type RuneDrawer struct {
	buf [][]string
}

type (
	Spline []Point
	Point  struct {
		X, Y int
	}
)

func (r *RuneDrawer) Write(x, y int, s string) {
	relativeX := 0
	relativeY := 0
	for _, char := range s {
		absoluteX := x + relativeX
		absoluteY := y + relativeY

		if char == '\n' {
			relativeX = 0
			relativeY++
			continue
		}

		r.ensureSize(absoluteX, absoluteY)

		r.buf[absoluteY][absoluteX] = string(char)
		relativeX++
	}
}

func (r *RuneDrawer) WriteX(x, y int, prefix, s, suffix string) {
	relativeX := 0
	relativeY := 0
	s += "\n"
	for _, char := range s {
		absoluteX := x + relativeX
		absoluteY := y + relativeY

		if char == '\n' {
			r.buf[absoluteY][absoluteX-1] += suffix
			relativeX = 0
			relativeY++
			continue
		}

		r.ensureSize(absoluteX, absoluteY)

		if relativeX == 0 {
			r.buf[absoluteY][absoluteX] = prefix + string(char)
		} else {
			r.buf[absoluteY][absoluteX] = string(char)
		}
		relativeX++
	}
}

func (r *RuneDrawer) WriteString(x, y int, s string) {
	r.ensureSize(x, y)
	r.buf[y][x] = s
}

func (r *RuneDrawer) WriteRune(x, y int, char rune) {
	r.ensureSize(x, y)
	r.buf[y][x] = string(char)
}

func (r *RuneDrawer) lookup(x, y int) string {
	if x < 0 || y < 0 || y >= len(r.buf) || x >= len(r.buf[y]) {
		return space
	}
	return r.buf[y][x]
}

func (r *RuneDrawer) ensureSize(x, y int) {
	// ensure there are enough rows
	if y >= len(r.buf) {
		rowsToAdd := y + 1 - len(r.buf)
		for range rowsToAdd {
			r.buf = append(r.buf, nil)
		}
	}

	// ensure there are enough columns in the row
	if x >= len(r.buf[y]) {
		columnsToAdd := x + 1 - len(r.buf[y])
		for range columnsToAdd {
			r.buf[y] = append(r.buf[y], space)
		}
	}
}

func (r *RuneDrawer) DrawSpline(spline Spline) {
	for i := 0; i < len(spline)-1; i++ {
		r.DrawLine(spline[i], spline[i+1])
	}
}

const (
	space          = " "
	lineMarker     = "\f"
	horizontalLine = "─"
	verticalLine   = "│"
	tlToBr         = "⟍"
	blToTr         = "⟋"
	verticalRight  = "├"
	verticalLeft   = "┤"
	horizontalDown = "┬"
	horizontalUp   = "┴"
	cross          = "┼"
	upLeft         = "╮"
	rightUp        = "╯"
	leftUp         = "╰"
	upRight        = "╭"
)

func (r *RuneDrawer) DrawLine(start, end Point) {
	// is horizontal
	if start.Y == end.Y {
		if start.X > end.X {
			start, end = end, start
		}
		r.ensureSize(end.X, end.Y)
		for x := start.X; x <= end.X; x++ {
			r.buf[end.Y][x] = lineMarker
		}
		return
	}
	// is vertical
	if start.X == end.X {
		if start.Y > end.Y {
			start, end = end, start
		}
		for y := start.Y; y <= end.Y; y++ {
			r.ensureSize(end.X, y)
			r.buf[y][end.X] = lineMarker
		}
		return
	}

	// best effort: draw two lines (vertical and horizontal) to achieve the same point to point distance
	// treat all lines as top-to-bottom
	var top Point
	var bottom Point
	if start.Y < end.Y {
		top = start
		bottom = end
	} else {
		top = end
		bottom = start
	}

	r.DrawSpline(
		Spline{
			Point{top.X, top.Y},
			Point{top.X, bottom.Y},
			Point{bottom.X, bottom.Y},
		},
	)
}

func (r *RuneDrawer) DrawRect(x, y, width, height int) {
	topLeft := Point{x, y}
	topRight := Point{x + width, y}
	bottomLeft := Point{x, y + height}
	bottomRight := Point{x + width, y + height}

	r.DrawLine(topLeft, topRight)
	r.DrawLine(topRight, bottomRight)
	r.DrawLine(bottomRight, bottomLeft)
	r.DrawLine(bottomLeft, topLeft)

	r.WriteString(topLeft.X, topLeft.Y, upRight)
	r.WriteString(topRight.X, topRight.Y, upLeft)
	r.WriteString(bottomLeft.X, bottomLeft.Y, leftUp)
	r.WriteString(bottomRight.X, bottomRight.Y, rightUp)
}

var (
	northConnecting = []string{lineMarker, verticalLine, verticalRight, verticalLeft, horizontalDown, cross, upLeft, upRight}
	southConnecting = []string{lineMarker, verticalLine, verticalRight, verticalLeft, horizontalUp, cross, leftUp, rightUp}
	eastConnecting  = []string{lineMarker, horizontalLine, verticalLeft, horizontalDown, horizontalUp, cross, upLeft, rightUp}
	westConnecting  = []string{lineMarker, horizontalLine, verticalRight, horizontalDown, horizontalUp, cross, leftUp, upRight}
)

func (r *RuneDrawer) EncodeCorners() {
	// replace all corner markers with the correct rune
	for y, row := range r.buf {
		for x, char := range row {
			if char == lineMarker {
				// look around to find the correct rune
				north := slices.Contains(northConnecting, r.lookup(x, y-1))
				south := slices.Contains(southConnecting, r.lookup(x, y+1))
				east := slices.Contains(eastConnecting, r.lookup(x+1, y))
				west := slices.Contains(westConnecting, r.lookup(x-1, y))
				if north {
					if south {
						if east {
							if west {
								// north-south-east-west
								r.buf[y][x] = cross
							} else {
								// north-south-east
								r.buf[y][x] = verticalRight
							}
						} else {
							if west {
								// north-south-west
								r.buf[y][x] = verticalLeft
							} else {
								// north-south
								r.buf[y][x] = verticalLine
							}
						}
					} else {
						if east {
							if west {
								// north-east-west
								r.buf[y][x] = horizontalUp
							} else {
								// north-east
								r.buf[y][x] = leftUp
							}
						} else {
							if west {
								// north-west
								r.buf[y][x] = rightUp
							} else {
								// north
								r.buf[y][x] = verticalLine
							}
						}
					}
				} else {
					if south {
						if east {
							if west {
								// south-east-west
								r.buf[y][x] = horizontalDown
							} else {
								// south-east
								r.buf[y][x] = upRight
							}
						} else {
							if west {
								// south-west
								r.buf[y][x] = upLeft
							} else {
								// south
								r.buf[y][x] = verticalLine
							}
						}
					} else {
						if east {
							if west {
								// east-west
								r.buf[y][x] = horizontalLine
							} else {
								// east
								r.buf[y][x] = horizontalLine
							}
						} else {
							if west {
								// west
								r.buf[y][x] = horizontalLine
							} else {
								// none
								r.buf[y][x] = space
							}
						}
					}
				}
			}
		}
	}
}

func (r *RuneDrawer) Width() int {
	w := 0
	for _, row := range r.buf {
		w = max(w, len(row))
	}
	return w
}

func (r *RuneDrawer) String() string {
	var sb strings.Builder
	for i, row := range r.buf {
		if i > 0 {
			sb.WriteRune('\n')
		}
		for _, char := range row {
			sb.WriteString(char)
		}
	}
	return sb.String()
}
