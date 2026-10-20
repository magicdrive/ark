package core

import (
	"fmt"
	"io"
	"os"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/textenc"
)

// dumpWarnings receives a line for each file the dump leaves out because its
// text cannot be read.
var dumpWarnings io.Writer = os.Stderr

// dumpText returns the text of the file at fpath for the dump, decoded from
// its own encoding (textenc.Decode, or the encoding --file-encoding names for
// it), so that files of any encodings join as UTF-8. It returns false when
// the dump leaves the file out: binary (silently, as always), not UTF-8 under
// --skip-non-utf8 (silently, as asked), or not text in an encoding it can
// tell or in the one named for it — announced on stderr, never dumped with
// characters replaced.
func dumpText(opt *commandline.Option, fpath string, data []byte) (string, bool) {
	r := textenc.Decode(data, opt.FileEncodingFor(fpath))
	switch r.Kind {
	case textenc.Text:
		if opt.SkipNonUTF8Flag && r.Encoding != "utf-8" {
			return "", false
		}
		return r.Text, true
	case textenc.Binary:
		return "", false
	}
	if opt.SkipNonUTF8Flag {
		return "", false
	}
	fmt.Fprintf(dumpWarnings, "ark: %s is left out of the dump: %s (name its encoding with --file-encoding)\n", fpath, r.Reason)
	return "", false
}

// warnUnusedFileEncodings announces the --file-encoding rules that match none
// of the files to dump: a pattern that names nothing is a mistake to see.
func warnUnusedFileEncodings(opt *commandline.Option, files map[string]bool) {
	if len(opt.FileEncodingRules) == 0 {
		return
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	for _, r := range opt.UnusedFileEncodingRules(paths) {
		fmt.Fprintf(dumpWarnings, "ark: --file-encoding %s=%s matches no file of the dump\n", r.Pattern, r.Encoding)
	}
}
