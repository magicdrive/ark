// Package actions reads the GitHub Actions meaning of a YAML file that
// yamldoc read: a workflow (.github/workflows/*.yml, *.yaml) and an action's
// metadata (action.yml, action.yaml). It states what the file's own text
// fixes and runs nothing: no expression (${{ ... }}) is evaluated, no script
// is read as a program, no action or workflow is fetched.
//
// Declarations. A workflow is its file (Qualified: the file's path); its
// triggers are the events of `on` ("<file>::on:<event>"); its jobs are
// "<file>::<job id>"; a job's steps "<file>::<job id>::<step key>". An
// action is its directory ("<dir>", "." at the root) — `uses: ./dir` names
// the directory, and the runner reads the action.yml or action.yaml in it
// —, the steps of a composite action "<dir>::<step key>". A step's key is
// its id, else its name, else its `uses` without its ref, else "run" (or "step"); a key a
// step of the same job shares with an earlier one is "<key>#<n>" for the
// n-th. Settings that shape a workflow or a job (permissions, concurrency,
// env, defaults, if, strategy, environment, outputs, container, services,
// with, secrets; an action's inputs, outputs and runs) are keys named by
// their JSON Pointer ("<file>#/jobs/<id>/permissions"), as the YAML layer
// names keys.
//
// Relations, all evidence of the file alone:
//
//   - contains: a workflow its triggers and jobs, a job and a composite
//     action their steps (Exact: the file's own structure);
//   - depends_on: a job the jobs its `needs` names, in the same workflow
//     (Exact; a name no job has is Unresolved);
//   - call: a job the workflow its `uses: ./.github/workflows/x.yml`
//     names — path evidence of the file at the same commit (Exact); a step
//     the action its `uses: ./dir` names, at most Strong: the path is
//     relative to the runner's workspace, which a checkout to another path
//     or of another repository changes; a remote action or workflow
//     (owner/repo[/path]@ref, docker://image) is outside the repository.
//     A `uses` with an expression is Dynamic; one that is no reference
//     GitHub accepts is reported (actions_invalid_uses) and no reference: it
//     names nothing, here or elsewhere.
//
// What a workflow under a directory's .github/workflows names by "./" is
// relative to that directory (a repository checked out inside another);
// an action outside any .github directory takes the repository root.
//
// Content reached through an alias (`steps: *common`, `needs: *deps`) is
// read as GitHub reads it, as the job's own; its declarations and
// references are located at the alias. Merge keys (<<) are no GitHub
// Actions syntax: reported (actions_merge_key) and not merged.
package actions

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/magicdrive/ark/internal/languages/yaml/yamldoc"
	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// Problem codes.
const (
	CodeInvalidUses       = "actions_invalid_uses"
	CodeNeedsCycle        = "actions_needs_cycle"
	CodeMergeKey          = "actions_merge_key"
	CodeMultipleDocuments = "actions_multiple_documents"
	CodeNotMapping        = "actions_not_mapping"
	CodeDuplicateStepID   = "actions_duplicate_step_id"
	CodeDuplicateJobID    = "actions_duplicate_job_id"
	CodeInvalidJobID      = "actions_invalid_job_id"
	CodeTruncated         = "actions_truncated"
)

// Symbol is a declaration the file states.
type Symbol struct {
	Name, Qualified string
	Kind            symbol.SymbolKind
	Span            yamldoc.Span
	Parent          string
	Signature       string
}

// Ref is a reference the file states. NameQualified names its target's
// identity; FileIdentity marks it as path evidence (a file's own
// declaration); InRepository marks an identity that can only be the
// repository's; Cap bounds its confidence ("strong"); Dynamic marks a name
// an expression computes.
type Ref struct {
	Name, Kind, Container, NameQualified string
	Span                                 yamldoc.Span
	FileIdentity, InRepository, Dynamic  bool
	Cap, TargetKinds                     string
}

// Result is what a GitHub Actions file states.
type Result struct {
	Symbols  []Symbol
	Refs     []Ref
	Problems []yamldoc.Problem
	// ProblemsTruncated: the file has more problems than Problems holds.
	ProblemsTruncated bool
}

// Kind of file.
type fileKind int

const (
	notActions fileKind = iota
	workflowFile
	actionFile
)

