//go:build linux
// +build linux

package main

import (
	"os"
	"sort"
)

func ApplySort(files []os.DirEntry) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})
}
