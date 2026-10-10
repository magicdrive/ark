package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/commandline"
	arkctx "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages"
	"github.com/magicdrive/ark/internal/libgitignore"
)

// ToolsHandler handles all MCP tools
// defaultMaxFiles is the get_files_arklite file limit when maxFiles is absent
// or not positive.
const defaultMaxFiles = 10

type ToolsHandler struct {
	rootDir    string
	opt        *commandline.Option
	cacheStore cache.Store // nil → NopStore

	// ignore holds the repository's compiled ignore rules, rooted at rootDir,
	// by source: [ArkSource] the .arkignore rule, [GitSource] the .gitignore
	// rule.
	// They depend only on the repository's ignore files — never on the
	// process working directory or a request — and are rebuilt when those
	// files change.
	ignoreMu     sync.Mutex
	ignore       [2]ignoreState
	ignoreHits   int // compiled rule reused (tests)
	ignoreBuilds int // rule parsed and compiled (tests)

	// indexes reuses completed repository indexes across requests (see
	// index_cache.go); created on first use.
	indexOnce sync.Once
	indexes   *indexCache

	// newIndex replaces index construction in tests (e.g. to force SymbolID
	// collisions through index.NewWithIDs); nil means index.NewWithCache.
	newIndex func(ctx context.Context, root string, providers []language.Provider, store cache.Store) (*index.RepositoryIndex, error)

	// buildContext replaces the Context Engine in search_context tests; nil
	// means the engine (see contextBuild).
	buildContext func(idx *index.RepositoryIndex, root string, req arkctx.Request) (*arkctx.Result, error)

	// base and req are set on the handler of one request (forRequest): base
	// is the server's handler, which holds the state shared across requests
	// (ignore rules, indexes); req holds the request's policy snapshot.
	base *ToolsHandler
	req  *requestScope
}

// requestScope is what one request reads once and then uses throughout: the
// ignore files and the access policy compiled from them, so a request never
// mixes two generations of the rules.
type requestScope struct {
	reader *libgitignore.IgnoreReader // reads each rule file once for the request

	policyOnce sync.Once
	policy     accessPolicy

	access requestAccess // the pinned root and policy snapshot (request_access.go)
}

// forRequest returns the handler for one request: the same server state,
// with a policy snapshot taken on first use and kept for the request.
func (h *ToolsHandler) forRequest() *ToolsHandler {
	if h.req != nil {
		return h
	}
	return &ToolsHandler{
		rootDir:      h.rootDir,
		opt:          h.opt,
		cacheStore:   h.cacheStore,
		newIndex:     h.newIndex,
		buildContext: h.buildContext,
		base:         h,
		req:          &requestScope{reader: h.newRequestReader()},
	}
}

// shared returns the handler holding the state shared across requests.
func (h *ToolsHandler) shared() *ToolsHandler {
	if h.base != nil {
		return h.base
	}
	return h
}

// newIgnoreReader returns a reader of the repository's ignore files.
func (h *ToolsHandler) newIgnoreReader() *libgitignore.IgnoreReader {
	var extra []string
	if h.opt != nil {
		extra = h.opt.AdditionallyIgnoreRuleFilenameList
	}
	return libgitignore.NewIgnoreReader(h.rootDir, extra)
}

// newRequestReader returns a request's reader: it also records the entries of
// its repository walk outside the directories no index enters, so the
// request's index freshness check need not list the directories again
// (listedSources).
func (h *ToolsHandler) newRequestReader() *libgitignore.IgnoreReader {
	r := h.newIgnoreReader()
	r.CollectEntries(index.SkipDirName)
	return r
}

// sourceListingKey carries a request's repository listing (listedSources) in
// its context.
type sourceListingKey struct{}

// sourceListing is the request's walk of the server root: the root as the
// reader spelled it, and the entries in walk order.
type sourceListing struct {
	root    string
	entries []libgitignore.Entry
}