// classify tells a workflow from its path, an action's metadata from its
// name and its content (a mapping with runs).
func classify(file string, s *yamldoc.Stream) (fileKind, string) {
	f := strings.ReplaceAll(file, "\\", "/")
	base := path.Base(f)
	dir := path.Dir(f)
	if ext := path.Ext(base); (ext == ".yml" || ext == ".yaml") && (dir == ".github/workflows" || strings.HasSuffix(dir, "/.github/workflows")) {
		return workflowFile, strings.TrimSuffix(dir, ".github/workflows")
	}
	if base == "action.yml" || base == "action.yaml" {
		for _, d := range s.Docs {
			if k, _ := yamldoc.Get(d.Root, "runs"); k != nil && d.Index == 0 {
				root := ""
				if f == ".github" || strings.HasPrefix(f, ".github/") {
					root = ""
				} else if i := strings.LastIndex(f, "/.github/"); i >= 0 {
					root = f[:i+1]
				}
				return actionFile, root
			}
		}
	}
	return notActions, ""
}

// Extract reads file as a GitHub Actions file. ok is false for a file that
// is none: its keys are the YAML layer's to state. At most maxSymbols
// declarations and maxRefs references are stated, and as many needs read
// (actions_truncated past them): content an alias repeats is read again
// where it is repeated, so a small file could otherwise state a number of
// relations that grows with the square of its size.
func Extract(file string, s *yamldoc.Stream, maxSymbols, maxRefs int) (Result, bool) {
	kind, root := classify(file, s)
	if kind == notActions {
		return Result{}, false
	}
	sep := "/"
	if strings.Contains(file, "\\") && !strings.Contains(file, "/") {
		sep = "\\" // the index's file IDs separate with the OS's separator (Windows)
	}
	x := &extractor{s: s, file: file, root: root, sep: sep, budget: maxSymbols, refBudget: maxRefs, needsBudget: maxRefs, refSeen: map[refKey]bool{}}
	switch kind {
	case workflowFile:
		x.workflow()
	case actionFile:
		x.action()
	}
	return x.out, true
}

type refKey struct {
	kind, name, container string
	span                  yamldoc.Span
}

// maxProblems bounds the problems a file reports.
const maxProblems = 1000

type extractor struct {
	s           *yamldoc.Stream
	file        string
	root        string            // the directory "./" names, "" or ending in "/"
	sep         string            // the separator of the index's file IDs, which identities of paths use
	budget      int               // declarations left
	refBudget   int               // references left
	needsBudget int               // needs left to read for the cycle check
	jobIDs      map[string]string // the workflow's job ids by their case-folded spelling
	truncated   bool
	refSeen     map[refKey]bool
	out         Result
}

// at is where the declarations and references of a node are located: the
// node's own bytes, or — for content reached through an alias — the alias's.
type at struct{ via *yamldoc.Span }

func (x *extractor) span(a at, n *yaml.Node) yamldoc.Span {
	if a.via != nil {
		return *a.via
	}
	return x.s.NodeSpan(n)
}

func (x *extractor) entry(a at, k, v *yaml.Node) yamldoc.Span {
	if a.via != nil {
		return *a.via
	}
	return x.s.EntrySpan(k, v)
}

// deref follows an alias: what it names, located at the alias unless an
// outer alias already locates it.
func (x *extractor) deref(a at, n *yaml.Node) (*yaml.Node, at) {
	if n == nil || n.Kind != yaml.AliasNode {
		return n, a
	}
	t, ok := yamldoc.Deref(n)
	if !ok {
		return nil, a
	}
	if a.via == nil {
		sp := x.s.NodeSpan(n)
		a = at{via: &sp}
	}
	return t, a
}

func (x *extractor) symbol(s Symbol) bool {
	if x.budget <= 0 {
		x.truncate(s.Span)
		return false
	}
	x.budget--
	x.out.Symbols = append(x.out.Symbols, s)
	return true
}

func (x *extractor) truncate(sp yamldoc.Span) {
	if !x.truncated {
		x.truncated = true
		x.problem(CodeTruncated, sp, false, "the file states more declarations, references or needs than Ark reads: the rest are left out")
	}
}

