package secrets

import "testing"

func TestHide(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		s       string
		secrets []string
		want    string
	}{
		{
			name:    "no secrets",
			s:       "hello world",
			secrets: []string{},
			want:    "hello world",
		},
		{
			name:    "one secret",
			s:       "hello world",
			secrets: []string{"world"},
			want:    "hello ****",
		},
		{
			name:    "repeated secrets",
			s:       "hello world world hello hello world",
			secrets: []string{"world"},
			want:    "hello **** **** hello hello ****",
		},
		{
			name:    "complex secrets",
			s:       "foo [<( bar .+ baz [a-z]*",
			secrets: []string{"[<(", ".+"},
			want:    "foo **** bar **** baz [a-z]*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Hide(tt.s, tt.secrets)
			if got != tt.want {
				t.Errorf("Hide() = %v, want %v", got, tt.want)
			}
		})
	}
}