// requestListing returns the request's repository listing: the entries of the
// very walk its access policy snapshot was read by, so the freshness check and
// the policy describe one state of the directories.
func (h *ToolsHandler) requestListing() (sourceListing, bool) {
	if h.req == nil {
		return sourceListing{}, false
	}
	entries, ok := h.req.reader.Entries()
	return sourceListing{root: h.rootDir, entries: entries}, ok
}

// listedSources returns the listing's entries for a walk of index root
// canonical (symlink-free, inside the server root), spelled below canonical,
// or false when the listing cannot stand in for that walk: the walk failed,
// or canonical lies in a directory whose entries were not recorded.
func (l sourceListing) listedSources(canonical string) ([]index.WalkEntry, bool) {
	if len(l.entries) == 0 || !l.entries[0].D.IsDir() {
		return nil, false
	}
	realRoot := l.root
	if r, err := filepath.EvalSymlinks(l.root); err == nil {
		realRoot = r
	}
	rel, ok := relInside(realRoot, canonical)
	if !ok {
		return nil, false
	}
	prefix := l.root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if index.SkipDirName(part) {
				return nil, false // not recorded below such a directory
			}
		}
		prefix = filepath.Join(l.root, rel)
	}
	var out []index.WalkEntry
	for _, e := range l.entries {
		if e.Path != prefix && !strings.HasPrefix(e.Path, prefix+string(filepath.Separator)) {
			if len(out) > 0 {
				break // a directory's entries are contiguous in walk order
			}
			continue
		}
		out = append(out, index.WalkEntry{Path: canonical + e.Path[len(prefix):], D: e.D})
	}
	if len(out) == 0 || !out[0].D.IsDir() {
		return nil, false
	}
	return out, true
}

// ignoreReader returns the request's reader (each rule file read once for
// the request), or a fresh one outside a request.
func (h *ToolsHandler) ignoreReader() *libgitignore.IgnoreReader {
	if h.req != nil {
		return h.req.reader
	}
	return h.newIgnoreReader()
}

// ignoreFiles reads the whole repository's ignore files.
func (h *ToolsHandler) ignoreFiles() (*libgitignore.IgnoreFiles, error) {
	return h.ignoreReader().All()
}

// NewToolsHandler creates a new tools handler.
func NewToolsHandler(rootDir string, opt *commandline.Option) *ToolsHandler {
	return NewToolsHandlerWithCache(rootDir, opt, nil)
}

// NewToolsHandlerWithCache creates a ToolsHandler that persists extraction
// results in store. Pass cache.NopStore{} to disable caching.
//
// A relative rootDir is made absolute against the current directory now, so
// the root means the same directory for the handler's whole life whatever the
// process CWD later becomes. (The server resolves and validates --root at
// startup; this keeps every other constructor caller equally safe.)
func NewToolsHandlerWithCache(rootDir string, opt *commandline.Option, store cache.Store) *ToolsHandler {
	if abs, err := filepath.Abs(rootDir); err == nil {
		rootDir = abs
	}
	return &ToolsHandler{
		rootDir:    rootDir,
		opt:        opt,
		cacheStore: store,
	}
}

// ignoreState is a compiled rule of one source and the fingerprint of the
// ignore files it was compiled from.
type ignoreState struct {
	fingerprint string
	rule        *libgitignore.GitIgnore
	err         error // why rule is nil, if it is
}

// ignoreRule returns the file tools' ignore rule set rooted at h.rootDir: the
// .arkignore rule and, if allowGitignore, the .gitignore rule — each source
// on its own, so either excludes. A source that cannot be compiled (e.g. an
// unreadable rule file) is left out; the access policy, which reads the
// .arkignore source itself, fails closed in that case.
func (h *ToolsHandler) ignoreRule(allowGitignore bool) *libgitignore.RuleSet {
	ark, _ := h.sourceRule(libgitignore.ArkSource)
	rs := &libgitignore.RuleSet{Ark: ark}
	if allowGitignore {
		rs.Git, _ = h.sourceRule(libgitignore.GitSource)
	}
	return rs
}