// ref states a reference once: the same reference of one container stated
// twice (a needs list naming a job twice) is one.
func (x *extractor) ref(r Ref) {
	k := refKey{r.Kind, r.Name, r.Container, r.Span}
	if x.refSeen[k] {
		return
	}
	if x.refBudget <= 0 {
		x.truncate(r.Span)
		return
	}
	x.refBudget--
	x.refSeen[k] = true
	x.out.Refs = append(x.out.Refs, r)
}

func (x *extractor) problem(code string, sp yamldoc.Span, isErr bool, msg string) {
	if len(x.out.Problems) >= maxProblems {
		x.out.ProblemsTruncated = true
		return
	}
	x.out.Problems = append(x.out.Problems, yamldoc.Problem{Code: code, Message: msg, Start: sp.Start, End: sp.End, Error: isErr})
}

// contains states that parent contains the declaration child, at sp. Its
// name is the child's identity: a structural relation is no use of a name,
// and find_references of a name (an action's, a job's) lists its uses only.
func (x *extractor) contains(parent string, child Symbol, sp yamldoc.Span) {
	x.ref(Ref{Name: child.Qualified, Kind: string(reference.KindContains), Container: parent, Span: sp,
		NameQualified: child.Qualified, InRepository: true, TargetKinds: string(child.Kind)})
}

// mergeKeys reports the merge keys of a mapping.
func (x *extractor) mergeKeys(m *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
			x.problem(CodeMergeKey, x.s.NodeSpan(k), false, "a merge key (<<) is no GitHub Actions syntax: it is not merged")
		}
	}
}

// pointer is the JSON Pointer token of a key.
func pointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// keys states the settings of a mapping that are keys, under parent.
func (x *extractor) keys(a at, m *yaml.Node, names []string, ptr, parent string) {
	for _, name := range names {
		k, v := yamldoc.Get(m, name)
		if k == nil {
			continue
		}
		x.symbol(Symbol{Name: ptr + "/" + pointer(name), Qualified: x.file + "#" + ptr + "/" + pointer(name), Kind: symbol.KindKey,
			Span: x.entry(a, k, v), Parent: parent})
	}
}

// document returns the root of the file's first document as a mapping, or
// nil after reporting why there is none.
func (x *extractor) document() *yaml.Node {
	var root *yaml.Node
	n := 0
	for _, d := range x.s.Docs {
		if d.Root == nil || d.Root.Kind == yaml.ScalarNode && d.Root.Tag == "!!null" {
			continue
		}
		n++
		if n == 1 && d.Index == 0 {
			root = d.Root
		}
		if n == 2 {
			x.problem(CodeMultipleDocuments, yamldoc.Span{Start: d.Start, End: d.Start}, false,
				"a GitHub Actions file is one YAML document: the documents after the first are not read")
		}
	}
	if root == nil {
		return nil
	}
	root, _ = yamldoc.Deref(root)
	if root == nil || root.Kind != yaml.MappingNode {
		x.problem(CodeNotMapping, x.s.NodeSpan(root), false, "a GitHub Actions file's document is a mapping")
		return nil
	}
	return root
}

// jobID is the form GitHub requires of a job id.
var jobID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

var workflowKeys = []string{"permissions", "concurrency", "env", "defaults"}

var jobKeys = []string{"if", "permissions", "strategy", "concurrency", "environment", "outputs",
	"env", "defaults", "container", "services", "with", "secrets"}

