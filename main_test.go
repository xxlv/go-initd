package main

import (
	"testing"
)

func TestDirname(t *testing.T) {
	testCases := []struct {
		name         string
		expectedDir  string
		expectedFail bool
	}{
		{
			name:        "/path/to/file.txt",
			expectedDir: "/path/to",
		},
		{
			name:        "/path/to/file",
			expectedDir: "/path/to",
		},
		{
			name:        "",
			expectedDir: "",
		},
	}

	for _, tc := range testCases {
		dir := dirname(tc.name)
		if dir != tc.expectedDir {
			t.Errorf("dirname(%q) got %q, except  %q", tc.name, dir, tc.expectedDir)
		}

	}
}