// sourceRule returns the current rule of one source.
//
// The ignore files are read (walked, read and hashed) for every request, so
// an added, removed, moved or edited rule file is seen by the next request;
// only compiling is skipped while their fingerprint is unchanged. The rule is
// compiled from the very bytes the fingerprint was taken of, so the cache can
// never pair a fingerprint with the rule of other contents.
func (h *ToolsHandler) sourceRule(src libgitignore.Source) (*libgitignore.GitIgnore, error) {
	files, err := h.ignoreFiles()
	if err != nil {
		return nil, err // the repository could not be walked: nothing to reuse
	}
	fp := files.Fingerprint()
	i := int(src)
	s := h.shared()
	s.ignoreMu.Lock()
	defer s.ignoreMu.Unlock()
	if s.ignore[i].fingerprint == fp {
		s.ignoreHits++
		return s.ignore[i].rule, s.ignore[i].err
	}
	s.ignoreBuilds++
	rule, buildErr := files.CompileSource(src)
	s.ignore[i] = ignoreState{fingerprint: fp, rule: rule, err: buildErr}
	return rule, buildErr
}

// fileToolOption returns a per-request copy of the server's file-selection
// options with the request's overrides applied and every derived field
// (extension/directory lists, regexps, switches, ignore rule) recomputed from
// them. An invalid override (e.g. a regexp that does not compile) is an error:
// a request that cannot be honoured must not look like an empty result.
func (h *ToolsHandler) fileToolOption(args map[string]interface{}) (*commandline.Option, error) {
	var opt commandline.Option
	if h.opt != nil {
		opt = *h.opt
	}
	// The file tools' root is the server root (Option.IgnoreRoot): the walk
	// that starts there enters it even when it is a symlink.
	opt.WorkingDir, opt.TargetDirname = h.rootDir, ""
	if opt.AllowGitignoreFlagValue == "" {
		opt.AllowGitignoreFlagValue = "on"
	}
	if opt.IgnoreDotFileFlagValue == "" {
		opt.IgnoreDotFileFlagValue = "off"
	}
	for key, dst := range map[string]*string{
		"includeExt":       &opt.IncludeExt,
		"excludeExt":       &opt.ExcludeExt,
		"excludeDir":       &opt.ExcludeDir,
		"patternRegex":     &opt.PatternRegexpString,
		"excludeFileRegex": &opt.ExcludeFileRegexpString,
		"excludeDirRegex":  &opt.ExcludeDirRegexpString,
	} {
		if v, ok := args[key].(string); ok {
			*dst = v
		}
	}
	for key, dst := range map[string]*string{
		"ignoreDotfiles": &opt.IgnoreDotFileFlagValue,
		"allowGitignore": &opt.AllowGitignoreFlagValue,
	} {
		if v, ok := args[key].(bool); ok {
			*dst = "off"
			if v {
				*dst = "on"
			}
		}
	}
	if v, ok := args["skipNonUTF8"].(bool); ok {
		opt.SkipNonUTF8Flag = v
	}
	if err := opt.NormalizeFileFilters(); err != nil {
		return nil, err
	}
	if err := opt.AllowGitignoreFlag.Set(opt.AllowGitignoreFlagValue); err != nil {
		return nil, fmt.Errorf("allowGitignore %w", err)
	}
	opt.GitIgnoreRule = h.ignoreRule(opt.AllowGitignoreFlag.Bool())
	opt.AccessExclude = h.accessPolicy().excludesWalked
	return &opt, nil
}

// fileToolOptionError is the tool result for an option set that cannot be
// honoured.
func fileToolOptionError(err error) *CallToolResult {
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Invalid options: %v", err)}},
		IsError: true,
	}
}

