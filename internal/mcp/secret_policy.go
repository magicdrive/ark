package mcp

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/magicdrive/ark/internal/commandline"
	"github.com/magicdrive/ark/internal/fsroot"
	"github.com/magicdrive/ark/internal/secretpolicy"
	"github.com/magicdrive/ark/internal/secrets"
)

// The masking policy of the served repository (internal/secretpolicy).
//
// Every request that masks reads the repository's secret rules
// configuration from the request's pinned root — so an edit is seen by the
// next request — and compiles it only when its bytes changed (its
// fingerprint, as the ignore rules are reused: sourceRule). A compiled
// policy is immutable: concurrent requests share it, and a request keeps the
// one it took for all its masking, whatever another request compiles.
//
// The entries that disable built-in masking rules apply only when the
// operator started the server with --secret-rule-overrides on: a file in
// the repository, which an agent may write, must not weaken the masking the
// operator runs. Without it they are left out — the rest of the
// configuration applies — and the server logs, once per configuration, which
// entries it left out.
//
// A configuration that cannot be read or compiled fails closed: a result
// with repository text in it is replaced by the error (policyError), and
// only results built from identifiers alone go out. The error and the log
// name the file and the problem, never a value.

// policyCache is the last compiled configuration, shared by the requests
// of one server.
type policyCache struct {
	mu     sync.Mutex
	fp     string
	loaded *secretpolicy.Loaded
	err    error
	logged map[string]bool // fingerprints whose problems were logged
	builds int             // compiled (tests)
}

func newPolicyCache() *policyCache { return &policyCache{logged: map[string]bool{}} }

// load returns the policy of the configuration at the root of tree.
func (c *policyCache) load(tree *fsroot.Tree, opt *commandline.Option) (*secrets.Policy, error) {
	name, data, err := secretpolicy.Read(tree)
	if err != nil {
		c.mu.Lock()
		// Nothing compiled stands for it: the next read compiles again,
		// and until then current() withholds error messages.
		c.fp, c.loaded, c.err = "", nil, err
		if key := "read:" + err.Error(); !c.logged[key] {
			c.logged[key] = true
			log.Printf("ark: secret rules configuration: %v; results with repository text are refused", err)
		}
		c.mu.Unlock()
		return nil, err
	}
	o := secretpolicy.Options{AllowWeakening: opt != nil && opt.SecretRuleOverrides}
	fp := secretpolicy.Fingerprint(name, data, o)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fp != fp || (c.loaded == nil && c.err == nil) {
		c.builds++
		c.fp = fp
		if name == "" {
			c.loaded, c.err = secretpolicy.None(o), nil
		} else {
			c.loaded, c.err = secretpolicy.Build(name, data, o)
		}
		switch {
		case c.err != nil && !c.logged[fp]:
			log.Printf("ark: secret rules configuration: %v; results with repository text are refused", c.err)
		case c.err == nil && len(c.loaded.Ignored) > 0 && !c.logged[fp]:
			log.Printf("ark: %s disables built-in masking rules (%s); not applied: the server was not started with --secret-rule-overrides on", name, strings.Join(c.loaded.IgnoredIDs(), ", "))
		}
		c.logged[fp] = true
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.loaded.Policy, nil
}

// current returns what an error message, which belongs to no request, is
// masked by: the policy of the configuration the server read last. The
// request that failed read it just before (CallTool, ReadResource take
// their policy even when the tool fails), so it is the current one; before
// any configuration was read, the recommended policy. If the configuration
// read last does not compile, there is no policy to mask by: the error is
// returned and the message must be withheld (errorData).
func (c *policyCache) current() (*secrets.Policy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.err != nil:
		return nil, c.err
	case c.loaded != nil:
		return c.loaded.Policy, nil
	}
	return secrets.Recommended(), nil
}

// policyCache returns the server's policy cache, made on first use.
func (h *ToolsHandler) policyCache() *policyCache {
	s := h.shared()
	s.policiesOnce.Do(func() {
		if s.policies == nil {
			s.policies = newPolicyCache()
		}
	})
	return s.policies
}

// maskPolicy returns the request's masking policy, read and taken once per
// request (requestScope).
func (h *ToolsHandler) maskPolicy() (*secrets.Policy, error) {
	if h.req == nil {
		return nil, errNoRequest
	}
	r := h.req
	r.maskOnce.Do(func() {
		if _, err := h.snapshot(); err != nil && h.req.access.tree == nil {
			r.maskErr = err
			return
		}
		r.mask, r.maskErr = h.policyCache().load(h.req.access.tree, h.opt)
	})
	return r.mask, r.maskErr
}

// policyError is the result that replaces one the configuration's error
// leaves unmaskable.
func policyError(err error) *CallToolResult {
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Secret masking configuration error: %v. Nothing was returned: fix the configuration or remove it.", err)}},
		IsError: true,
	}
}

// identifiersOnly reports whether a result of tool goes out unmasked
// whatever the policy: the plain text of an identifier tool (sanitize.go).
func identifiersOnly(tool string, r *CallToolResult) bool {
	if !identifierTextTools[tool] || compactTextTools[tool] {
		return false
	}
	for _, c := range r.Content {
		if isJSONText(c.Text) {
			return false
		}
	}
	return true
}
