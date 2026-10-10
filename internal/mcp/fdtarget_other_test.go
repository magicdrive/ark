//go:build !linux && !darwin

package mcp

// fdDir is empty: the descriptors' objects are not named here (on Windows a
// held handle is seen by the removal it blocks instead).
const fdDir = ""

func fdTarget(string) (string, bool) { return "", false }