// defaultProviders returns the providers used for repository indexing and
// relation queries: the full canonical language registry. Every MCP
// intelligence tool now dispatches through the same registry.
func defaultProviders() []language.Provider {
	return languages.Registry().Providers()
}

// buildIndex constructs a RepositoryIndex for fullPath, using the cache store
// when available.
//
// The index is built over the canonical form of fullPath (absolute, symlinks
// resolved), which must still lie inside the canonical server root, and is
// shared with every other request for the same directory while its sources
// are unchanged.
func (h *ToolsHandler) buildIndex(ctx context.Context, fullPath string) (*index.RepositoryIndex, error) {
	canonical, err := h.canonicalDir(fullPath)
	if err != nil {
		return nil, err
	}
	s := h.shared()
	if h.req != nil {
		ctx = context.WithValue(ctx, policyKey{}, h.accessPolicy())
		if listing, ok := h.requestListing(); ok {
			ctx = context.WithValue(ctx, sourceListingKey{}, listing)
		}
	}
	s.indexOnce.Do(func() {
		providers := defaultProviders()
		store := h.cacheStore
		if store == nil {
			store = cache.NopStore{}
		}
		// The index never contains a file the .arkignore access policy
		// excludes: each build and each freshness fingerprint walks the
		// sources with the policy current at that moment, so a policy change
		// that alters the file set alters the fingerprint and forces a rebuild.
		build := func(ctx context.Context, root string, providers []language.Provider, store cache.Store) (*index.RepositoryIndex, error) {
			return index.NewWithCacheExcluding(ctx, root, providers, store, s.accessPolicy().indexExclude)
		}
		if h.newIndex != nil {
			build = h.newIndex
		}
		s.indexes = newIndexCache(providers, func(ctx context.Context, root string) (*index.RepositoryIndex, error) {
			return build(ctx, root, providers, store)
		})
		if s.newIndex == nil {
			// A freshness check runs in the request: it uses the request's
			// snapshot (taken after the request arrived, as the index cache
			// requires). A build runs detached and reads the policy itself.
			//
			// The request has already walked the repository to read its
			// .arkignore files (IgnoreReader.All); the freshness check reuses
			// that listing instead of listing the directories again, and
			// reads and hashes the same files in the same order
			// (index.SourceFingerprintListed). Without a usable listing it
			// walks.
			s.indexes.fingerprint = func(ctx context.Context, root string) (string, error) {
				policy, ok := ctx.Value(policyKey{}).(accessPolicy)
				if !ok {
					policy = s.accessPolicy()
				} else if listing, ok := ctx.Value(sourceListingKey{}).(sourceListing); ok {
					if entries, ok := listing.listedSources(root); ok {
						return index.SourceFingerprintListed(ctx, root, providers, policy.indexExclude, entries)
					}
				}
				return index.SourceFingerprintExcluding(ctx, root, providers, policy.indexExclude)
			}
		}
	})
	return s.indexes.get(ctx, canonical)
}

// canonicalDir returns the absolute, symlink-free form of dir, refusing a
// directory that resolves outside the (equally canonical) server root.
func (h *ToolsHandler) canonicalDir(dir string) (string, error) {
	canonical, err := canonicalPath(dir)
	if err != nil {
		return "", err
	}
	root, err := canonicalPath(h.rootDir)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(root, canonical); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the server root %q", dir, h.rootDir)
	}
	return canonical, nil
}

func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("index: root %q: %w", p, err)
	}
	return resolved, nil
}

