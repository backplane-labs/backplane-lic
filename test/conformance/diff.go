package conformance

import (
	"fmt"
	"strings"
)

// diffContextLines is the unified-diff context around each change group.
const diffContextLines = 3

// diffOp is one line-level edit.
type diffOp struct {
	kind byte // '=', '-' or '+'
	line string
}

// Diff renders a unified diff between the line sequences a and b, or an
// empty string when they are equal.
func Diff(a, b string) string {
	hunks := renderHunks(diffOps(strings.Split(a, "\n"), strings.Split(b, "\n")))
	if len(hunks) == 0 {
		return ""
	}

	return strings.Join(hunks, "")
}

// diffOps computes the edit script via a longest-common-subsequence table.
func diffOps(from, into []string) []diffOp {
	lcs := make([][]int, len(from)+1)
	for index := range lcs {
		lcs[index] = make([]int, len(into)+1)
	}

	for index := len(from) - 1; index >= 0; index-- {
		for jndex := len(into) - 1; jndex >= 0; jndex-- {
			switch {
			case from[index] == into[jndex]:
				lcs[index][jndex] = lcs[index+1][jndex+1] + 1
			case lcs[index+1][jndex] >= lcs[index][jndex+1]:
				lcs[index][jndex] = lcs[index+1][jndex]
			default:
				lcs[index][jndex] = lcs[index][jndex+1]
			}
		}
	}

	var ops []diffOp

	index, jndex := 0, 0
	for index < len(from) && jndex < len(into) {
		switch {
		case from[index] == into[jndex]:
			ops = append(ops, diffOp{kind: '=', line: from[index]})
			index++
			jndex++
		case lcs[index+1][jndex] >= lcs[index][jndex+1]:
			ops = append(ops, diffOp{kind: '-', line: from[index]})
			index++
		default:
			ops = append(ops, diffOp{kind: '+', line: into[jndex]})
			jndex++
		}
	}

	for ; index < len(from); index++ {
		ops = append(ops, diffOp{kind: '-', line: from[index]})
	}

	for ; jndex < len(into); jndex++ {
		ops = append(ops, diffOp{kind: '+', line: into[jndex]})
	}

	return ops
}

// renderHunks groups the edit script into unified hunks with fixed context.
func renderHunks(ops []diffOp) []string {
	var groups [][2]int

	lastChange := -1

	for index, op := range ops {
		if op.kind == '=' {
			continue
		}

		if lastChange < 0 || index-lastChange > 2*diffContextLines+1 {
			groups = append(groups, [2]int{index, index})
		} else {
			groups[len(groups)-1][1] = index
		}

		lastChange = index
	}

	if len(groups) == 0 {
		return nil
	}

	// Line numbers each op starts at, on the old and new side.
	oldAt, newAt := make([]int, len(ops)), make([]int, len(ops))

	oldLine, newLine := 1, 1

	for index, op := range ops {
		oldAt[index], newAt[index] = oldLine, newLine

		switch op.kind {
		case '=':
			oldLine++
			newLine++
		case '-':
			oldLine++
		case '+':
			newLine++
		}
	}

	hunks := make([]string, 0, len(groups))
	for _, group := range groups {
		start := max(group[0]-diffContextLines, 0)
		end := min(group[1]+diffContextLines, len(ops)-1)

		hunks = append(hunks, renderHunk(ops, oldAt, newAt, start, end))
	}

	return hunks
}

// renderHunk formats one hunk: the @@ header and its context/changed lines.
func renderHunk(ops []diffOp, oldAt, newAt []int, start, end int) string {
	oldCount, newCount := 0, 0

	var body strings.Builder

	for index := start; index <= end; index++ {
		op := ops[index]
		body.WriteByte(op.kind)
		body.WriteString(op.line)
		body.WriteByte('\n')

		switch op.kind {
		case '=':
			oldCount++
			newCount++
		case '-':
			oldCount++
		case '+':
			newCount++
		}
	}

	return fmt.Sprintf("@@ -%d,%d +%d,%d @@\n%s", oldAt[start], oldCount, newAt[start], newCount, body.String())
}