func (x *extractor) workflow() {
	sig := ""
	root := x.document()
	if v, ok := yamldoc.Scalar(get(root, "name")); ok {
		sig = "name: " + v
	}
	wf := Symbol{Name: path.Base(x.file), Qualified: x.file, Kind: symbol.KindWorkflow,
		Span: yamldoc.Span{Start: 0, End: len(x.s.Src)}, Signature: sig}
	x.symbol(wf)
	if root == nil {
		return
	}
	x.mergeKeys(root)
	x.triggers(root, wf.Qualified)
	x.keys(at{}, root, workflowKeys, "", wf.Qualified)
	jk, jobs := yamldoc.Get(root, "jobs")
	if jk == nil {
		return
	}
	jobs, a := x.deref(at{}, jobs)
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return
	}
	x.mergeKeys(jobs)
	// Job ids are case-insensitive (GitHub: "build" and "Build" are one
	// job): the first of a spelling is the job, a later one a duplicate.
	x.jobIDs = map[string]string{}
	yamldoc.Pairs(jobs, func(k, _ *yaml.Node) {
		if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" {
			return
		}
		if !jobID.MatchString(k.Value) {
			// GitHub accepts no other id; and the identities of triggers
			// ("::on:") and steps ("::job::step") rest on it.
			if yamldoc.IsName(k.Value) {
				x.problem(CodeInvalidJobID, x.span(a, k), false, fmt.Sprintf("%q is no job id GitHub accepts (a letter or _, then letters, digits, - and _): the job is not read", k.Value))
			} else {
				x.problem(CodeInvalidJobID, x.span(a, k), false, "a job id that is not one line of text: the job is not read")
			}
			return
		}
		folded := strings.ToLower(k.Value)
		if _, dup := x.jobIDs[folded]; dup {
			x.problem(CodeDuplicateJobID, x.span(a, k), false, fmt.Sprintf("the job id %q is stated twice (job ids are case-insensitive): the later one is not read", k.Value))
			return
		}
		x.jobIDs[folded] = k.Value
	})
	needs := map[string][]string{}
	var order []string
	keyOf := map[string]yamldoc.Span{}
	done := map[string]bool{}
	yamldoc.Pairs(jobs, func(k, v *yaml.Node) {
		if k.Kind != yaml.ScalarNode || k.Tag == "!!merge" || !jobID.MatchString(k.Value) {
			return
		}
		folded := strings.ToLower(k.Value)
		if x.jobIDs[folded] != k.Value || done[folded] {
			return // a duplicate, reported above
		}
		done[folded] = true
		job := x.job(a, wf.Qualified, k, v)
		if job == "" {
			return
		}
		order = append(order, folded)
		keyOf[folded] = x.span(a, k)
		needs[folded] = x.needsOf(a, v)
	})
	x.cycles(order, needs, keyOf)
}

func get(m *yaml.Node, key string) *yaml.Node {
	_, v := yamldoc.Get(m, key)
	return v
}

// triggers states the events of `on`: a name, a list of names or a
// mapping of events to their settings.
func (x *extractor) triggers(root *yaml.Node, wf string) {
	k, v := yamldoc.Get(root, "on")
	if k == nil {
		return
	}
	v, a := x.deref(at{}, v)
	seenEvents := map[string]bool{}
	event := func(name string, key, val *yaml.Node, a at) {
		if !yamldoc.IsName(name) || key.Tag == "!!null" {
			return // `on:` with no event names none; a text that is no name names none
		}
		var sp yamldoc.Span
		if val != nil {
			sp = x.entry(a, key, val)
		} else {
			sp = x.span(a, key)
		}
		// "<file>::on:<event>": no job id holds ":", so no step
		// ("<file>::<job>::<step>") of a job named "on" shares it.
		if seenEvents[name] {
			return // an event named twice is one trigger
		}
		seenEvents[name] = true
		t := Symbol{Name: name, Qualified: wf + "::on:" + name, Kind: symbol.KindTrigger, Span: sp, Parent: wf}
		if x.symbol(t) {
			x.contains(wf, t, x.span(a, key))
		}
	}
	switch {
	case v == nil:
	case v.Kind == yaml.ScalarNode:
		event(v.Value, v, nil, a)
	case v.Kind == yaml.SequenceNode:
		for _, it := range v.Content {
			it, ia := x.deref(a, it)
			if it != nil && it.Kind == yaml.ScalarNode {
				event(it.Value, it, nil, ia)
			}
		}
	case v.Kind == yaml.MappingNode:
		x.mergeKeys(v)
		yamldoc.Pairs(v, func(ek, ev *yaml.Node) {
			if ek.Kind == yaml.ScalarNode && ek.Tag != "!!merge" {
				event(ek.Value, ek, ev, a)
			}
		})
	}
}