// ListTools returns all available tools
func (h *ToolsHandler) ListTools() []Tool {
	tools := []Tool{
		{
			Name:        "get_directory_tree",
			Description: "Get directory tree structure as JSON. For a large repository, bound it with maxDepth and excludeDirs",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to scan",
					},
					"maxDepth": map[string]interface{}{
						"type":        "integer",
						"description": "List directories at most this deep (1 = the path's direct children); deeper directories are listed without contents and marked truncated. 0 or absent = unlimited",
					},
					"excludeDirs": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated directories to omit: a name (matches that path component anywhere, e.g. vendor) or a path relative to the tree root (e.g. storage/framework)",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_file_content",
			Description: "Get content of a single file with optional filtering",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to read",
					},
					"maskSecrets": map[string]interface{}{
						"type":        "boolean",
						"description": "Accepted for compatibility. Masking follows the server's --mask-secrets setting; a call cannot turn it off",
						"default":     true,
					},
					"deleteComments": map[string]interface{}{
						"type":        "boolean",
						"description": "Remove code comments",
						"default":     false,
					},
					"withLineNumbers": map[string]interface{}{
						"type":        "boolean",
						"description": "Include line numbers",
						"default":     true,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in directory with filtering options",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to scan",
					},
					"includeExt": map[string]interface{}{
						"type":        "string",
						"description": "Include only these extensions (comma-separated)",
					},
					"excludeExt": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these extensions (comma-separated)",
					},
					"excludeDir": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these directories (comma-separated)",
					},
					"patternRegex": map[string]interface{}{
						"type":        "string",
						"description": "Include files matching this regex pattern",
					},
					"excludeFileRegex": map[string]interface{}{
						"type":        "string",
						"description": "Exclude files matching this regex pattern",
					},
					"excludeDirRegex": map[string]interface{}{
						"type":        "string",
						"description": "Exclude directories matching this regex pattern",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
					"skipNonUTF8": map[string]interface{}{
						"type":        "boolean",
						"description": "Skip non-UTF8 files",
						"default":     false,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "search_in_files",
			Description: "Search for text within files",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path to search in",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"isRegex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat query as regex",
						"default":     false,
					},
					"includeExt": map[string]interface{}{
						"type":        "string",
						"description": "Include only these extensions (comma-separated)",
					},
					"excludeExt": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these extensions (comma-separated)",
					},
					"excludeDir": map[string]interface{}{
						"type":        "string",
						"description": "Exclude these directories (comma-separated)",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     100,
					},
				},
				"required": []string{"path", "query"},
			},
		},
		{
			Name:        "get_file_info",
			Description: "Get metadata information about a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path to analyze",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_project_stats",
			Description: "Get statistics about a project directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Project directory path",
					},
					"ignoreDotfiles": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore dotfiles",
						"default":     false,
					},
					"allowGitignore": map[string]interface{}{
						"type":        "boolean",
						"description": "Respect .gitignore rules",
						"default":     true,
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "get_files_arklite",
			Description: "Get multiple files in arklite format",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"paths": map[string]interface{}{
						"type":        "array",
						"description": "Array of file paths to include",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"maskSecrets": map[string]interface{}{
						"type":        "boolean",
						"description": "Accepted for compatibility. Masking follows the server's --mask-secrets setting; a call cannot turn it off",
						"default":     true,
					},
					"deleteComments": map[string]interface{}{
						"type":        "boolean",
						"description": "Remove code comments",
						"default":     false,
					},
					"maxFiles": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of files to process",
						"default":     10,
					},
				},
				"required": []string{"paths"},
			},
		},
	}
	// Add syntax tools
	tools = append(tools, SyntaxToolDefinitions()...)
	tools = append(tools, ReferenceToolDefinitions()...)
	tools = append(tools, RelationsToolDefinitions()...)
	tools = append(tools, CallersToolDefinitions()...)
	tools = append(tools, RepomapToolDefinitions()...)
	tools = append(tools, ContextToolDefinitions()...)
	tools = append(tools, SearchContextToolDefinitions()...)
	tools = append(tools, ImpactToolDefinitions()...)
	tools = append(tools, SearchToolDefinitions()...)
	tools = append(tools, LanguageSupportToolDefinitions()...)
	tools = append(tools, DiagnosticsToolDefinitions()...)
	return tools
}

