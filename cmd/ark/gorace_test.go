package ark

import (
	"strings"
	"testing"
)

// The GORACE the tests give their processes, for the race report path of a
// Windows runner: a drive colon, backslashes and a short name, all inside the
// quoted log_path value (tests/integration also runs it through the race
// detector itself).
func TestGoraceFor_WindowsPath(t *testing.T) {
	const report = `C:\Users\RUNNER~1\AppData\Local\Temp\ark-cmdtest-race-123\report`
	for _, tc := range []struct{ caller, want string }{
		{"", `atexit_sleep_ms=0 log_path="` + report + `"`},
		{" halt_on_error=1 ", `atexit_sleep_ms=0 halt_on_error=1 log_path="` + report + `"`},
	} {
		if got, err := goraceFor(tc.caller, report); err != nil || got != tc.want {
			t.Errorf("goraceFor(%q):\n got %s (%v)\nwant %s", tc.caller, got, err, tc.want)
		}
	}
	if got, err := goraceFor("", `C:\a"b\report`); err == nil || strings.Contains(got, `a"b`) {
		t.Errorf("a double quote in the path must be refused, got %s", got)
	}
}