// job states one job and what it states; it returns the job's identity, or
// "" when the file's budget is spent.
func (x *extractor) job(a at, wf string, k, v *yaml.Node) string {
	body, ba := x.deref(a, v)
	sig := ""
	if r, ok := yamldoc.Scalar(get(body, "runs-on")); ok {
		sig = "runs-on: " + r
	}
	if u, ok := yamldoc.Scalar(get(body, "uses")); ok {
		sig = "uses: " + u
	}
	job := Symbol{Name: k.Value, Qualified: wf + "::" + k.Value, Kind: symbol.KindJob, Span: x.entry(a, k, v), Parent: wf, Signature: sig}
	if !x.symbol(job) {
		return ""
	}
	x.contains(wf, job, x.span(a, k))
	if body == nil || body.Kind != yaml.MappingNode {
		return job.Qualified
	}
	x.mergeKeys(body)
	x.keys(ba, body, jobKeys, "/jobs/"+pointer(k.Value), job.Qualified)
	// needs: the jobs this one runs after.
	if nk, nv := yamldoc.Get(body, "needs"); nk != nil {
		x.forNames(ba, nv, func(name string, sp yamldoc.Span) {
			r := Ref{Name: name, Kind: string(reference.KindExplicitDependency), Container: job.Qualified, Span: sp,
				InRepository: true, TargetKinds: string(symbol.KindJob)}
			switch id, declared := x.jobIDs[strings.ToLower(name)]; {
			case strings.Contains(name, "${{"):
				r.Dynamic = true
			case declared:
				r.NameQualified = wf + "::" + id // job ids are case-insensitive
			default:
				r.NameQualified = wf + "::" + name
			}
			x.ref(r)
		})
	}
	if uk, uv := yamldoc.Get(body, "uses"); uk != nil {
		uv, ua := x.deref(ba, uv)
		if uv != nil && uv.Kind == yaml.ScalarNode {
			x.uses(job.Qualified, uv.Value, x.span(ua, uv), true)
		}
	}
	if sk, sv := yamldoc.Get(body, "steps"); sk != nil {
		x.steps(ba, job.Qualified, sv)
	}
	return job.Qualified
}

// forNames calls f with each name of a scalar or a sequence of scalars.
func (x *extractor) forNames(a at, n *yaml.Node, f func(string, yamldoc.Span)) {
	n, a = x.deref(a, n)
	switch {
	case n == nil:
	case n.Kind == yaml.ScalarNode:
		if n.Tag != "!!null" && yamldoc.IsName(n.Value) {
			f(n.Value, x.span(a, n))
		}
	case n.Kind == yaml.SequenceNode:
		for _, it := range n.Content {
			if x.refBudget <= 0 {
				x.truncate(x.span(a, n))
				return
			}
			it, ia := x.deref(a, it)
			if it != nil && it.Kind == yaml.ScalarNode && it.Tag != "!!null" && yamldoc.IsName(it.Value) {
				f(it.Value, x.span(ia, it))
			}
		}
	}
}

// needsOf returns the job ids (case-folded) a job's needs states, as many
// as the file's needs budget leaves.
func (x *extractor) needsOf(a at, v *yaml.Node) []string {
	body, ba := x.deref(a, v)
	var out []string
	if nk, nv := yamldoc.Get(body, "needs"); nk != nil {
		x.forNames(ba, nv, func(name string, sp yamldoc.Span) {
			if x.needsBudget <= 0 {
				x.truncate(sp)
				return
			}
			x.needsBudget--
			out = append(out, strings.ToLower(name))
		})
	}
	return out
}

