package mcp

import (
	"container/list"
	"context"
	"crypto/sha256"
	"sync"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/syntax"
)

// Parse results by content.
//
// find_symbol and find_references walk a directory and parse every source
// file they reach, on every request. The memo keeps what parsing a file's
// bytes yields under the digest of those bytes (and of what else the result
// depends on: the language, the provider's cache version, the file's
// repository-relative name where the result embeds it):
//
//   - Nothing about access changes: every request still walks, decides and
//     reads each file through its own walker (internal/safewalk); only the
//     parse of bytes already parsed is skipped. A lookup needs the bytes
//     themselves, so an entry reveals nothing a request did not read.
//   - Content, never metadata, is the key: other bytes are another key, so
//     an entry is never stale and needs no invalidation.
//   - Values are shared read-only between requests; callers copy what they
//     return.
//   - Owned by the server's handler (shared across its requests), safe for
//     concurrent requests, bounded: least recently used entries go first
//     once the bytes they were parsed from exceed parseMemoBytes.
type parseMemo struct {
	mu    sync.Mutex
	max   int64
	size  int64
	order *list.List // front: most recently used
	items map[memoKey]*list.Element

	hits, misses uint64 // tests and diagnostics
}

// parseMemoBytes bounds the source bytes the memo's entries stand for.
const parseMemoBytes = 64 << 20

type memoKey struct {
	what string // the result's kind and what it depends on besides content
	sum  [sha256.Size]byte
}

type memoEntry struct {
	key  memoKey
	cost int64
	val  any
}

func newParseMemo(max int64) *parseMemo {
	return &parseMemo{max: max, order: list.New(), items: map[memoKey]*list.Element{}}
}

func newMemoKey(what string, src []byte) memoKey {
	return memoKey{what: what, sum: sha256.Sum256(src)}
}

// get returns the value stored under k.
func (m *parseMemo) get(k memoKey) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[k]
	if !ok {
		m.misses++
		return nil, false
	}
	m.hits++
	m.order.MoveToFront(el)
	return el.Value.(*memoEntry).val, true
}

// put stores val under k; cost is the size of the bytes it was parsed from.
func (m *parseMemo) put(k memoKey, cost int64, val any) {
	if cost > m.max {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[k]; ok {
		m.order.MoveToFront(el)
		return
	}
	m.items[k] = m.order.PushFront(&memoEntry{key: k, cost: cost, val: val})
	m.size += cost
	for m.size > m.max {
		el := m.order.Back()
		e := el.Value.(*memoEntry)
		m.order.Remove(el)
		delete(m.items, e.key)
		m.size -= e.cost
	}
}

// parsedSymbols is what find_symbol takes from a file's bytes: whether they
// parse, and the symbols extracted.
type parsedSymbols struct {
	ok      bool
	symbols []syntax.Symbol
}

// fileSymbols parses src (the file named file) as lang and extracts its
// symbols, as find_symbol always did (syntax.Parse decides whether the file
// counts as parsed, syntax.ExtractSymbols extracts) — once per content and
// file name (a provider may name a symbol after its file).
func (h *ToolsHandler) fileSymbols(lang syntax.SupportedLanguage, file string, src []byte) parsedSymbols {
	memo := h.parseMemo()
	key := newMemoKey("symbols\x00"+string(lang)+"\x00"+file, src)
	if v, ok := memo.get(key); ok {
		return v.(parsedSymbols)
	}
	var out parsedSymbols
	if res, err := syntax.ParseNamed(lang, file, src); err == nil {
		out = parsedSymbols{ok: true, symbols: syntax.ExtractSymbols(res)}
		res.Release()
	}
	memo.put(key, int64(len(src)), out)
	return out
}

// fileReferences extracts src (the file fileID of the server root) with
// provider, as find_references always did — once per content. A failed
// extraction yields ok false.
func (h *ToolsHandler) fileReferences(provider language.Provider, fileID source.FileID, src []byte) ([]language.ReferenceDraft, bool) {
	memo := h.parseMemo()
	key := newMemoKey("references\x00"+string(provider.Language())+"\x00"+provider.CacheVersion()+"\x00"+string(fileID), src)
	type refs struct {
		ok   bool
		refs []language.ReferenceDraft
	}
	if v, ok := memo.get(key); ok {
		r := v.(refs)
		return r.refs, r.ok
	}
	var out refs
	if ext, err := provider.Extract(context.Background(), fileID, src); err == nil {
		out = refs{ok: true, refs: ext.References}
	}
	memo.put(key, int64(len(src)), out)
	return out.refs, out.ok
}

// parseMemo returns the server's memo, made on first use.
func (h *ToolsHandler) parseMemo() *parseMemo {
	s := h.shared()
	s.memoOnce.Do(func() { s.memo = newParseMemo(parseMemoBytes) })
	return s.memo
}
