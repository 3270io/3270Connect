package connect3270

import "testing"

func TestEscapeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "abc", want: "\"abc\""},
		{name: "quotes", input: "a\"b", want: "\"a\\\"b\""},
		{name: "newline", input: "a\nb", want: "\"a\\nb\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeString(tt.input); got != tt.want {
				t.Fatalf("escapeString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
