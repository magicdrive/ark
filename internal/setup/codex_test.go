package setup

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// fakeCodex is a stateful stand-in for the `codex` CLI. It emits the current
// transport-nested list format and can inject per-operation failures.
type fakeCodex struct {
	installed bool
	servers   map[string]codexServer
	calls     []string
	listBad   bool // emit unparseable list output

	failRemove       bool // `codex mcp remove` fails
	addFailRemaining int  // number of upcoming `codex mcp add` calls that fail
	addNoopRemaining int  // adds that "succeed" but store nothing (→ verify fails)
}

func newFakeCodex(installed bool) *fakeCodex {
	return &fakeCodex{installed: installed, servers: map[string]codexServer{}}
}

func (f *fakeCodex) setStdio(name, command string, args []string) {
	f.servers[name] = codexServer{transportType: "stdio", command: command, args: args}
}

func (f *fakeCodex) look(name string) (string, error) {
	if name == "codex" && f.installed {
		return "/usr/local/bin/codex", nil
	}
	return "", fmt.Errorf("not found")
}

func (f *fakeCodex) run(name string, args ...string) ([]byte, []byte, int, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if len(args) < 2 || args[0] != "mcp" {
		return nil, []byte("unknown command"), 1, fmt.Errorf("unknown")
	}
	switch args[1] {
	case "list":
		if f.listBad {
			return []byte("not json"), nil, 0, nil
		}
		return f.listJSON(), nil, 0, nil
	case "add":
		return f.add(args)
	case "remove":
		if f.failRemove {
			return nil, []byte("remove boom"), 1, fmt.Errorf("remove failed")
		}
		delete(f.servers, args[2])
		return nil, nil, 0, nil
	default:
		return nil, []byte("unknown subcommand"), 1, fmt.Errorf("unknown")
	}
}

func (f *fakeCodex) listJSON() []byte {
	type transport struct {
		Type    string   `json:"type"`
		Command string   `json:"command,omitempty"`
		Args    []string `json:"args,omitempty"`
	}
	type entry struct {
		Name      string    `json:"name"`
		Enabled   bool      `json:"enabled"`
		Transport transport `json:"transport"`
	}
	var arr []entry
	for name, s := range f.servers {
		arr = append(arr, entry{
			Name:      name,
			Enabled:   true,
			Transport: transport{Type: s.transportType, Command: s.command, Args: s.args},
		})
	}
	b, _ := json.Marshal(arr)
	return b
}

func (f *fakeCodex) add(args []string) ([]byte, []byte, int, error) {
	if f.addFailRemaining > 0 {
		f.addFailRemaining--
		return nil, []byte("add boom"), 1, fmt.Errorf("add failed")
	}
	// codex mcp add <name> -- <command> <args...>
	name := args[2]
	var cmd string
	var cargs []string
	for i := 3; i < len(args); i++ {
		if args[i] == "--" {
			if i+1 < len(args) {
				cmd = args[i+1]
				cargs = append(cargs, args[i+2:]...)
			}
			break
		}
	}
	if f.addNoopRemaining > 0 {
		f.addNoopRemaining-- // simulate a silent write failure: verify will fail
		return nil, nil, 0, nil
	}
	f.servers[name] = codexServer{transportType: "stdio", command: cmd, args: cargs}
	return nil, nil, 0, nil
}

func (f *fakeCodex) addCalls() int    { return f.callCount("mcp add") }
func (f *fakeCodex) removeCalls() int { return f.callCount("mcp remove") }

func (f *fakeCodex) callCount(sub string) int {
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func TestCodex_NotInstalled_Errors(t *testing.T) {
	a := codexAdapter{runner: newFakeCodex(false)}
	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "Codex CLI") {
		t.Fatalf("expected codex-not-found error, got %v", err)
	}
}

func TestCodex_Absent_Adds(t *testing.T) {
	f := newFakeCodex(true)
	a := codexAdapter{runner: f}
	res, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.State != StateAbsent || !res.Changed {
		t.Fatalf("expected absent+changed, got %s changed=%v", res.State, res.Changed)
	}
	got := f.servers["ark"]
	if got.command != "/bin/ark" || strings.Join(got.args, " ") != "mcp-server --root /repo" {
		t.Errorf("unexpected registered entry: %+v", got)
	}
}

func TestCodex_Equivalent_NoOp(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/bin/ark", []string{"mcp-server", "--root", "/repo"})
	a := codexAdapter{runner: f}
	res, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.State != StateEquivalent || res.Changed {
		t.Fatalf("expected equivalent no-op, got %s changed=%v", res.State, res.Changed)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "mcp add") || strings.Contains(c, "mcp remove") {
			t.Errorf("no-op should not mutate, but called: %s", c)
		}
	}
}