// CallTool executes a specific tool
// CallTool runs a tool and returns its result as it may leave the server:
// with the secrets in its repository-derived text masked (sanitize.go),
// unless masking is turned off (masking.go).
func (h *ToolsHandler) CallTool(name string, arguments map[string]interface{}) (*CallToolResult, error) {
	h, end := h.beginRequest()
	defer end()
	result, err := h.callTool(name, arguments)
	if !h.masking(name, arguments) {
		return result, err
	}
	return sanitizeToolResult(name, result), err
}

func (h *ToolsHandler) callTool(name string, arguments map[string]interface{}) (*CallToolResult, error) {
	switch name {
	case "get_directory_tree":
		return h.getDirectoryTree(arguments)
	case "get_file_content":
		return h.getFileContent(arguments)
	case "list_files":
		return h.listFiles(arguments)
	case "search_in_files":
		return h.searchInFiles(arguments)
	case "get_file_info":
		return h.getFileInfo(arguments)
	case "get_project_stats":
		return h.getProjectStats(arguments)
	case "get_files_arklite":
		return h.getFilesArklite(arguments)
	case "get_symbols":
		return h.getSymbols(arguments)
	case "find_symbol":
		return h.findSymbol(arguments)
	case "get_symbol":
		return h.getSymbol(arguments)
	case "find_references":
		return h.findReferences(arguments)
	case "get_relations":
		return h.getRelations(arguments)
	case "get_callers":
		return h.getCallers(arguments)
	case "get_callees":
		return h.getCallees(arguments)
	case "get_repository_map":
		return h.getRepositoryMap(arguments)
	case "get_context":
		return h.getContext(arguments)
	case "search_context":
		return h.searchContext(arguments)
	case "analyze_change_impact":
		return h.analyzeChangeImpact(arguments)
	case "search_code":
		return h.searchCode(arguments)
	case "get_language_support":
		return h.getLanguageSupport(arguments)
	case "get_diagnostics":
		return h.getDiagnostics(arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *ToolsHandler) getDirectoryTree(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath, _, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	var limits treeLimits
	if v, ok := args["maxDepth"].(float64); ok && v > 0 {
		limits.maxDepth = int(v)
	}
	if v, ok := args["excludeDirs"].(string); ok {
		for _, d := range strings.Split(v, ",") {
			if d = strings.Trim(filepath.ToSlash(strings.TrimSpace(d)), "/"); d != "" {
				limits.excludeDirs = append(limits.excludeDirs, filepath.ToSlash(filepath.Clean(d)))
			}
		}
	}
	tree, err := GenerateBoundedDirectoryTreeJSON(fullPath, h.ignoreRule(true), h.accessPolicy().excludesWalked, limits)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: tree}},
	}, nil
}

func (h *ToolsHandler) getFileContent(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	// Decided and read in one operation on the request's pinned tree
	// (request_access.go).
	data, fullPath, _, gateErr, readErr := h.readToolFile(path)
	if gateErr != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: gateErr.Error()}},
			IsError: true,
		}, nil
	}

	// Create option based on parameters
	opt := *h.opt // Copy base options
	if maskSecrets, ok := args["maskSecrets"].(bool); ok {
		if maskSecrets {
			opt.MaskSecretsFlagValue = "on"
		} else {
			opt.MaskSecretsFlagValue = "off"
		}
	}
	if deleteComments, ok := args["deleteComments"].(bool); ok {
		opt.DeleteCommentsFlag = deleteComments
	}
	if withLineNumbers, ok := args["withLineNumbers"].(bool); ok {
		if withLineNumbers {
			opt.WithLineNumberFlagValue = "on"
		} else {
			opt.WithLineNumberFlagValue = "off"
		}
	}

	content, err := processFileContent(data, readErr, fullPath, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: content}},
	}, nil
}

