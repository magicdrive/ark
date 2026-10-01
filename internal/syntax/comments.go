package syntax

import (
	ts "github.com/odvcencio/gotreesitter"
)

// CommentRange represents the byte range of a comment in source code
type CommentRange struct {
	StartByte uint32
	EndByte   uint32
}

// ExtractCommentRanges extracts all comment ranges from source code using Tree-sitter.
// This accurately identifies comments without being confused by strings containing
// comment-like patterns (e.g., "https://example.com" or "/* not a comment */").
func ExtractCommentRanges(filename string, source []byte) ([]CommentRange, error) {
	result, err := ParseFile(filename, source)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	return extractCommentRangesFromTree(result.Tree, result.Language), nil
}

// extractCommentRangesFromTree walks the tree and collects all comment nodes
func extractCommentRangesFromTree(tree *ts.Tree, lang *ts.Language) []CommentRange {
	var ranges []CommentRange
	root := tree.RootNode()

	// Use Walk function from gotreesitter
	ts.Walk(root, func(node *ts.Node, depth int) ts.WalkAction {
		nodeType := node.Type(lang)

		// Check if this is a comment node
		if isCommentNodeType(nodeType) {
			ranges = append(ranges, CommentRange{
				StartByte: node.StartByte(),
				EndByte:   node.EndByte(),
			})
		}

		return ts.WalkContinue
	})

	return ranges
}

// isCommentNodeType returns true if the node type represents a comment
func isCommentNodeType(nodeType string) bool {
	switch nodeType {
	case "comment",
		"line_comment",
		"block_comment",
		"multiline_comment",
		// JavaScript/TypeScript
		"hash_bang_line",
		// Python
		"expression_statement": // Skip, need deeper check
		return nodeType == "comment" || nodeType == "line_comment" ||
			nodeType == "block_comment" || nodeType == "multiline_comment" ||
			nodeType == "hash_bang_line"
	}
	return false
}

// DeleteCommentsTS deletes comments from source code using Tree-sitter.
// Returns the source with comments removed, preserving line structure.
func DeleteCommentsTS(filename string, source []byte) ([]byte, error) {
	ranges, err := ExtractCommentRanges(filename, source)
	if err != nil {
		return nil, err
	}

	if len(ranges) == 0 {
		return source, nil
	}

	return removeRangesFromSource(source, ranges), nil
}

// removeRangesFromSource removes the specified byte ranges from source.
// Ranges should not overlap. This function sorts them by StartByte descending
// and removes from end to beginning to preserve byte offsets.
func removeRangesFromSource(source []byte, ranges []CommentRange) []byte {
	if len(ranges) == 0 {
		return source
	}

	// Sort ranges by StartByte in descending order (remove from end first)
	sortedRanges := make([]CommentRange, len(ranges))
	copy(sortedRanges, ranges)

	// Simple bubble sort (comment count is typically small)
	for i := 0; i < len(sortedRanges)-1; i++ {
		for j := 0; j < len(sortedRanges)-i-1; j++ {
			if sortedRanges[j].StartByte < sortedRanges[j+1].StartByte {
				sortedRanges[j], sortedRanges[j+1] = sortedRanges[j+1], sortedRanges[j]
			}
		}
	}

	result := make([]byte, len(source))
	copy(result, source)

	// Remove from end to beginning to preserve offsets
	for _, r := range sortedRanges {
		start := int(r.StartByte)
		end := int(r.EndByte)

		if start < 0 || end > len(result) || start >= end {
			continue
		}

		// Check if comment is followed by a newline and preserve it
		// to maintain line structure
		skipNewline := false
		if end < len(result) && result[end] == '\n' {
			skipNewline = true
		}

		// Remove the comment
		if skipNewline {
			result = append(result[:start], result[end+1:]...)
		} else {
			result = append(result[:start], result[end:]...)
		}
	}

	return result
}

// DeleteCommentsPreservingLines deletes comments but tries to preserve line count
// by replacing block comments with equivalent newlines
func DeleteCommentsPreservingLines(filename string, source []byte) ([]byte, error) {
	ranges, err := ExtractCommentRanges(filename, source)
	if err != nil {
		return nil, err
	}

	if len(ranges) == 0 {
		return source, nil
	}

	// Sort by StartByte descending
	sortedRanges := make([]CommentRange, len(ranges))
	copy(sortedRanges, ranges)
	for i := 0; i < len(sortedRanges)-1; i++ {
		for j := 0; j < len(sortedRanges)-i-1; j++ {
			if sortedRanges[j].StartByte < sortedRanges[j+1].StartByte {
				sortedRanges[j], sortedRanges[j+1] = sortedRanges[j+1], sortedRanges[j]
			}
		}
	}

	result := make([]byte, len(source))
	copy(result, source)

	for _, r := range sortedRanges {
		start := int(r.StartByte)
		end := int(r.EndByte)

		if start < 0 || end > len(result) || start >= end {
			continue
		}

		// Count newlines in the comment
		newlineCount := 0
		for _, b := range result[start:end] {
			if b == '\n' {
				newlineCount++
			}
		}

		// Replace with equivalent newlines to preserve line count
		replacement := make([]byte, newlineCount)
		for i := 0; i < newlineCount; i++ {
			replacement[i] = '\n'
		}

		result = append(result[:start], append(replacement, result[end:]...)...)
	}

	return result, nil
}
