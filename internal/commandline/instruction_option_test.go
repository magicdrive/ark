package commandline

import "testing"

func TestInstructionOptParse(t *testing.T) {
	_, opt, err := InstructionOptParse([]string{"claude"})
	if err != nil || opt.Target != "claude" || opt.HelpFlag {
		t.Fatalf("claude: %+v %v", opt, err)
	}
	_, opt, err = InstructionOptParse(nil)
	if err != nil || opt.Target != "" {
		t.Fatalf("no target must parse (the caller reports it): %+v %v", opt, err)
	}
	_, opt, err = InstructionOptParse([]string{"--help"})
	if err != nil || !opt.HelpFlag || opt.Target != "" {
		t.Fatalf("--help: %+v %v", opt, err)
	}
	_, opt, err = InstructionOptParse([]string{"claude", "-h"})
	if err != nil || !opt.HelpFlag || opt.Target != "claude" {
		t.Fatalf("claude -h: %+v %v", opt, err)
	}
	if _, _, err := InstructionOptParse([]string{"claude", "extra"}); err == nil {
		t.Error("an extra operand must be an error")
	}
	// No installing/writing flags exist (generator, not installer).
	_, opt, _ = InstructionOptParse(nil)
	for _, name := range []string{"install", "write", "append", "force", "global"} {
		if opt.FlagSet.Lookup(name) != nil {
			t.Errorf("--%s must not exist", name)
		}
	}
}
