//go:build ignore

// eoftail counts the files whose last token is a tag cut off by the end of
// the input (eof-in-tag): x/net's tokenizer drops it, as the standard does,
// and leaves it in the final ErrorToken's Raw — the one place it shows.
package main

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/net/html"
)

func main() {
	n := 0
	for _, f := range os.Args[1:] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		z := html.NewTokenizer(bytes.NewReader(src))
		for z.Next() != html.ErrorToken {
		}
		if tail := z.Raw(); len(tail) > 0 {
			n++
			fmt.Printf("TAIL %s %q\n", f, tail)
		}
	}
	fmt.Printf("files_with_eof_in_tag=%d\n", n)
}
