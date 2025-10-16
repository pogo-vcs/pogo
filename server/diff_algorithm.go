package server

import (
	"strings"

	diffmp "github.com/sergi/go-diff/diffmatchpatch"
)

type DiffLine struct {
	Type       LineType
	Content    string
	OldLineNum int
	NewLineNum int
}

type LineType int

const (
	LineUnchanged LineType = iota
	LineAdded
	LineRemoved
)

func MyersDiff(oldContent, newContent string) []DiffLine {
	if oldContent == "" && newContent == "" {
		return []DiffLine{}
	}

	dmp := diffmp.New()

	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	diffs := dmp.DiffMain(chars1, chars2, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	var result []DiffLine
	oldLineNum := 0
	newLineNum := 0

	for _, diff := range diffs {
		lines := strings.Split(diff.Text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		for _, line := range lines {
			switch diff.Type {
			case diffmp.DiffEqual:
				result = append(result, DiffLine{
					Type:       LineUnchanged,
					Content:    line,
					OldLineNum: oldLineNum,
					NewLineNum: newLineNum,
				})
				oldLineNum++
				newLineNum++

			case diffmp.DiffDelete:
				result = append(result, DiffLine{
					Type:       LineRemoved,
					Content:    line,
					OldLineNum: oldLineNum,
					NewLineNum: -1,
				})
				oldLineNum++

			case diffmp.DiffInsert:
				result = append(result, DiffLine{
					Type:       LineAdded,
					Content:    line,
					OldLineNum: -1,
					NewLineNum: newLineNum,
				})
				newLineNum++
			}
		}
	}

	return result
}

func PatienceDiff(oldContent, newContent string) []DiffLine {
	if oldContent == "" && newContent == "" {
		return []DiffLine{}
	}

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	return patienceDiffLines(oldLines, newLines, 0, 0)
}

func patienceDiffLines(oldLines, newLines []string, oldStart, newStart int) []DiffLine {
	if len(oldLines) == 0 && len(newLines) == 0 {
		return []DiffLine{}
	}

	if len(oldLines) == 0 {
		result := make([]DiffLine, len(newLines))
		for i, line := range newLines {
			result[i] = DiffLine{
				Type:       LineAdded,
				Content:    line,
				OldLineNum: -1,
				NewLineNum: newStart + i,
			}
		}
		return result
	}

	if len(newLines) == 0 {
		result := make([]DiffLine, len(oldLines))
		for i, line := range oldLines {
			result[i] = DiffLine{
				Type:       LineRemoved,
				Content:    line,
				OldLineNum: oldStart + i,
				NewLineNum: -1,
			}
		}
		return result
	}

	uniqueOld := findUniqueCommonLines(oldLines, newLines)
	if len(uniqueOld) == 0 {
		return myersFallback(oldLines, newLines, oldStart, newStart)
	}

	lcs := longestCommonSubsequence(uniqueOld)
	if len(lcs) == 0 {
		return myersFallback(oldLines, newLines, oldStart, newStart)
	}

	var result []DiffLine
	oldIdx := 0
	newIdx := 0

	for _, match := range lcs {
		if oldIdx < match.oldIdx || newIdx < match.newIdx {
			subResult := patienceDiffLines(
				oldLines[oldIdx:match.oldIdx],
				newLines[newIdx:match.newIdx],
				oldStart+oldIdx,
				newStart+newIdx,
			)
			result = append(result, subResult...)
		}

		result = append(result, DiffLine{
			Type:       LineUnchanged,
			Content:    oldLines[match.oldIdx],
			OldLineNum: oldStart + match.oldIdx,
			NewLineNum: newStart + match.newIdx,
		})

		oldIdx = match.oldIdx + 1
		newIdx = match.newIdx + 1
	}

	if oldIdx < len(oldLines) || newIdx < len(newLines) {
		subResult := patienceDiffLines(
			oldLines[oldIdx:],
			newLines[newIdx:],
			oldStart+oldIdx,
			newStart+newIdx,
		)
		result = append(result, subResult...)
	}

	return result
}

type lineMatch struct {
	oldIdx int
	newIdx int
}

func findUniqueCommonLines(oldLines, newLines []string) []lineMatch {
	oldCounts := make(map[string]int)
	newCounts := make(map[string]int)
	oldIndices := make(map[string]int)
	newIndices := make(map[string]int)

	for i, line := range oldLines {
		oldCounts[line]++
		if oldCounts[line] == 1 {
			oldIndices[line] = i
		}
	}

	for i, line := range newLines {
		newCounts[line]++
		if newCounts[line] == 1 {
			newIndices[line] = i
		}
	}

	var matches []lineMatch
	for line, oldIdx := range oldIndices {
		if oldCounts[line] == 1 && newCounts[line] == 1 {
			if newIdx, exists := newIndices[line]; exists {
				matches = append(matches, lineMatch{
					oldIdx: oldIdx,
					newIdx: newIdx,
				})
			}
		}
	}

	return matches
}

func longestCommonSubsequence(matches []lineMatch) []lineMatch {
	if len(matches) == 0 {
		return []lineMatch{}
	}

	n := len(matches)
	tails := make([]lineMatch, 0, n)
	predecessors := make([]int, n)

	for i := range predecessors {
		predecessors[i] = -1
	}

	for i, match := range matches {
		pos := binarySearchMatches(tails, match)

		if pos == len(tails) {
			tails = append(tails, match)
		} else {
			tails[pos] = match
		}

		if pos > 0 {
			predecessors[i] = findPredecessor(matches, tails, pos-1)
		}
	}

	result := make([]lineMatch, len(tails))
	idx := -1
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i] == tails[len(tails)-1] {
			idx = i
			break
		}
	}

	for i := len(result) - 1; i >= 0 && idx >= 0; i-- {
		result[i] = matches[idx]
		idx = predecessors[idx]
	}

	return result
}

func binarySearchMatches(tails []lineMatch, match lineMatch) int {
	left, right := 0, len(tails)
	for left < right {
		mid := (left + right) / 2
		if tails[mid].oldIdx < match.oldIdx && tails[mid].newIdx < match.newIdx {
			left = mid + 1
		} else {
			right = mid
		}
	}
	return left
}

func findPredecessor(matches []lineMatch, tails []lineMatch, pos int) int {
	target := tails[pos]
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i] == target {
			return i
		}
	}
	return -1
}

func myersFallback(oldLines, newLines []string, oldStart, newStart int) []DiffLine {
	oldContent := strings.Join(oldLines, "\n")
	newContent := strings.Join(newLines, "\n")

	dmp := diffmp.New()
	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	diffs := dmp.DiffMain(chars1, chars2, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	var result []DiffLine
	oldLineNum := oldStart
	newLineNum := newStart

	for _, diff := range diffs {
		lines := strings.Split(diff.Text, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		for _, line := range lines {
			switch diff.Type {
			case diffmp.DiffEqual:
				result = append(result, DiffLine{
					Type:       LineUnchanged,
					Content:    line,
					OldLineNum: oldLineNum,
					NewLineNum: newLineNum,
				})
				oldLineNum++
				newLineNum++

			case diffmp.DiffDelete:
				result = append(result, DiffLine{
					Type:       LineRemoved,
					Content:    line,
					OldLineNum: oldLineNum,
					NewLineNum: -1,
				})
				oldLineNum++

			case diffmp.DiffInsert:
				result = append(result, DiffLine{
					Type:       LineAdded,
					Content:    line,
					OldLineNum: -1,
					NewLineNum: newLineNum,
				})
				newLineNum++
			}
		}
	}

	return result
}
