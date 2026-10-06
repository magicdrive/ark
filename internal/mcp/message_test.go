package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// Wire-level tests for inbound message classification. Every case goes through
// the real read → classify → dispatch → write path (StdioTransport.Start with
// in-memory pipes, HttpTransport.handleMCPRequest over httptest) — not through
// a hand-built MCPRequest — because the property under test lives in how a wire
// message is turned into a request or a notification.

// recorder is a request/notification handler pair that records what it was given.
type recorder struct {
	requests      []string // methods of dispatched requests
	notifications []string // methods of dispatched notifications
	panicOn       string   // notification method on which the handler panics
}

func (r *recorder) handleRequest(req *MCPRequest) *MCPResponse {
	r.requests = append(r.requests, req.Method)
	if req.Method == "tools/list" {
		return &MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": []any{}}}
	}
	return &MCPResponse{JSONRPC: "2.0", ID: req.ID, Error: &MCPError{Code: ErrorCodeMethodNotFound, Message: "Method not found: " + req.Method}}
}

func (r *recorder) handleNotification(n *MCPNotification) {
	r.notifications = append(r.notifications, n.Method)
	if n.Method == r.panicOn {
		panic("handler failure")
	}
}

// quietLog silences the diagnostic log (stderr) for the duration of a test and
// returns what was logged.
func quietLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

// serveStdio feeds input (newline-separated messages) to a stdio transport and
// returns the raw bytes written to stdout.
func serveStdio(t *testing.T, input string, h RequestHandler, n NotificationHandler) string {
	t.Helper()
	quietLog(t)
	var out bytes.Buffer
	tr := (&StdioTransport{in: strings.NewReader(input), out: &out}).WithNotificationHandler(n)
	if err := tr.Start(h); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return out.String()
}

// decodeLines parses stdout: every non-empty line must be one JSON object.
func decodeLines(t *testing.T, out string) []map[string]json.RawMessage {
	t.Helper()
	var msgs []map[string]json.RawMessage
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not a JSON object (stdout must carry protocol messages only): %q", line)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func errCode(t *testing.T, m map[string]json.RawMessage) int {
	t.Helper()
	var e struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(m["error"], &e); err != nil {
		t.Fatalf("response has no error object: %v", m)
	}
	return e.Code
}

func idOf(m map[string]json.RawMessage) string { return string(m["id"]) }

// --- classification: the cases that decide everything ------------------------

