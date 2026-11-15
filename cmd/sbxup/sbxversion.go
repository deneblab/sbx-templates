package main

// sbxup needs sbx 0.53 or newer: it reads `sbx ls --json` and `sbx template ls --json` without the
// pre-0.48 shapes, relies on the 0.53 `sbx exec` flags and on the `PATH:TARGET[:ro]` workspaces of 0.51.

import (
	"fmt"
	"regexp"
	"strconv"
)

// minSbx is the oldest sbx that sbxup supports.
var minSbx = [3]int{0, 53, 0}

var sbxVersionPattern = regexp.MustCompile(`v?(\d+)\.(\d+)(?:\.(\d+))?`)

// parseSbxVersion takes the first version number in the output of `sbx --version`, whatever words
// surround it ("sbx version 0.53.0", "v0.54.0-rc7").
func parseSbxVersion(out string) ([3]int, bool) {
	m := sbxVersionPattern.FindStringSubmatch(out)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		if m[i+1] != "" {
			v[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return v, true
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// checkSbxVersion refuses an sbx older than minSbx. `sbx --version` needs no daemon. An sbx that
// cannot be run or whose output holds no version is not judged here: the commands that follow
// report that better than a guess would.
func checkSbxVersion() error {
	out, err := sbxOutput("--version")
	if err != nil {
		return nil
	}
	v, ok := parseSbxVersion(string(out))
	if !ok || !versionLess(v, minSbx) {
		return nil
	}
	return fmt.Errorf("sbx %d.%d.%d is too old: sbxup needs sbx %d.%d or newer. Update sbx and run sbxup again",
		v[0], v[1], v[2], minSbx[0], minSbx[1])
}
