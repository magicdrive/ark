package core

import (
	"bytes"
)

type commentPattern struct {
	linePrefixes []string
	blockDelims  []struct{ start, end string }
}

func getCommentDelimiters(lang string) commentPattern {
	switch lang {
	case "markdown", "text":
		return commentPattern{}
	case "bash", "sh", "zsh", "dockerfile", "makefile", "cmake", "groovy", "abap", "ada", "ahk", "apache", "applescript", "as", "powershell", "r", "ruby", "perl", "el", "latex":
		return commentPattern{linePrefixes: []string{"#"}}
	case "vim":
		return commentPattern{linePrefixes: []string{"\""}} // Vim uses double quote for comments
	case "python", "yaml", "toml", "ini":
		return commentPattern{linePrefixes: []string{"#"}}
	case "go", "java", "c", "cpp", "csharp", "javascript", "typescript", "ts", "tsx", "jsx", "rust", "kotlin", "swift", "dart", "coffeescript":
		return commentPattern{
			linePrefixes: []string{"//"},
			blockDelims: []struct{ start, end string }{
				{"/*", "*/"},
			},
		}
	case "html", "xml", "vue":
		return commentPattern{
			blockDelims: []struct{ start, end string }{
				{"<!--", "-->"},
			},
		}
	case "php":
		return commentPattern{
			linePrefixes: []string{"//", "#"},
			blockDelims: []struct{ start, end string }{
				{"/*", "*/"},
			},
		}
	case "scala", "haskell", "clojure", "lisp", "elixir":
		return commentPattern{linePrefixes: []string{"--", ";"}}
	case "sql":
		return commentPattern{linePrefixes: []string{"--"}}
	case "css", "less", "scss":
		return commentPattern{
			blockDelims: []struct{ start, end string }{
				{"/*", "*/"},
			},
		}
	case "json", "bf":
		return commentPattern{}
	case "objectivec":
		return commentPattern{
			linePrefixes: []string{"//"},
			blockDelims: []struct{ start, end string }{
				{"/*", "*/"},
			},
		}
	default:
		return commentPattern{}
	}
}

func stripComments(data []byte, pattern commentPattern) []byte {
	for _, d := range pattern.blockDelims {
		data = stripBlockByDelimiter(data, d.start, d.end)
	}

	lines := bytes.Split(data, []byte("\n"))
	var result [][]byte

LINE:
	for _, line := range lines {
		trim := bytes.TrimSpace(line)
		if len(trim) == 0 {
			continue
		}
		for _, prefix := range pattern.linePrefixes {
			if bytes.HasPrefix(trim, []byte(prefix)) {
				continue LINE
			}
		}
		result = append(result, trim)
	}

	return bytes.Join(result, []byte("\n"))
}

func stripBlockByDelimiter(data []byte, start, end string) []byte {
	var out []byte
	inBlock := false
	i := 0
	for i < len(data) {
		if !inBlock && hasPrefixAt(data, start, i) {
			inBlock = true
			i += len(start)
			continue
		}
		if inBlock && hasPrefixAt(data, end, i) {
			inBlock = false
			i += len(end)
			continue
		}
		if !inBlock {
			out = append(out, data[i])
		}
		i++
	}
	return out
}

func hasPrefixAt(data []byte, prefix string, i int) bool {
	return i+len(prefix) <= len(data) && bytes.HasPrefix(data[i:], []byte(prefix))
}