func TestCodex_Conflict_NoForce_Errors(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server"})
	a := codexAdapter{runner: f}
	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected conflict error mentioning --force, got %v", err)
	}
	if f.servers["ark"].command != "/old/ark" {
		t.Error("conflict without --force mutated the entry")
	}
	if f.addCalls() != 0 {
		t.Error("conflict without --force must not call add")
	}
}

func TestCodex_Conflict_Force_Replaces(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server"})
	f.setStdio("other", "keep", nil)
	a := codexAdapter{runner: f}
	res, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo", Force: true})
	if err != nil {
		t.Fatalf("run --force: %v", err)
	}
	if res.State != StateConflict || !res.Changed {
		t.Fatalf("expected conflict+changed, got %s changed=%v", res.State, res.Changed)
	}
	if f.servers["ark"].command != "/bin/ark" {
		t.Error("ark not replaced")
	}
	if f.servers["other"].command != "keep" {
		t.Error("unrelated codex server damaged")
	}
}

func TestCodex_UnparseableList_Errors(t *testing.T) {
	f := newFakeCodex(true)
	f.listBad = true
	a := codexAdapter{runner: f}
	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err == nil {
		t.Fatal("expected error on unparseable list output")
	}
}

func TestCodex_UnsupportedTransport_IsConflictNotEmptyStdio(t *testing.T) {
	f := newFakeCodex(true)
	// An "ark" entry registered with a non-stdio transport.
	f.servers["ark"] = codexServer{transportType: "streamable_http"}
	a := codexAdapter{runner: f}

	// Without --force it must be a conflict (not Equivalent, not empty-stdio).
	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected conflict for non-stdio ark entry, got %v", err)
	}
	if f.removeCalls() != 0 || f.addCalls() != 0 {
		t.Error("conflict without --force must not mutate")
	}
	if f.servers["ark"].transportType != "streamable_http" {
		t.Error("existing non-stdio entry changed")
	}
}

// TestCodex_NonStdio_Force_RefusesBeforeMutation is the key safety regression:
// Ark can only restore a stdio entry, so a non-stdio existing entry must NOT be
// destroyed even with --force. The mutation must be refused up front.
func TestCodex_NonStdio_Force_RefusesBeforeMutation(t *testing.T) {
	f := newFakeCodex(true)
	f.servers["ark"] = codexServer{transportType: "streamable_http"}
	a := codexAdapter{runner: f}

	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/bin/ark", RootDir: "/repo", Force: true})
	if err == nil {
		t.Fatal("expected refusal for non-stdio entry even with --force")
	}
	if f.removeCalls() != 0 {
		t.Errorf("remove must NOT be called for non-stdio entry; calls=%v", f.calls)
	}
	if f.addCalls() != 0 {
		t.Errorf("add must NOT be called for non-stdio entry; calls=%v", f.calls)
	}
	if f.servers["ark"].transportType != "streamable_http" {
		t.Error("non-stdio entry was altered")
	}
	if !strings.Contains(err.Error(), "manually") && !strings.Contains(err.Error(), "manual") {
		t.Errorf("error should point to manual configuration, got: %v", err)
	}
}

