package server

import "testing"

func TestMyersDiffBasic(t *testing.T) {
	oldContent := "alpha\nbeta\ngamma"
	newContent := "alpha\ngamma\ntheta"

	got := MyersDiff(oldContent, newContent)
	want := []DiffLine{
		{Type: LineUnchanged, Content: "alpha", OldLineNum: 0, NewLineNum: 0},
		{Type: LineRemoved, Content: "beta", OldLineNum: 1, NewLineNum: -1},
		{Type: LineRemoved, Content: "gamma", OldLineNum: 2, NewLineNum: -1},
		{Type: LineAdded, Content: "gamma", OldLineNum: -1, NewLineNum: 1},
		{Type: LineAdded, Content: "theta", OldLineNum: -1, NewLineNum: 2},
	}

	assertDiffLinesEqual(t, want, got)
}

func TestMyersDiffEmptyInputs(t *testing.T) {
	tests := []struct {
		name       string
		oldContent string
		newContent string
		want       []DiffLine
	}{
		{
			name:       "both empty",
			oldContent: "",
			newContent: "",
			want:       []DiffLine{},
		},
		{
			name:       "old empty",
			oldContent: "",
			newContent: "one\ntwo",
			want: []DiffLine{
				{Type: LineAdded, Content: "one", OldLineNum: -1, NewLineNum: 0},
				{Type: LineAdded, Content: "two", OldLineNum: -1, NewLineNum: 1},
			},
		},
		{
			name:       "new empty",
			oldContent: "one\ntwo",
			newContent: "",
			want: []DiffLine{
				{Type: LineRemoved, Content: "one", OldLineNum: 0, NewLineNum: -1},
				{Type: LineRemoved, Content: "two", OldLineNum: 1, NewLineNum: -1},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := MyersDiff(tt.oldContent, tt.newContent)
			assertDiffLinesEqual(t, tt.want, got)
		})
	}
}

func TestPatienceDiffBasic(t *testing.T) {
	oldContent := "alpha\nbeta\ngamma\ndelta"
	newContent := "alpha\ngamma\ntheta\ndelta"

	got := PatienceDiff(oldContent, newContent)
	want := []DiffLine{
		{Type: LineUnchanged, Content: "alpha", OldLineNum: 0, NewLineNum: 0},
		{Type: LineRemoved, Content: "beta", OldLineNum: 1, NewLineNum: -1},
		{Type: LineUnchanged, Content: "gamma", OldLineNum: 2, NewLineNum: 1},
		{Type: LineAdded, Content: "theta", OldLineNum: -1, NewLineNum: 2},
		{Type: LineUnchanged, Content: "delta", OldLineNum: 3, NewLineNum: 3},
	}

	assertDiffLinesEqual(t, want, got)
}

func TestPatienceDiffFallsBackToMyers(t *testing.T) {
	oldContent := "alpha\nbeta\nalpha"
	newContent := "alpha\ntheta\nalpha"

	got := PatienceDiff(oldContent, newContent)
	want := []DiffLine{
		{Type: LineUnchanged, Content: "alpha", OldLineNum: 0, NewLineNum: 0},
		{Type: LineRemoved, Content: "beta", OldLineNum: 1, NewLineNum: -1},
		{Type: LineAdded, Content: "theta", OldLineNum: -1, NewLineNum: 1},
		{Type: LineUnchanged, Content: "alpha", OldLineNum: 2, NewLineNum: 2},
	}

	assertDiffLinesEqual(t, want, got)
}

func TestPatienceDiffHandlesEmptyInputs(t *testing.T) {
	tests := []struct {
		name       string
		oldContent string
		newContent string
		want       []DiffLine
	}{
		{
			name:       "old empty",
			oldContent: "",
			newContent: "one\ntwo",
			want: []DiffLine{
				{Type: LineAdded, Content: "one", OldLineNum: -1, NewLineNum: 0},
				{Type: LineAdded, Content: "two", OldLineNum: -1, NewLineNum: 1},
			},
		},
		{
			name:       "new empty",
			oldContent: "one\ntwo",
			newContent: "",
			want: []DiffLine{
				{Type: LineRemoved, Content: "one", OldLineNum: 0, NewLineNum: -1},
				{Type: LineRemoved, Content: "two", OldLineNum: 1, NewLineNum: -1},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := PatienceDiff(tt.oldContent, tt.newContent)
			assertDiffLinesEqual(t, tt.want, got)
		})
	}
}

func assertDiffLinesEqual(t *testing.T, want, got []DiffLine) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("unexpected diff length: want %d got %d\nwant: %#v\ngot: %#v", len(want), len(got), want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected diff line at index %d:\nwant: %#v\ngot: %#v", i, want[i], got[i])
		}
	}
}
