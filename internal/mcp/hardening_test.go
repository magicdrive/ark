package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Wire-level tests for the v4.3.1 boundary hardening: request panic containment,
// ping, message size limits, response marshal failure and HTTP origin/host/
// content-type/body checks.

// panicHandler panics for method "boom" and otherwise behaves like recorder.
func panicHandler(rec *recorder) RequestHandler {
	return func(req *MCPRequest) *MCPResponse {
		if req.Method == "boom" {
			panic("secret internal detail")
		}
		return rec.handleRequest(req)
	}
}

func TestStdio_RequestPanicIsolated(t *testing.T) {
	rec := &recorder{}
	out := serveStdio(t,
		`{"jsonrpc":"2.0","id":1,"method":"boom"}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n",
		panicHandler(rec), rec.handleNotification)
	msgs := decodeLines(t, out)
	if len(msgs) != 2 {
		t.Fatalf("want 2 responses, got %d: %s", len(msgs), out)
	}
	if string(msgs[0]["id"]) != "1" || !strings.Contains(string(msgs[0]["error"]), `"code":-32603`) {
		t.Errorf("request 1: want -32603 for id 1, got %s", out)
	}
	if strings.Contains(out, "secret internal detail") {
		t.Errorf("panic detail leaked to the wire: %s", out)
	}
	if string(msgs[1]["id"]) != "2" || msgs[1]["result"] == nil {
		t.Errorf("request 2 must succeed after a panic, got %s", msgs[1])
	}
}

func TestHTTP_RequestPanicIsolated(t *testing.T) {
	rec := &recorder{}
	tr := NewHttpTransport("localhost", "0").WithNotificationHandler(rec.handleNotification)
	status, body := postHTTP(t, tr, panicHandler(rec), `{"jsonrpc":"2.0","id":"x","method":"boom"}`)
	if status != http.StatusOK || !strings.Contains(body, `"id":"x"`) || !strings.Contains(body, `"code":-32603`) {
		t.Fatalf("panicking request: status=%d body=%s", status, body)
	}
	if strings.Contains(body, "secret internal detail") {
		t.Errorf("panic detail leaked: %s", body)
	}
	status, body = postHTTP(t, tr, panicHandler(rec), `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if status != http.StatusOK || !strings.Contains(body, `"result"`) {
		t.Errorf("following request: status=%d body=%s", status, body)
	}
}

// A panicking notification handler still yields zero responses, and a request
// panic recovery does not turn a notification into an error response.
func TestNotificationPanicStillNoResponse(t *testing.T) {
	rec := &recorder{panicOn: "boom"}
	out := serveStdio(t,
		`{"jsonrpc":"2.0","method":"boom"}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n",
		panicHandler(rec), rec.handleNotification)
	msgs := decodeLines(t, out)
	if len(msgs) != 1 || string(msgs[0]["id"]) != "2" {
		t.Fatalf("want exactly the response to id 2, got %s", out)
	}
}

func newTestServer(t *testing.T) *MCPServer {
	t.Helper()
	root := t.TempDir()
	if err := writeFile(root+"/a.go", "package a\nfunc F() {}\n"); err != nil {
		t.Fatal(err)
	}
	opt := createTestServerOption()
	opt.RootDir = root
	opt.NoCache = true
	return NewMCPServer(root, opt)
}

func TestPing_ResultIsEmptyObject(t *testing.T) {
	srv := newTestServer(t)
	for _, id := range []string{`123`, `"abc"`} {
		out := serveStdio(t, `{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`+"\n", srv.processRequest, nil)
		want := `{"jsonrpc":"2.0","id":` + id + `,"result":{}}`
		if strings.TrimSpace(out) != want {
			t.Errorf("ping id %s:\n got %s\nwant %s", id, strings.TrimSpace(out), want)
		}
	}
}

func TestToolsCall_MaxFilesNonPositive(t *testing.T) {
	srv := newTestServer(t)
	for _, n := range []string{"-1", "0"} {
		out := serveStdio(t,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_files_arklite","arguments":{"paths":["a.go"],"maxFiles":`+n+`}}}`+"\n"+
				`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n",
			srv.processRequest, nil)
		msgs := decodeLines(t, out)
		if len(msgs) != 2 || msgs[0]["result"] == nil || msgs[0]["error"] != nil {
			t.Errorf("maxFiles=%s: want a normal tool result then tools/list, got %.300s", n, out)
		}
		if !strings.Contains(string(msgs[0]["result"]), "a.go") {
			t.Errorf("maxFiles=%s: default limit should still include a.go: %s", n, msgs[0]["result"])
		}
	}
}

func TestStdio_LargeMessage(t *testing.T) {
	rec := &recorder{}
	pad := strings.Repeat("x", 1<<20)
	out := serveStdio(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"`+pad+`"}}`+"\n", rec.handleRequest, nil)
	if msgs := decodeLines(t, out); len(msgs) != 1 || msgs[0]["result"] == nil {
		t.Fatalf("1 MiB message must be processed, got %.200s", out)
	}
}

func TestStdio_OversizeMessageTerminatesSafely(t *testing.T) {
	quietLog(t)
	rec := &recorder{}
	var out bytes.Buffer
	pad := strings.Repeat("x", maxMessageBytes+1)
	tr := &StdioTransport{in: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":"` + pad + `"}` + "\n"), out: &out}
	err := tr.Start(rec.handleRequest)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want a clear 'exceeds' error, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("nothing may be written to stdout, got %.100s", out.String())
	}
}

func TestMarshalFailure(t *testing.T) {
	// A channel cannot be marshaled: the request still gets -32603 for its id.
	h := func(req *MCPRequest) *MCPResponse {
		return &MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: make(chan int)}
	}
	out := serveStdio(t, `{"jsonrpc":"2.0","id":7,"method":"x"}`+"\n"+`{"jsonrpc":"2.0","id":8,"method":"x"}`+"\n", h, nil)
	msgs := decodeLines(t, out)
	if len(msgs) != 2 || string(msgs[0]["id"]) != "7" || !strings.Contains(string(msgs[0]["error"]), "-32603") {
		t.Fatalf("stdio: want -32603 for each request, got %s", out)
	}

	tr := NewHttpTransport("localhost", "0")
	status, body := postHTTP(t, tr, h, `{"jsonrpc":"2.0","id":7,"method":"x"}`)
	if status != http.StatusOK || !strings.Contains(body, `"id":7`) || !strings.Contains(body, "-32603") {
		t.Errorf("HTTP: status=%d body=%s", status, body)
	}
}

func TestMarshalFailure_FallbackFailureIsBounded(t *testing.T) {
	quietLog(t)
	// An unmarshalable id makes the fallback fail too: stop, do not retry.
	bad := &MCPResponse{JSONRPC: "2.0", ID: make(chan int), Result: "ok"}
	if _, err := encodeResponse(bad); err == nil {
		t.Fatal("want an error when the fallback cannot be marshaled either")
	}
	var out bytes.Buffer
	tr := &StdioTransport{in: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"x"}` + "\n"), out: &out}
	if err := tr.Start(func(*MCPRequest) *MCPResponse { return bad }); err == nil {
		t.Error("stdio must fail safely")
	}
	if out.Len() != 0 {
		t.Errorf("nothing may be written, got %q", out.String())
	}
	status, _ := postHTTP(t, NewHttpTransport("localhost", "0"), func(*MCPRequest) *MCPResponse { return bad }, `{"jsonrpc":"2.0","id":1,"method":"x"}`)
	if status != http.StatusInternalServerError {
		t.Errorf("HTTP: want 500, got %d", status)
	}
}

// httpDo sends one request through handleMCPRequest.
func httpDo(t *testing.T, method, host, contentType string, headers map[string]string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	quietLog(t)
	rec := &recorder{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/mcp", body)
	req.Host = host
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	NewHttpTransport("localhost", "0").handleMCPRequest(rr, req, rec.handleRequest)
	return rr
}

const listBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

func TestHTTP_Origin(t *testing.T) {
	cases := map[string]int{
		"":                              200,
		"http://localhost:1234":         200,
		"https://localhost:1234":        200,
		"http://localhost":              200,
		"http://127.0.0.1:1234":         200,
		"https://127.0.0.1":             200,
		"http://[::1]:1234":             200,
		"https://[::1]":                 200,
		"https://evil.example":          403,
		"https://example.com":           403,
		"http://localhost.evil.example": 403,
		"http://127.0.0.1.evil.example": 403,
		"http://evil.example/localhost": 403,
		"http://localhost@evil.example": 403,
		"null":                          403,
		"file://localhost":              403,
	}
	for origin, want := range cases {
		hdr := map[string]string{}
		if origin != "" {
			hdr["Origin"] = origin
		}
		rr := httpDo(t, http.MethodPost, "localhost:8522", "application/json", hdr, strings.NewReader(listBody))
		if rr.Code != want {
			t.Errorf("Origin %q: got %d, want %d", origin, rr.Code, want)
		}
	}
}

func TestHTTP_Host(t *testing.T) {
	cases := map[string]int{
		"localhost":              200,
		"localhost:8522":         200,
		"LOCALHOST:8522":         200,
		"127.0.0.1":              200,
		"127.0.0.1:8522":         200,
		"[::1]":                  200,
		"[::1]:8522":             200,
		"evil.example":           403,
		"evil.example:8522":      403,
		"localhost.evil.example": 403,
		"192.168.1.5:8522":       403,
		"":                       403,
	}
	for host, want := range cases {
		rr := httpDo(t, http.MethodPost, host, "application/json", nil, strings.NewReader(listBody))
		if rr.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, rr.Code, want)
		}
	}
}

func TestHTTP_PreflightChecked(t *testing.T) {
	rr := httpDo(t, http.MethodOptions, "localhost:8522", "", map[string]string{"Origin": "https://evil.example"}, nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("evil preflight: got %d, want 403", rr.Code)
	}
	rr = httpDo(t, http.MethodOptions, "evil.example", "", nil, nil)
	if rr.Code != http.StatusForbidden {
		t.Errorf("rebinding preflight: got %d, want 403", rr.Code)
	}
}

func TestHTTP_ContentType(t *testing.T) {
	cases := map[string]int{
		"application/json":                  200,
		"application/json; charset=utf-8":   200,
		"Application/JSON":                  200,
		"text/plain":                        415,
		"application/x-www-form-urlencoded": 415,
		"multipart/form-data; boundary=x":   415,
		"":                                  415,
		"application/jsonx":                 415,
	}
	for ct, want := range cases {
		rr := httpDo(t, http.MethodPost, "localhost:8522", ct, nil, strings.NewReader(listBody))
		if rr.Code != want {
			t.Errorf("Content-Type %q: got %d, want %d", ct, rr.Code, want)
		}
	}
}

func TestHTTP_NoWildcardCORS(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodOptions} {
		rr := httpDo(t, m, "localhost:8522", "application/json", map[string]string{"Origin": "http://localhost:1"}, strings.NewReader(listBody))
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want none", m, got)
		}
	}
}

// Over a real listener: oversize body → 413, then the server still answers.
func TestHTTP_BodyLimitAndSurvival(t *testing.T) {
	quietLog(t)
	rec := &recorder{}
	tr := NewHttpTransport("localhost", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.handleMCPRequest(w, r, rec.handleRequest)
	}))
	defer srv.Close()

	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":"` + strings.Repeat("x", maxMessageBytes) + `"}`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: got %d, want 413", resp.StatusCode)
	}

	resp, err = http.Post(srv.URL, "application/json", strings.NewReader(listBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != 200 || got.Result == nil {
		t.Errorf("following request: status=%d err=%v resp=%+v", resp.StatusCode, err, got)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