func TestCodexArgs_Validation(t *testing.T) {
	// valid string args via the full list contract.
	ok := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark","args":["mcp-server","--root","/r"]}}]`)
	if servers, err := parseCodexList(ok); err != nil {
		t.Fatalf("valid args: %v", err)
	} else if got := servers["ark"].args; len(got) != 3 {
		t.Errorf("valid args: got %v", got)
	}

	// empty args.
	empty := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark","args":[]}}]`)
	if servers, err := parseCodexList(empty); err != nil {
		t.Fatalf("empty args: %v", err)
	} else if len(servers["ark"].args) != 0 {
		t.Errorf("empty args: got %v", servers["ark"].args)
	}

	// absent args.
	absent := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark"}}]`)
	if _, err := parseCodexList(absent); err != nil {
		t.Fatalf("absent args: %v", err)
	}

	// null args.
	null := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark","args":null}}]`)
	if _, err := parseCodexList(null); err != nil {
		t.Fatalf("null args: %v", err)
	}

	// mixed string/non-string args → error (no silent normalization).
	mixed := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark","args":["mcp-server",123,"--root","/r"]}}]`)
	if _, err := parseCodexList(mixed); err == nil {
		t.Error("mixed-type args must be a parse error, not silently normalized")
	}

	// wrong args type → error.
	wrong := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"ark","args":"mcp-server"}}]`)
	if _, err := parseCodexList(wrong); err == nil {
		t.Error("non-array args must be a parse error")
	}
}

// ---- Failure-atomicity tests (task §10) --------------------------------------

func TestCodex_Force_AddFailure_RestoresOriginal(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server", "--root", "/old"})
	f.setStdio("other", "keep", nil)
	f.addFailRemaining = 1 // the desired add fails; the restore add succeeds
	a := codexAdapter{runner: f}

	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/new/ark", RootDir: "/repo", Force: true})
	if err == nil {
		t.Fatal("expected error when add fails")
	}
	if !strings.Contains(err.Error(), "previous Ark configuration was restored") {
		t.Errorf("error should report restoration, got: %v", err)
	}
	got := f.servers["ark"]
	if got.command != "/old/ark" || strings.Join(got.args, " ") != "mcp-server --root /old" {
		t.Errorf("original Ark config not restored: %+v", got)
	}
	if f.servers["other"].command != "keep" {
		t.Error("unrelated server damaged during failed replace")
	}
}

func TestCodex_Force_VerifyFailure_RestoresOriginal(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server", "--root", "/old"})
	f.addNoopRemaining = 1 // desired add "succeeds" but stores nothing → verify fails
	a := codexAdapter{runner: f}

	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/new/ark", RootDir: "/repo", Force: true})
	if err == nil {
		t.Fatal("expected verify failure error")
	}
	if !strings.Contains(err.Error(), "verify failed") {
		t.Errorf("expected verify-failure message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "previous Ark configuration was restored") {
		t.Errorf("error should report restoration, got: %v", err)
	}
	got := f.servers["ark"]
	if got.command != "/old/ark" {
		t.Errorf("original Ark config not restored after verify failure: %+v", got)
	}
}

func TestCodex_Force_RemoveFailure_DoesNotAttemptAdd(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server"})
	f.failRemove = true
	a := codexAdapter{runner: f}

	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/new/ark", RootDir: "/repo", Force: true})
	if err == nil {
		t.Fatal("expected error when remove fails")
	}
	if f.addCalls() != 0 {
		t.Errorf("add must not be attempted after remove failure; calls=%v", f.calls)
	}
	if f.servers["ark"].command != "/old/ark" {
		t.Error("original Ark config changed despite remove failure")
	}
	if !strings.Contains(err.Error(), "left unchanged") {
		t.Errorf("error should say the previous config was left unchanged, got: %v", err)
	}
}

func TestCodex_Force_RestoreFailure_ReportsBothErrors(t *testing.T) {
	f := newFakeCodex(true)
	f.setStdio("ark", "/old/ark", []string{"mcp-server"})
	f.addFailRemaining = 2 // desired add fails AND the restore add fails
	a := codexAdapter{runner: f}

	_, err := a.run(Options{Client: ClientCodex, ArkPath: "/new/ark", RootDir: "/repo", Force: true})
	if err == nil {
		t.Fatal("expected error when both add and restore fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "failed to replace Ark MCP configuration") {
		t.Errorf("missing primary failure, got: %v", err)
	}
	if !strings.Contains(msg, "restoration of the previous Ark configuration also failed") {
		t.Errorf("missing restore-failure warning, got: %v", err)
	}
}

func TestParseCodexList_Shapes(t *testing.T) {
	// Current transport array form.
	arr := []byte(`[{"name":"ark","transport":{"type":"stdio","command":"a","args":["x"]}}]`)
	// Legacy flat object form (fallback).
	flat := []byte(`{"ark":{"command":"a","args":["x"]}}`)
	// Legacy enveloped form (fallback).
	env := []byte(`{"mcpServers":{"ark":{"command":"a","args":["x"]}}}`)

	for name, data := range map[string][]byte{"transport-array": arr, "flat": flat, "enveloped": env} {
		got, err := parseCodexList(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s, ok := got["ark"]
		if !ok {
			t.Fatalf("%s: ark not found", name)
		}
		if s.transportType != "stdio" || s.command != "a" || len(s.args) != 1 || s.args[0] != "x" {
			t.Errorf("%s: unexpected %+v", name, s)
		}
	}

	empty, err := parseCodexList([]byte("  "))
	if err != nil || len(empty) != 0 {
		t.Errorf("empty: got %v err %v", empty, err)
	}
}
