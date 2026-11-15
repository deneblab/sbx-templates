package main

import (
	"strings"
	"testing"
)

func TestParseSbxVersion(t *testing.T) {
	for in, want := range map[string][3]int{
		"sbx version 0.53.0\n": {0, 53, 0},
		"v0.54.0-rc7":          {0, 54, 0},
		"Client Version: 0.48": {0, 48, 0},
		"0.51.2 (abc123)":      {0, 51, 2},
	} {
		got, ok := parseSbxVersion(in)
		if !ok || got != want {
			t.Errorf("parseSbxVersion(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	if _, ok := parseSbxVersion("dev build"); ok {
		t.Error("a version was read from text without one")
	}
}

func TestCheckSbxVersion(t *testing.T) {
	for out, wantErr := range map[string]bool{
		"sbx version 0.48.1": true,
		"0.52.9":             true,
		"0.53.0":             false,
		"v0.54.0-rc7":        false,
		"1.0.0":              false,
		"no version here":    false, // not judged
	} {
		stubSbx(t, map[string]string{"--version": out})
		err := checkSbxVersion()
		if (err != nil) != wantErr {
			t.Errorf("%q: error = %v, want error %v", out, err, wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "0.53") {
			t.Errorf("%q: the error does not name the minimum: %v", out, err)
		}
	}
	stubSbx(t, nil) // sbx cannot be run
	if err := checkSbxVersion(); err != nil {
		t.Errorf("an sbx that cannot be run was judged: %v", err)
	}
}
