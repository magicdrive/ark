package mcp

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/magicdrive/ark/internal/commandline"
)

// RunMCPServe launches an MCP-compatible HTTP server using ProjectWatcher
func RunMCPServe(root string, serverOpt *commandline.ServeOption) {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current dir: %v", err)
	}

	opt := serverOpt.GeneralOption

	allowed, _ := GenerateDirectoryStructure(root, []string{}, opt)
	pw := NewProjectWatcher(root, allowed, func(root string) []string {
		xallowed, _ := GenerateDirectoryStructure(root, []string{}, opt)
		return xallowed
	})

	server := &http.Server{
		Addr:    ":" + serverOpt.Port,
		Handler: SetupRoutes(root, pw, serverOpt),
	}

	// Graceful shutdown setup
	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

		<-sigint
		log.Printf("[mcp] shutdown signal received")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("[mcp] graceful shutdown failed: %v", err)
		} else {
			log.Printf("[mcp] server shutdown complete")
		}
		close(idleConnsClosed)
	}()

	log.Printf("[mcp] MCP server running at http://localhost%s", serverOpt.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("[mcp] server error: %v", err)
	}

	<-idleConnsClosed
}

// SetupRoutes initializes all MCP endpoints using the ProjectWatcher.
func SetupRoutes(root string, pw *ProjectWatcher, serverOpt *commandline.ServeOption) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/mcp/metadata", HandleMetadata())
	mux.HandleFunc("/mcp/chunks", func(w http.ResponseWriter, r *http.Request) {
		pw.RefreshIfDirty()
		HandleChunks(root, pw.GetAllowed())(w, r)
	})
	mux.HandleFunc("/mcp/chunk/", func(w http.ResponseWriter, r *http.Request) {
		pw.RefreshIfDirty()
		HandleChunkByID(root, pw.GetAllowed())(w, r)
	})
	mux.HandleFunc("/mcp/file", func(w http.ResponseWriter, r *http.Request) {
		pw.RefreshIfDirty()
		HandleFile(root, pw.GetAllowed(), serverOpt)(w, r)
	})
	mux.HandleFunc("/mcp/structure", func(w http.ResponseWriter, r *http.Request) {
		pw.RefreshIfDirty()
		HandleStructureJSON(pw.GetAllowed())(w, r)
	})
	mux.HandleFunc("/mcp/search", func(w http.ResponseWriter, r *http.Request) {
		pw.RefreshIfDirty()
		HandleSearch(root, pw.GetAllowed())(w, r)
	})

	return mux
}
