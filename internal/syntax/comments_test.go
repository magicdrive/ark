package syntax

import (
	"strings"
	"testing"
)

func TestDeleteCommentsTS_Go(t *testing.T) {
	source := []byte(`package main

// This is a line comment
func Greet() {
	/* This is a block comment */
	fmt.Println("Hello")
}
`)

	result, err := DeleteCommentsTS("test.go", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// Comments should be removed
	if strings.Contains(resultStr, "This is a line comment") {
		t.Error("Line comment was not removed")
	}
	if strings.Contains(resultStr, "This is a block comment") {
		t.Error("Block comment was not removed")
	}

	// Code should remain
	if !strings.Contains(resultStr, "package main") {
		t.Error("Package declaration was incorrectly removed")
	}
	if !strings.Contains(resultStr, "func Greet()") {
		t.Error("Function declaration was incorrectly removed")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("String content was incorrectly removed")
	}
}

func TestDeleteCommentsTS_StringWithURLNotRemoved(t *testing.T) {
	// PROMPT.md Section 24: URLs in strings should NOT be removed
	source := []byte(`package main

func GetURL() string {
	return "https://example.com/path"
}
`)

	result, err := DeleteCommentsTS("test.go", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// URL in string should NOT be removed
	if !strings.Contains(resultStr, "https://example.com/path") {
		t.Error("URL in string was incorrectly removed as comment")
	}
}

func TestDeleteCommentsTS_StringWithCommentPatternNotRemoved(t *testing.T) {
	// PROMPT.md Section 24: Comment patterns in strings should NOT be removed
	source := []byte(`package main

func GetPattern() string {
	return "/* not a comment */"
}
`)

	result, err := DeleteCommentsTS("test.go", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// Comment pattern in string should NOT be removed
	if !strings.Contains(resultStr, "/* not a comment */") {
		t.Error("Comment pattern in string was incorrectly removed")
	}
}

func TestDeleteCommentsTS_JavaScript(t *testing.T) {
	source := []byte(`
// Single line comment
function greet() {
  /* Multi-line
     comment */
  const url = "https://example.com";
  const text = "/* not comment */";
  console.log("Hello");
}
`)

	result, err := DeleteCommentsTS("test.js", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// Real comments should be removed
	if strings.Contains(resultStr, "Single line comment") {
		t.Error("Line comment was not removed")
	}
	if strings.Contains(resultStr, "Multi-line") {
		t.Error("Block comment was not removed")
	}

	// String contents should remain
	if !strings.Contains(resultStr, "https://example.com") {
		t.Error("URL in string was incorrectly removed")
	}
	if !strings.Contains(resultStr, "/* not comment */") {
		t.Error("Comment pattern in string was incorrectly removed")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("String content was incorrectly removed")
	}
}

func TestDeleteCommentsTS_Python(t *testing.T) {
	source := []byte(`
# This is a comment
def greet():
    url = "https://example.com"
    print("Hello")
`)

	result, err := DeleteCommentsTS("test.py", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// Comment should be removed
	if strings.Contains(resultStr, "This is a comment") {
		t.Error("Comment was not removed")
	}

	// String contents should remain
	if !strings.Contains(resultStr, "https://example.com") {
		t.Error("URL in string was incorrectly removed")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("String content was incorrectly removed")
	}
}

func TestDeleteCommentsTS_TypeScript(t *testing.T) {
	source := []byte(`
// Line comment
interface User {
  id: number;
}

/* Block comment */
function greet(): void {
  const url = "https://api.example.com";
  console.log("Hello");
}
`)

	result, err := DeleteCommentsTS("test.ts", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	// Comments should be removed
	if strings.Contains(resultStr, "Line comment") {
		t.Error("Line comment was not removed")
	}
	if strings.Contains(resultStr, "Block comment") {
		t.Error("Block comment was not removed")
	}

	// Code and strings should remain
	if !strings.Contains(resultStr, "interface User") {
		t.Error("Interface was incorrectly removed")
	}
	if !strings.Contains(resultStr, "https://api.example.com") {
		t.Error("URL in string was incorrectly removed")
	}
}

func TestDeleteCommentsTS_UnsupportedLanguage(t *testing.T) {
	source := []byte("Some content")

	_, err := DeleteCommentsTS("unknown.xyz", source)
	if err != ErrUnsupportedLanguage {
		t.Errorf("Expected ErrUnsupportedLanguage, got %v", err)
	}
}

func TestExtractCommentRanges_Go(t *testing.T) {
	source := []byte(`package main

// Comment at line 3
func Greet() {}
`)

	ranges, err := ExtractCommentRanges("test.go", source)
	if err != nil {
		t.Fatalf("ExtractCommentRanges failed: %v", err)
	}

	if len(ranges) != 1 {
		t.Errorf("Expected 1 comment range, got %d", len(ranges))
	}

	// The comment text should match
	if len(ranges) > 0 {
		commentText := string(source[ranges[0].StartByte:ranges[0].EndByte])
		if !strings.Contains(commentText, "Comment at line 3") {
			t.Errorf("Comment range doesn't contain expected text: %q", commentText)
		}
	}
}

func TestDeleteCommentsTS_EOFComment(t *testing.T) {
	// Test comment at end of file
	source := []byte(`package main

func Greet() {}
// EOF comment`)

	result, err := DeleteCommentsTS("test.go", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	if strings.Contains(resultStr, "EOF comment") {
		t.Error("EOF comment was not removed")
	}
	if !strings.Contains(resultStr, "func Greet()") {
		t.Error("Function was incorrectly removed")
	}
}

func TestDeleteCommentsTS_InlineComment(t *testing.T) {
	// Test inline comment after code
	source := []byte(`package main

func Greet() {} // inline comment
`)

	result, err := DeleteCommentsTS("test.go", source)
	if err != nil {
		t.Fatalf("DeleteCommentsTS failed: %v", err)
	}

	resultStr := string(result)

	if strings.Contains(resultStr, "inline comment") {
		t.Error("Inline comment was not removed")
	}
	if !strings.Contains(resultStr, "func Greet()") {
		t.Error("Function was incorrectly removed")
	}
}
