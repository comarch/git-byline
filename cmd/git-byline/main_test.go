package main

import (
	"errors"
	"testing"

	"github.com/comarch/git-byline/internal/app"
)

// TestExitCode covers every Run result shape main can observe. main
// itself runs only in a spawned binary, so the mapping lives here where
// the defensive branch is reachable.
func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		code int
		err  error
		want int
	}{
		{
			name: "success",
			code: app.ExitSuccess,
			err:  nil,
			want: app.ExitSuccess,
		},
		{
			name: "usage error passes code through",
			code: app.ExitUsage,
			err:  nil,
			want: app.ExitUsage,
		},
		{
			name: "failure passes code through",
			code: app.ExitFailure,
			err:  errors.New("operational failure"),
			want: app.ExitFailure,
		},
		{
			name: "error with success code is a bug",
			code: app.ExitSuccess,
			err:  errors.New("inconsistent result"),
			want: app.ExitFailure,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.code, tc.err); got != tc.want {
				t.Fatalf("exitCode(%d, %v) = %d, want %d", tc.code, tc.err, got, tc.want)
			}
		})
	}
}
