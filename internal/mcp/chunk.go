package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const DefaultMaxTokensPerChunk = 30000 // Default fallback value

// Chunk represents a group of files split by token estimate
type Chunk struct {
	ID            int      `json:"id"`
	TokenEstimate int      `json:"token_estimate"`
	Files         []string `json:"files"`
}

type ChunkIndex struct {
	TotalTokensEstimate int     `json:"total_tokens_estimate"`
	MaxPerChunk         int     `json:"max_per_chunk"`
	Chunks              []Chunk `json:"chunks"`
}

// estimateTokens approximates token count from byte length
func estimateTokens(b []byte) int {
	return len(b) / 4 // Rough approximation: 4 chars ≈ 1 token
}

// GenerateChunkIndex generates a list of file-based chunks given a root dir and a list of allowed files
func GenerateChunkIndex(root string, files []string, maxChunkSize int) (ChunkIndex, error) {
	sort.Strings(files)

	chunks := []Chunk{}
	current := Chunk{ID: 1}
	total := 0

	for _, path := range files {
		absPath := filepath.Join(root, path)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		tokens := estimateTokens(data)

		if current.TokenEstimate+tokens > maxChunkSize && len(current.Files) > 0 {
			chunks = append(chunks, current)
			current = Chunk{ID: current.ID + 1}
		}
		current.TokenEstimate += tokens
		current.Files = append(current.Files, path)
		total += tokens
	}

	if len(current.Files) > 0 {
		chunks = append(chunks, current)
	}

	return ChunkIndex{
		TotalTokensEstimate: total,
		MaxPerChunk:         maxChunkSize,
		Chunks:              chunks,
	}, nil
}

// HandleChunks serves the list of available chunks as JSON
func HandleChunks(root string, files []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		maxChunkSize := DefaultMaxTokensPerChunk
		if v := r.URL.Query().Get("max_chunk_size"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				maxChunkSize = parsed
			}
		}

		index, err := GenerateChunkIndex(root, files, maxChunkSize)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to generate chunk index"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(index)
	}
}

// HandleChunkByID serves one chunk's file contents in arklite-like format
func HandleChunkByID(root string, files []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		maxChunkSize := DefaultMaxTokensPerChunk
		if v := r.URL.Query().Get("max_chunk_size"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				maxChunkSize = parsed
			}
		}

		chunkIDStr := strings.TrimPrefix(r.URL.Path, "/mcp/chunk/")
		index, err := GenerateChunkIndex(root, files, maxChunkSize)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Chunk index generation failed"))
			return
		}
		var chunk *Chunk
		for _, ch := range index.Chunks {
			if fmt.Sprintf("%d", ch.ID) == chunkIDStr {
				chunk = &ch
				break
			}
		}
		if chunk == nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Chunk not found"))
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, rel := range chunk.Files {
			abspath := filepath.Join(root, rel)
			data, err := os.ReadFile(abspath)
			if err != nil {
				continue
			}
			io.WriteString(w, fmt.Sprintf("@%s\n", rel))
			w.Write(data)
			w.Write([]byte("\n\n"))
		}
	}
}