// cycles reports the jobs whose needs lead back to them: GitHub runs no
// such workflow.
func (x *extractor) cycles(order []string, needs map[string][]string, keyOf map[string]yamldoc.Span) {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	onCycle := map[string]bool{}
	var stack []string
	var visit func(string)
	visit = func(id string) {
		color[id] = grey
		stack = append(stack, id)
		for _, n := range needs[id] {
			if _, ok := needs[n]; !ok {
				continue
			}
			switch color[n] {
			case white:
				visit(n)
			case grey:
				for i := len(stack) - 1; i >= 0; i-- {
					onCycle[stack[i]] = true
					if stack[i] == n {
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	for _, id := range order {
		if color[id] == white {
			visit(id)
		}
	}
	for _, id := range order {
		if onCycle[id] {
			x.problem(CodeNeedsCycle, keyOf[id], false, fmt.Sprintf("the job %q needs itself through its needs: GitHub does not run the workflow", x.jobIDs[id]))
		}
	}
}

// steps states the steps of a job or a composite action.
func (x *extractor) steps(a at, parent string, v *yaml.Node) {
	v, a = x.deref(a, v)
	if v == nil || v.Kind != yaml.SequenceNode {
		return
	}
	used := map[string]int{}    // steps of each key so far
	locals := map[string]bool{} // the local names given
	ids := map[string]bool{}    // step ids, case-folded (they are case-insensitive)
	for _, it := range v.Content {
		item, ia := x.deref(a, it)
		if item == nil || item.Kind != yaml.MappingNode {
			continue
		}
		x.mergeKeys(item)
		key := ""
		id, hasID := yamldoc.Scalar(get(item, "id"))
		if hasID && yamldoc.IsName(id) {
			key = id
			if ids[strings.ToLower(id)] {
				_, iv := yamldoc.Get(item, "id")
				x.problem(CodeDuplicateStepID, x.span(ia, iv), false, fmt.Sprintf("the step id %q is stated twice in one job (step ids are case-insensitive)", id))
			}
			ids[strings.ToLower(id)] = true
		}
		name, _ := yamldoc.Scalar(get(item, "name"))
		usesVal, hasUses := yamldoc.Scalar(get(item, "uses"))
		usesName := withoutUserinfo(usesVal)
		_, hasRun := yamldoc.Get(item, "run")
		switch {
		case key != "":
		case yamldoc.IsName(name):
			key = name
		case hasUses && yamldoc.IsName(usesName):
			// The action without its ref: a ref an update bot moves
			// leaves the step's identity as it was.
			key = usesName
			if i := strings.LastIndex(key, "@"); i > 0 && !strings.HasPrefix(key, "./") && !strings.HasPrefix(key, "docker://") {
				key = strings.TrimSuffix(key[:i], "/")
			}
		case hasRun != nil:
			key = "run"
		default:
			key = "step"
		}
		// The n-th step of a key is "<key>#n" — the next n no step's own
		// key ("foo#2" written as a name) has taken.
		local := key
		for n := max(used[key]+1, 2); locals[local]; n++ {
			local = fmt.Sprintf("%s#%d", key, n)
			used[key] = n
		}
		if used[key] == 0 {
			used[key] = 1
		}
		locals[local] = true
		sig := ""
		switch {
		case hasUses:
			sig = "uses: " + usesName
		case hasRun != nil:
			sig = "run"
			if sh, ok := yamldoc.Scalar(get(item, "shell")); ok {
				sig += " (" + sh + ")"
			}
		}
		st := Symbol{Name: key, Qualified: parent + "::" + local, Kind: symbol.KindStep, Span: x.span(ia, item), Parent: parent, Signature: sig}
		if !x.symbol(st) {
			return
		}
		first := item
		if len(item.Content) > 0 {
			first = item.Content[0]
		}
		x.contains(parent, st, x.span(ia, first))
		if uk, uv := yamldoc.Get(item, "uses"); uk != nil {
			uv, ua := x.deref(ia, uv)
			if uv != nil && uv.Kind == yaml.ScalarNode {
				x.uses(st.Qualified, uv.Value, x.span(ua, uv), false)
			}
		}
	}
}

// uses states the reference of a `uses`: of a job (a reusable workflow) or
// of a step (an action).
func (x *extractor) uses(container, val string, sp yamldoc.Span, job bool) {
	target := symbol.KindAction
	if job {
		target = symbol.KindWorkflow
	}
	val = withoutUserinfo(val)
	r := Ref{Name: val, Kind: string(reference.KindCall), Container: container, Span: sp, TargetKinds: string(target)}
	switch {
	case !yamldoc.IsName(val):
		// A multi-line, control-character or overlong text is no reference
		// (and no name to show).
		x.invalid(sp, "")
		return
	case strings.Contains(val, "${{"):
		r.Dynamic = true
	case strings.HasPrefix(val, "./"):
		// The path names a directory of the workspace: one that leaves it
		// ("./../x") names nothing of the repository.
		rel := path.Clean(strings.TrimPrefix(val, "./"))
		p := path.Clean(x.root + rel)
		switch {
		case rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/"):
			x.invalid(sp, val)
			return
		case job && !localWorkflow(strings.TrimPrefix(val, "./")):
			// GitHub calls a local reusable workflow only of the
			// repository's .github/workflows directory itself.
			x.invalid(sp, val)
			return
		case job:
			// A local reusable workflow is the file at the caller's commit.
			r.NameQualified, r.FileIdentity, r.InRepository = x.filePath(p), true, true
		default:
			// A local action: a directory of the runner's workspace.
			r.NameQualified, r.InRepository, r.Cap = x.filePath(p), true, "strong"
		}
	case strings.HasPrefix(val, "docker://"):
		if job || len(val) == len("docker://") {
			x.invalid(sp, val)
			return
		}
		r.NameQualified = "remote:" + val
	default:
		ref, at, ok := strings.Cut(val, "@")
		ref = strings.TrimSuffix(ref, "/") // owner/repo/path/@ref
		parts := strings.Split(ref, "/")
		valid := ok && at != "" && !strings.Contains(at, "@") && len(parts) >= 2 && !strings.ContainsAny(val, " \t\n")
		for _, p := range parts {
			valid = valid && p != "" && p != "." && p != ".."
		}
		if job {
			// owner/repo/.github/workflows/x.yml@ref
			valid = valid && len(parts) == 5 && parts[2] == ".github" && parts[3] == "workflows" &&
				(path.Ext(ref) == ".yml" || path.Ext(ref) == ".yaml")
		}
		if !valid {
			x.invalid(sp, val)
			return
		}
		r.Name = ref
		r.NameQualified = "remote:" + val
	}
	x.ref(r)
}

// filePath spells a slash-separated repository path as the index's file
// IDs are spelled.
func (x *extractor) filePath(p string) string { return strings.ReplaceAll(p, "/", x.sep) }

func (x *extractor) invalid(sp yamldoc.Span, val string) {
	if val == "" {
		x.problem(CodeInvalidUses, sp, false, "a uses that is not one line of text is no reference GitHub Actions accepts")
		return
	}
	x.problem(CodeInvalidUses, sp, false, fmt.Sprintf("%q is no reference GitHub Actions accepts in uses", val))
}

// localWorkflow reports whether a workspace path names a workflow file of
// the .github/workflows directory itself (no subdirectory), as GitHub
// requires of a local reusable workflow.
func localWorkflow(rel string) bool {
	dir, file := path.Split(rel)
	ext := path.Ext(file)
	return dir == ".github/workflows/" && (ext == ".yml" || ext == ".yaml") && len(file) > len(ext)
}

// withoutUserinfo removes the credentials a docker:// reference may carry
// (docker://user:token@host/image): they are no part of its name.
func withoutUserinfo(val string) string {
	rest, ok := strings.CutPrefix(val, "docker://")
	if !ok {
		return val
	}
	host, _, _ := strings.Cut(rest, "/")
	if i := strings.LastIndex(host, "@"); i >= 0 {
		return "docker://" + rest[i+1:]
	}
	return val
}

var actionKeys = []string{"inputs", "outputs", "runs"}

func (x *extractor) action() {
	dir := path.Dir(strings.ReplaceAll(x.file, "\\", "/"))
	name := path.Base(dir)
	if dir == "." {
		name = "action"
	}
	root := x.document()
	sig := ""
	if u, ok := yamldoc.Scalar(get(get(root, "runs"), "using")); ok {
		sig = "using: " + u
	}
	act := Symbol{Name: name, Qualified: x.filePath(dir), Kind: symbol.KindAction, Span: yamldoc.Span{Start: 0, End: len(x.s.Src)}, Signature: sig}
	x.symbol(act)
	if root == nil {
		return
	}
	x.mergeKeys(root)
	x.keys(at{}, root, actionKeys, "", act.Qualified)
	for _, group := range []string{"inputs", "outputs"} {
		g, ga := x.deref(at{}, get(root, group))
		seen := map[string]bool{}
		yamldoc.Pairs(g, func(k, v *yaml.Node) {
			if k.Kind == yaml.ScalarNode && k.Tag != "!!merge" && yamldoc.IsName(k.Value) && !seen[k.Value] {
				seen[k.Value] = true
				ptr := "/" + group + "/" + pointer(k.Value)
				x.symbol(Symbol{Name: ptr, Qualified: x.file + "#" + ptr, Kind: symbol.KindKey,
					Span: x.entry(ga, k, v), Parent: x.file + "#/" + group})
			}
		})
	}
	runs, ra := x.deref(at{}, get(root, "runs"))
	if sk, sv := yamldoc.Get(runs, "steps"); sk != nil {
		x.steps(ra, act.Qualified, sv)
	}
}