func TestWire_Classification(t *testing.T) {
	type want struct {
		responses int    // exactly this many responses on stdout
		code      int    // error code when responses == 1 and the response is an error (0 = result)
		id        string // raw id of that response ("" = not checked)
		requests  int    // requests dispatched to the request handler
	}
	cases := []struct {
		name  string
		input string
		want  want
	}{
		// requests
		{"known request", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, want{1, 0, "1", 1}},
		{"known request, string id", `{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`, want{1, 0, `"abc"`, 1}},
		{"unknown request", `{"jsonrpc":"2.0","id":7,"method":"nope"}`, want{1, ErrorCodeMethodNotFound, "7", 1}},
		{"unknown request, string id preserved", `{"jsonrpc":"2.0","id":"x-1","method":"nope"}`, want{1, ErrorCodeMethodNotFound, `"x-1"`, 1}},
		{"id echoed exactly (beyond float64)", `{"jsonrpc":"2.0","id":12345678901234567890,"method":"tools/list"}`, want{1, 0, "12345678901234567890", 1}},
		{"id 0 is an id", `{"jsonrpc":"2.0","id":0,"method":"tools/list"}`, want{1, 0, "0", 1}},
		{"empty-string id is an id", `{"jsonrpc":"2.0","id":"","method":"tools/list"}`, want{1, 0, `""`, 1}},

		// notifications: never a response, whatever they are
		{"notifications/initialized", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, want{}},
		{"notifications/cancelled", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`, want{}},
		{"notifications/progress", `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t","progress":1}}`, want{}},
		{"notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed"}`, want{}},
		{"unknown notification", `{"jsonrpc":"2.0","method":"notifications/whatever"}`, want{}},
		{"notification with invalid params (string)", `{"jsonrpc":"2.0","method":"notifications/initialized","params":"nope"}`, want{}},
		{"notification with invalid params (number)", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":42}`, want{}},
		{"notification with array params", `{"jsonrpc":"2.0","method":"notifications/progress","params":[1,2]}`, want{}},
		{"unknown notification with invalid params", `{"jsonrpc":"2.0","method":"x/y","params":"nope"}`, want{}},
		{"a request method sent without id is not executed", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_file_content","arguments":{"path":"x"}}}`, want{}},
		{"initialize without id is not executed", `{"jsonrpc":"2.0","method":"initialize","params":{}}`, want{}},
		{"notification without jsonrpc member", `{"method":"notifications/initialized"}`, want{}},

		// explicit null id: NOT a notification
		{"explicit id null, known method", `{"jsonrpc":"2.0","id":null,"method":"tools/list"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"explicit id null, unknown method", `{"jsonrpc":"2.0","id":null,"method":"nope"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"explicit id null, notification-looking method", `{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"id of boolean type", `{"jsonrpc":"2.0","id":true,"method":"tools/list"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"id of object type", `{"jsonrpc":"2.0","id":{"a":1},"method":"tools/list"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"id of array type", `{"jsonrpc":"2.0","id":[1],"method":"tools/list"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},

		// parse errors keep their response
		{"malformed JSON", `{"jsonrpc":"2.0","id":1,"method":`, want{1, ErrorCodeParseError, "null", 0}},
		{"malformed JSON that looks like a notification", `{"jsonrpc":"2.0","method":"notifications/initialized"`, want{1, ErrorCodeParseError, "null", 0}},
		{"not JSON", `hello`, want{1, ErrorCodeParseError, "null", 0}},

		// valid JSON that is not a message object
		{"number", `123`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"string", `"hello"`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"null", `null`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"boolean", `true`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"empty array", `[]`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"batch is valid JSON, not a parse error, and is not executed", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, want{1, ErrorCodeInvalidRequest, "null", 0}},

		// invalid envelopes are answered, never swallowed as notifications
		{"missing method, id present", `{"jsonrpc":"2.0","id":1}`, want{1, ErrorCodeInvalidRequest, "1", 0}},
		{"missing method, no id", `{"jsonrpc":"2.0"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"empty object", `{}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"method of number type", `{"jsonrpc":"2.0","id":1,"method":123}`, want{1, ErrorCodeInvalidRequest, "1", 0}},
		{"method of number type, no id", `{"jsonrpc":"2.0","method":123}`, want{1, ErrorCodeInvalidRequest, "null", 0}},
		{"empty method", `{"jsonrpc":"2.0","id":1,"method":""}`, want{1, ErrorCodeInvalidRequest, "1", 0}},
		{"method together with result", `{"jsonrpc":"2.0","id":1,"method":"tools/list","result":{}}`, want{1, ErrorCodeInvalidRequest, "1", 0}},
		{"members are matched case-sensitively", `{"jsonrpc":"2.0","ID":1,"Method":"tools/list"}`, want{1, ErrorCodeInvalidRequest, "null", 0}},

		// responses from the client are recognised and never answered
		{"client response (result)", `{"jsonrpc":"2.0","id":5,"result":{}}`, want{}},
		{"client response (error)", `{"jsonrpc":"2.0","id":5,"error":{"code":-32000,"message":"x"}}`, want{}},
		{"client response with string id", `{"jsonrpc":"2.0","id":"s","result":{"ok":true}}`, want{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			out := serveStdio(t, c.input+"\n", rec.handleRequest, rec.handleNotification)
			msgs := decodeLines(t, out)
			if len(msgs) != c.want.responses {
				t.Fatalf("responses = %d, want %d; stdout=%q", len(msgs), c.want.responses, out)
			}
			if len(rec.requests) != c.want.requests {
				t.Errorf("dispatched requests = %v, want %d", rec.requests, c.want.requests)
			}
			if len(msgs) == 1 {
				m := msgs[0]
				if got := string(m["jsonrpc"]); got != `"2.0"` {
					t.Errorf("jsonrpc = %s", got)
				}
				if c.want.id != "" && idOf(m) != c.want.id {
					t.Errorf("id = %s, want %s", idOf(m), c.want.id)
				}
				if c.want.code != 0 {
					if got := errCode(t, m); got != c.want.code {
						t.Errorf("error code = %d, want %d", got, c.want.code)
					}
				} else if _, hasErr := m["error"]; hasErr {
					t.Errorf("unexpected error response: %v", m)
				}
			}
		})
	}
}

// Missing id and explicit null id are different on the wire and are classified
// differently, even though both decode to nil in Go.
func TestClassify_MissingIDIsNotNullID(t *testing.T) {
	if k := classifyMessage([]byte(`{"jsonrpc":"2.0","method":"m"}`)).kind; k != messageNotification {
		t.Errorf("missing id: kind = %v, want notification", k)
	}
	if k := classifyMessage([]byte(`{"jsonrpc":"2.0","id":null,"method":"m"}`)).kind; k != messageInvalid {
		t.Errorf("id null: kind = %v, want invalid", k)
	}
	if k := classifyMessage([]byte(`{"jsonrpc":"2.0","id":0,"method":"m"}`)).kind; k != messageRequest {
		t.Errorf("id 0: kind = %v, want request", k)
	}
}

// --- the invariant: a notification NEVER produces a response -----------------

func TestWire_NotificationNeverProducesResponse(t *testing.T) {
	methods := []string{
		// request methods, sent as notifications
		"initialize", "tools/list", "tools/call", "resources/list", "resources/read", "ping",
		// notifications Ark knows
		"notifications/initialized", "notifications/cancelled", "notifications/progress", "notifications/roots/list_changed",
		// notifications Ark does not know, and arbitrary names
		"notifications/tools/list_changed", "notifications/", "notifications", "unknown", "a/b/c",
		"rpc.reserved", "ünïcode/メソッド", "with space", strings.Repeat("m", 5000), "\u0000", "tools/list ",
	}
	paramses := []string{``, `"params":{}`, `"params":[]`, `"params":null`, `"params":"x"`, `"params":42`, `"params":{"a":{"b":[1,2,{"c":null}]}}`}

	rec := &recorder{}
	var in strings.Builder
	sent := 0
	for _, m := range methods {
		mj, _ := json.Marshal(m)
		for _, p := range paramses {
			msg := `{"jsonrpc":"2.0","method":` + string(mj)
			if p != "" {
				msg += "," + p
			}
			in.WriteString(msg + "}\n")
			sent++
		}
	}
	out := serveStdio(t, in.String(), rec.handleRequest, rec.handleNotification)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("%d notifications produced output: %q", sent, out)
	}
	if len(rec.requests) != 0 {
		t.Errorf("notifications must never reach the request handler, got %v", rec.requests)
	}
	if len(rec.notifications) != sent {
		t.Errorf("notification handler saw %d of %d notifications", len(rec.notifications), sent)
	}
}

// A failing notification handler can not leak a response, and the server keeps
// serving.
func TestWire_NotificationHandlerFailureProducesNoResponse(t *testing.T) {
	rec := &recorder{panicOn: "notifications/initialized"}
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":9,"method":"tools/list"}` + "\n"
	msgs := decodeLines(t, serveStdio(t, input, rec.handleRequest, rec.handleNotification))
	if len(msgs) != 1 || idOf(msgs[0]) != "9" {
		t.Fatalf("want exactly the tools/list response (id 9), got %v", msgs)
	}
}

// With no notification handler at all, notifications are still accepted silently.
func TestWire_NotificationWithoutHandlerIsAcceptedSilently(t *testing.T) {
	rec := &recorder{}
	out := serveStdio(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n", rec.handleRequest, nil)
	if strings.TrimSpace(out) != "" {
		t.Errorf("stdout = %q", out)
	}
}

// FuzzRouteNotificationInvariant checks, against an independent reading of the
// wire form, that nothing shaped like a notification ever yields a response and
// that nothing — whatever its bytes — panics or yields a malformed response.
func FuzzRouteNotificationInvariant(f *testing.F) {
	for _, s := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":5,"result":{}}`,
		`{"method":"x","params":"p"}`, `[]`, `123`, `null`, `{`, ``, `{"id":"a"}`, `{"ID":1,"method":"m"}`,
	} {
		f.Add([]byte(s))
	}
	log.SetOutput(io.Discard)
	f.Fuzz(func(t *testing.T, raw []byte) {
		var calls int
		rt := router{request: func(r *MCPRequest) *MCPResponse {
			calls++
			return &MCPResponse{JSONRPC: "2.0", ID: r.ID, Result: map[string]any{}}
		}}
		resp := rt.route(raw)

		// Independent oracle: is this, on the wire, a notification?
		var members map[string]json.RawMessage
		isNotification := false
		if json.Unmarshal(raw, &members) == nil && members != nil {
			var method string
			_, hasID := members["id"]
			_, hasResult := members["result"]
			_, hasError := members["error"]
			if json.Unmarshal(members["method"], &method) == nil && method != "" && !hasID && !hasResult && !hasError {
				isNotification = true
			}
		}
		if isNotification && (resp != nil || calls != 0) {
			t.Fatalf("notification %q produced a response or reached the request handler", raw)
		}
		if resp != nil {
			if resp.JSONRPC != "2.0" {
				t.Fatalf("response without jsonrpc 2.0 for %q", raw)
			}
			if resp.Result != nil && resp.Error != nil {
				t.Fatalf("response with both result and error for %q", raw)
			}
			if _, err := json.Marshal(resp); err != nil {
				t.Fatalf("response not serializable for %q: %v", raw, err)
			}
		}
	})
}

// --- session level -----------------------------------------------------------

func testServer(t *testing.T) *MCPServer {
	t.Helper()
	opt := createTestServerOption()
	opt.RootDir = t.TempDir()
	opt.NoCache = true
	return NewMCPServer(opt.RootDir, opt)
}

// serveSession runs input through a transport wired like RunMCPServe wires it.
func serveSession(t *testing.T, s *MCPServer, input string) []map[string]json.RawMessage {
	t.Helper()
	return decodeLines(t, serveStdio(t, input, s.processRequest, s.processNotification))
}

// The Cursor sequence: initialize → notifications/initialized → tools/list.
func TestSession_InitializeNotifyList(t *testing.T) {
	s := testServer(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cursor","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	msgs := serveSession(t, s, input)

	if len(msgs) != 2 {
		t.Fatalf("response count = %d, want 2 (initialize, tools/list); got %v", len(msgs), msgs)
	}
	if idOf(msgs[0]) != "1" || idOf(msgs[1]) != "2" {
		t.Errorf("response ids = %s, %s; want 1, 2", idOf(msgs[0]), idOf(msgs[1]))
	}
	for i, m := range msgs {
		if _, ok := m["result"]; !ok {
			t.Errorf("response %d is not a result: %v", i, m)
		}
		if idOf(m) == "null" {
			t.Errorf("response %d has id null", i)
		}
	}
}

// Notifications of every kind interleaved with requests: requests after them are
// still served normally and in order.
func TestSession_RequestsAfterNotificationsStillServed(t *testing.T) {
	s := testServer(t)
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1,"progress":0.5}}`,
		`{"jsonrpc":"2.0","method":"notifications/roots/list_changed"}`,
		`{"jsonrpc":"2.0","method":"notifications/never-heard-of-it"}`,
		`{"jsonrpc":"2.0","id":"three","method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","id":5,"result":{}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/list"}`,
	}
	msgs := serveSession(t, s, strings.Join(lines, "\n")+"\n")

	var ids []string
	for _, m := range msgs {
		ids = append(ids, idOf(m))
	}
	if want := []string{"1", "2", `"three"`, "4", "6"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("response ids = %v, want %v", ids, want)
	}
	if got := errCode(t, msgs[3]); got != ErrorCodeMethodNotFound {
		t.Errorf("unknown request error code = %d", got)
	}
}

// stdout carries protocol messages only, including for the error paths.
func TestSession_StdoutIsProtocolOnly(t *testing.T) {
	s := testServer(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/unknown"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{ broken`,
		`123`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/list"}`,
	}, "\n") + "\n"
	out := serveStdio(t, input, s.processRequest, s.processNotification)
	msgs := decodeLines(t, out) // fails on any non-JSON line
	if len(msgs) != 5 {         // initialize, tools/list, parse error, invalid (123), invalid (null id)
		t.Fatalf("responses = %d, want 5; stdout=%q", len(msgs), out)
	}
	for _, m := range msgs {
		if string(m["jsonrpc"]) != `"2.0"` {
			t.Errorf("not a JSON-RPC 2.0 message: %v", m)
		}
	}
}

// Diagnostics go to stderr (the log), never to stdout.
func TestSession_DiagnosticsGoToStderrNotStdout(t *testing.T) {
	s := testServer(t)
	var logged bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(old)

	var out bytes.Buffer
	tr := (&StdioTransport{
		in:  strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/unsupported"}` + "\n" + `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"),
		out: &out,
	}).WithNotificationHandler(s.processNotification)
	if err := tr.Start(s.processRequest); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(logged.String(), "unsupported") || !strings.Contains(logged.String(), "unsolicited response") {
		t.Errorf("expected diagnostics in the log, got %q", logged.String())
	}
}

