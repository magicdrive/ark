package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
)

// Inbound message classification (JSON-RPC 2.0 as used by MCP).
//
// A transport hands each raw inbound message (one stdio line, one HTTP body) to
// router.route. The message is first classified from its wire form, and only
// then dispatched:
//
//	request          id member present, string or number  → dispatch → result/error response
//	notification     id member ABSENT, valid method       → dispatch (side effects only) → NEVER a response
//	client response  no method; result or error present   → recognised, logged, NEVER answered
//	invalid          anything else                        → one error response (-32700 / -32600)
//
// The decisive property is that classification looks at which members are
// present on the wire, not at decoded values: a missing "id" and an explicit
// `"id": null` both decode to nil in Go, but the first is a notification and the
// second is an invalid request (MCP forbids null ids). So the envelope is read
// as a map of raw members before anything is decoded into a typed message.
//
// A notification can never produce a response by construction: the notification
// branch of route has no response to return, and NotificationHandler returns
// nothing, so neither the method name, the params, nor a failing handler can
// leak one.

// MCPNotification is a classified JSON-RPC notification: a request without an id.
type MCPNotification struct {
	Method string
	Params interface{}
}

// NotificationHandler receives a notification for its side effects. It has no
// way to return a response.
type NotificationHandler func(notification *MCPNotification)

type messageKind int

const (
	messageInvalid messageKind = iota
	messageRequest
	messageNotification
	messageClientResponse
)

// inboundMessage is the result of classifying one raw message.
type inboundMessage struct {
	kind         messageKind
	request      *MCPRequest      // messageRequest
	notification *MCPNotification // messageNotification
	errResponse  *MCPResponse     // messageInvalid: the single error response to send
}

// classifyMessage classifies one raw inbound JSON-RPC message.
func classifyMessage(raw []byte) inboundMessage {
	// Not JSON at all: Parse error. The id cannot be known, so it is null.
	var anything interface{}
	if err := json.Unmarshal(raw, &anything); err != nil {
		return invalidMessage(nil, ErrorCodeParseError, "Parse error", err.Error())
	}

	// Valid JSON that is not an object (a number, a string, null, or an array —
	// batches are not supported) is not a JSON-RPC message object.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return invalidMessage(nil, ErrorCodeInvalidRequest, "Invalid Request", "a JSON-RPC message must be a JSON object")
	}

	// Members are read raw and by exact (case-sensitive) name, so presence and
	// JSON null stay distinguishable.
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return invalidMessage(nil, ErrorCodeInvalidRequest, "Invalid Request", err.Error())
	}
	idRaw, hasID := members["id"]
	methodRaw, hasMethod := members["method"]
	_, hasResult := members["result"]
	_, hasError := members["error"]

	// id for an error response: the request's own id when it is a valid one,
	// null when it cannot be determined.
	var errID interface{}
	if hasID {
		errID, _ = decodeID(idRaw)
	}

	if !hasMethod {
		if hasResult || hasError {
			// A response from the client. Ark sends no requests of its own, so
			// there is nothing to correlate it with — and a response is never
			// answered.
			return inboundMessage{kind: messageClientResponse}
		}
		return invalidMessage(errID, ErrorCodeInvalidRequest, "Invalid Request", "missing method")
	}

	var method string
	if err := json.Unmarshal(methodRaw, &method); err != nil || method == "" {
		return invalidMessage(errID, ErrorCodeInvalidRequest, "Invalid Request", "method must be a non-empty string")
	}
	if hasResult || hasError {
		return invalidMessage(errID, ErrorCodeInvalidRequest, "Invalid Request", "a message with a method must not carry result or error")
	}

	var params interface{}
	if p, ok := members["params"]; ok {
		_ = json.Unmarshal(p, &params) // already known to be valid JSON
	}

	// A notification is a request without an id member.
	if !hasID {
		return inboundMessage{kind: messageNotification, notification: &MCPNotification{Method: method, Params: params}}
	}

	// An id member is present: it must be a string or a number. In particular
	// an explicit null is NOT a notification (and MCP forbids it for requests).
	id, ok := decodeID(idRaw)
	if !ok {
		return invalidMessage(nil, ErrorCodeInvalidRequest, "Invalid Request", "id must be a string or a number; null is not allowed")
	}
	var version string
	if v, ok := members["jsonrpc"]; ok {
		_ = json.Unmarshal(v, &version)
	}
	return inboundMessage{kind: messageRequest, request: &MCPRequest{JSONRPC: version, ID: id, Method: method, Params: params}}
}

// decodeID decodes a request id member. Only a string or a number is a valid id;
// numbers keep their exact wire spelling (json.Number) so the response echoes it
// unchanged.
func decodeID(raw json.RawMessage) (id interface{}, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}
	switch c := raw[0]; {
	case c == '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, false
		}
		return s, true
	case c == '-' || (c >= '0' && c <= '9'):
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&n) != nil {
			return nil, false
		}
		return n, true
	}
	return nil, false
}

func invalidMessage(id interface{}, code int, message, data string) inboundMessage {
	return inboundMessage{kind: messageInvalid, errResponse: &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: code, Message: message, Data: data},
	}}
}

// router is the one place where inbound messages are classified and dispatched.
// Every transport uses it, so stdio and HTTP cannot disagree about what a
// message is.
type router struct {
	request      RequestHandler
	notification NotificationHandler // nil: notifications are accepted and ignored
}

// route handles one raw inbound message. It returns the response the transport
// must write, or nil when no response may be written.
func (rt router) route(raw []byte) *MCPResponse {
	msg := classifyMessage(raw)
	switch msg.kind {
	case messageRequest:
		return rt.dispatchRequest(msg.request)
	case messageNotification:
		rt.dispatchNotification(msg.notification)
		return nil
	case messageClientResponse:
		log.Printf("ark: ignoring unsolicited response from the client")
		return nil
	default:
		return msg.errResponse
	}
}

// dispatchNotification delivers a notification and contains any failure of its
// handler: a notification has no response channel, so nothing may escape.
func (rt router) dispatchNotification(n *MCPNotification) {
	if rt.notification == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ark: notification %q handler failed: %v", n.Method, r)
		}
	}()
	rt.notification(n)
}

// dispatchRequest runs the request handler and contains a panic: a request
// always gets exactly one response, so a handler that panics is answered with
// -32603 for that request's id (no panic detail on the wire) and the transport
// keeps serving. Notifications never come through here.
func (rt router) dispatchRequest(req *MCPRequest) (resp *MCPResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ark: request %q handler panicked: %v\n%s", req.Method, r, debug.Stack())
			resp = internalErrorResponse(req.ID)
		}
	}()
	return rt.request(req)
}

// internalErrorResponse is the -32603 response for a request id. It carries no
// detail: diagnostics go to the log, not to the client.
func internalErrorResponse(id interface{}) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPError{Code: ErrorCodeInternalError, Message: "Internal error"},
	}
}

// encodeResponse serialises a response. If that fails, it falls back once to a
// -32603 response for the same id so the request is not left unanswered; if the
// fallback fails too it gives up with an error (no further retries).
func encodeResponse(resp *MCPResponse) ([]byte, error) {
	b, err := json.Marshal(resp)
	if err == nil {
		return b, nil
	}
	log.Printf("ark: marshaling response failed: %v", err)
	b, ferr := json.Marshal(internalErrorResponse(resp.ID))
	if ferr != nil {
		return nil, fmt.Errorf("marshaling internal error response: %w", ferr)
	}
	return b, nil
}
