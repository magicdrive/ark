//go:build darwin || windows
// +build darwin windows

package main

import (
	"os"
	"sort"
	"strings"
)

func ApplySort(files []os.DirEntry) {
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name())
	})
}
