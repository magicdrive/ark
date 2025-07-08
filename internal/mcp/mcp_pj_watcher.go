package mcp

import (
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/core"
)

type ProjectWatcher struct {
	Root         string
	AllowedFiles []string
	dirty        atomic.Bool
	rescanFunc   func(string) []string
	Option       *commandline.ServeOption
}

func NewProjectWatcher(root string, opt *commandline.ServeOption, initial []string, rescan func(string) []string) *ProjectWatcher {
	pw := &ProjectWatcher{
		Root:         root,
		AllowedFiles: initial,
		rescanFunc:   rescan,
		Option:       opt,
	}
	pw.startWatcher()
	return pw
}

func (pw *ProjectWatcher) startWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[watcher] failed to initialize: %v", err)
		return
	}

	// watch all existing directories recursively
	log.Printf("[watcher] add watcher: %v", pw.Root)

	filepath.WalkDir(pw.Root, func(path string, dEntry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if core.IsUnderGitDir(path) {
			return nil
		}
		if dEntry.IsDir() {
			if err := watcher.Add(path); err != nil {
				log.Printf("[watcher] failed to watch dir: %s: %v", path, err)
			}
		}
		return nil
	})

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op == fsnotify.Chmod {
					return
				}

				if !core.CanBoaded(pw.Option.GeneralOption, event.Name) {
					return
				}

				log.Printf("[watcher] event: %v", event)
				pw.dirty.Store(true)

				// watch new directories when created
				if event.Op&fsnotify.Create == fsnotify.Create {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						err := watcher.Add(event.Name)
						if err != nil {
							log.Printf("[watcher] failed to watch new dir: %s: %v", event.Name, err)
						} else {
							log.Printf("[watcher] now watching new dir: %s", event.Name)
						}
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[watcher] error: %v", err)
			}
		}
	}()

	// background refresher
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			pw.RefreshIfDirty()
		}
	}()
}

// RefreshIfDirty rescans files if dirty flag is set
func (pw *ProjectWatcher) RefreshIfDirty() {
	if pw.dirty.Swap(false) {
		pw.AllowedFiles = pw.rescanFunc(pw.Root)
		log.Printf("[watcher] project files refreshed")
	}
}

// ShouldRefresh tells whether refresh hint should be set
func (pw *ProjectWatcher) ShouldRefresh() bool {
	return pw.dirty.Load()
}

// GetAllowed returns the current allowed file list
func (pw *ProjectWatcher) GetAllowed() []string {
	return pw.AllowedFiles
}