// --- HTTP, and parity with stdio ---------------------------------------------

func postHTTP(t *testing.T, tr *HttpTransport, h RequestHandler, body string) (int, string) {
	t.Helper()
	quietLog(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	tr.handleMCPRequest(rr, req, h)
	return rr.Code, rr.Body.String()
}

func TestHTTP_NotificationIs202WithNoBody(t *testing.T) {
	rec := &recorder{}
	tr := NewHttpTransport("localhost", "0").WithNotificationHandler(rec.handleNotification)
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`,
		`{"jsonrpc":"2.0","method":"notifications/unknown"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":"invalid"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`, // a request method without id is a notification
		`{"jsonrpc":"2.0","id":5,"result":{}}`,    // client response
	} {
		status, got := postHTTP(t, tr, rec.handleRequest, body)
		if status != http.StatusAccepted || got != "" {
			t.Errorf("%s\n  → status %d body %q, want 202 and empty body (never `null`, never a JSON-RPC response)", body, status, got)
		}
	}
	if len(rec.requests) != 0 {
		t.Errorf("request handler was called for notifications/responses: %v", rec.requests)
	}
}

func TestHTTP_RequestsStillGetResponses(t *testing.T) {
	rec := &recorder{}
	tr := NewHttpTransport("localhost", "0")

	status, body := postHTTP(t, tr, rec.handleRequest, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var ok struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &ok) != nil || ok.ID != 1 || ok.Result == nil {
		t.Errorf("known request → %d %q", status, body)
	}

	status, body = postHTTP(t, tr, rec.handleRequest, `{"jsonrpc":"2.0","id":"u","method":"nope"}`)
	var bad struct {
		ID    string `json:"id"`
		Error struct{ Code int }
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &bad) != nil || bad.ID != "u" || bad.Error.Code != ErrorCodeMethodNotFound {
		t.Errorf("unknown request → %d %q", status, body)
	}

	status, body = postHTTP(t, tr, rec.handleRequest, `{ broken`)
	if status != http.StatusOK || !strings.Contains(body, `"code":-32700`) || !strings.Contains(body, `"id":null`) {
		t.Errorf("parse error → %d %q", status, body)
	}
}

// stdio and HTTP share one classifier: for every input, "no response on stdio"
// is exactly "202 with no body on HTTP", and otherwise both carry the same
// response.
func TestStdioAndHTTP_ClassifyIdentically(t *testing.T) {
	inputs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":"a","method":"nope"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/whatever","params":{"x":1}}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":null,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":{},"method":"tools/list"}`,
		`{ broken`, `123`, `null`, `[]`, `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`,
		`{"jsonrpc":"2.0"}`, `{"jsonrpc":"2.0","id":1}`, `{"jsonrpc":"2.0","id":1,"method":7}`,
		`{"jsonrpc":"2.0","id":5,"result":{}}`, `{"jsonrpc":"2.0","id":5,"error":{"code":1,"message":"m"}}`,
	}
	for _, in := range inputs {
		stdioRec, httpRec := &recorder{}, &recorder{}
		stdioOut := strings.TrimSpace(serveStdio(t, in+"\n", stdioRec.handleRequest, stdioRec.handleNotification))
		tr := NewHttpTransport("localhost", "0").WithNotificationHandler(httpRec.handleNotification)
		status, body := postHTTP(t, tr, httpRec.handleRequest, in)

		if stdioOut == "" {
			if status != http.StatusAccepted || body != "" {
				t.Errorf("%s\n  stdio: no response; HTTP: %d %q", in, status, body)
			}
		} else if status != http.StatusOK || strings.TrimSpace(body) != stdioOut {
			t.Errorf("%s\n  stdio: %s\n  HTTP : %d %s", in, stdioOut, status, strings.TrimSpace(body))
		}
		if !reflect.DeepEqual(stdioRec.requests, httpRec.requests) || !reflect.DeepEqual(stdioRec.notifications, httpRec.notifications) {
			t.Errorf("%s\n  dispatch differs: stdio %v/%v, HTTP %v/%v", in, stdioRec.requests, stdioRec.notifications, httpRec.requests, httpRec.notifications)
		}
	}
}

// A notification handler is invoked once per notification, on both transports.
func TestNotificationHandlerIsInvoked(t *testing.T) {
	var n atomic.Int32
	h := func(*MCPNotification) { n.Add(1) }
	rec := &recorder{}
	serveStdio(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n", rec.handleRequest, h)
	tr := NewHttpTransport("localhost", "0").WithNotificationHandler(h)
	postHTTP(t, tr, rec.handleRequest, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if n.Load() != 2 {
		t.Errorf("notification handler invoked %d times, want 2", n.Load())
	}
}