func (h *ToolsHandler) listFiles(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath, _, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	opt, err := h.fileToolOption(args)
	if err != nil {
		return fileToolOptionError(err), nil
	}

	files, err := ListFilteredFiles(fullPath, opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	filesList := strings.Join(files, "\n")
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: filesList}},
	}, nil
}

func (h *ToolsHandler) searchInFiles(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}
	query, ok := args["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query parameter is required")
	}

	fullPath, _, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	isRegex := false
	if val, ok := args["isRegex"].(bool); ok {
		isRegex = val
	}

	maxResults := 100
	if val, ok := args["maxResults"].(float64); ok {
		maxResults = int(val)
	}

	opt, err := h.fileToolOption(args)
	if err != nil {
		return fileToolOptionError(err), nil
	}

	results, err := SearchInFiles(fullPath, query, isRegex, maxResults, opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: results}},
	}, nil
}

func (h *ToolsHandler) getFileInfo(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	// The object described is the object decided (request_access.go).
	info, fullPath, gateErr, err := h.statToolFile(path)
	if gateErr != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: gateErr.Error()}},
			IsError: true,
		}, nil
	}
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	language := DetectLanguage(fullPath)

	fileInfo := map[string]interface{}{
		"path":      path,
		"size":      info.Size(),
		"modTime":   info.ModTime().Format(time.RFC3339),
		"isDir":     info.IsDir(),
		"language":  language,
		"extension": filepath.Ext(fullPath),
		"basename":  filepath.Base(fullPath),
	}

	infoJSON, err := json.MarshalIndent(fileInfo, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(infoJSON)}},
	}, nil
}

func (h *ToolsHandler) getProjectStats(args map[string]interface{}) (*CallToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path parameter is required")
	}

	fullPath, _, err := h.resolveToolPath(path)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}

	opt, err := h.fileToolOption(args)
	if err != nil {
		return fileToolOptionError(err), nil
	}

	stats, err := GetProjectStats(fullPath, opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	statsJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: string(statsJSON)}},
	}, nil
}

func (h *ToolsHandler) getFilesArklite(args map[string]interface{}) (*CallToolResult, error) {
	pathsInterface, ok := args["paths"]
	if !ok {
		return nil, fmt.Errorf("paths parameter is required")
	}

	pathsSlice, ok := pathsInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("paths must be an array")
	}

	var paths []string
	for _, p := range pathsSlice {
		if pathStr, ok := p.(string); ok {
			paths = append(paths, pathStr)
		}
	}

	maxFiles := defaultMaxFiles
	if val, ok := args["maxFiles"].(float64); ok {
		maxFiles = int(val)
	}
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}

	if len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}

	// Create option based on parameters
	opt := *h.opt // Copy base options
	opt.OutputFormatValue = "arklite"

	if maskSecrets, ok := args["maskSecrets"].(bool); ok {
		if maskSecrets {
			opt.MaskSecretsFlagValue = "on"
		} else {
			opt.MaskSecretsFlagValue = "off"
		}
	}
	if deleteComments, ok := args["deleteComments"].(bool); ok {
		opt.DeleteCommentsFlag = deleteComments
	}

	// Every path is decided and read on the request's pinned tree under its
	// one policy snapshot (request_access.go) before anything is written: a
	// refusal of any path — at the gate, or while it is read — answers the
	// whole call, so no content goes out with it.
	files := make([]arkliteFile, 0, len(paths))
	for _, path := range paths {
		data, fullPath, _, gateErr, readErr := h.readToolFile(path)
		if gateErr != nil {
			return &CallToolResult{
				Content: []Content{{Type: "text", Text: gateErr.Error()}},
				IsError: true,
			}, nil
		}
		files = append(files, arkliteFile{path: fullPath, data: data, err: readErr})
	}

	content, err := generateArklite(files, &opt)
	if err != nil {
		return &CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil
	}

	return &CallToolResult{
		Content: []Content{{Type: "text", Text: content}},
	}, nil
}
