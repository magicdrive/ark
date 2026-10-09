package commandline

import (
	"regexp"
	"strings"
	"testing"
)

// The help text states every repository-dump flag's default; each must be the
// default the CLI actually applies. (help.txt once said 'ark_output.txt' and
// line numbers 'on' while the CLI wrote ark-output.txt without them.)
func TestHelp_DumpDefaultsMatchTheFlags(t *testing.T) {
	_, opt, err := GeneralOptParse([]string{})
	if err != nil {
		t.Fatal(err)
	}
	start := regexp.MustCompile(`general \([^)]*\) options:`).FindStringIndex(helpMessage)
	if start == nil {
		t.Fatal("help has no general options section")
	}
	section, _, _ := strings.Cut(helpMessage[start[0]:], "\n\n")
	line := regexp.MustCompile(`--([a-z0-9-]+) .*default:? '([^']*)'`)
	checked := 0
	for l := range strings.SplitSeq(section, "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name, documented := m[1], m[2]
		want := ""
		switch name {
		case "output-filename":
			want = opt.OutputFilename // derived from the format when the flag is empty
		case "output-format":
			// "auto": derived from the output file name, plain text without one.
			want = map[string]string{"plaintext": "txt", "markdown": "md", "xml": "xml", "arklite": "arklite"}[opt.OutputFormat.String()]
		default:
			f := opt.FlagSet.Lookup(name)
			if f == nil {
				t.Errorf("help documents --%s, which the CLI does not define", name)
				continue
			}
			want = f.DefValue
		}
		if documented != want {
			t.Errorf("help says --%s defaults to %q; the CLI uses %q", name, documented, want)
		}
		checked++
	}
	if checked < 6 {
		t.Errorf("checked %d defaults; the help format changed?", checked)
	}
}

func TestHelp_DescribesArkAndLinksItsDocumentation(t *testing.T) {
	if !strings.Contains(helpMessage, "Code intelligence engine for AI coding agents") {
		t.Error("help does not describe Ark as a code intelligence engine for AI coding agents")
	}
	if !strings.Contains(helpMessage, "https://github.com/magicdrive/ark#readme") || strings.Contains(helpMessage, "ark/README.md") {
		t.Error("help must link https://github.com/magicdrive/ark#readme")
	}
}
