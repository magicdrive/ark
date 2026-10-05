package skill_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/instruction"
	"github.com/magicdrive/ark/internal/mcp"
	"github.com/magicdrive/ark/internal/skill"
)

// ---- helpers -------------------------------------------------------------------

var backtickedTool = regexp.MustCompile("`(?:mcp__ark__)?([a-z]+(?:_[a-z]+)+)`")

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// artifacts generates both skill types (no slash-command install) and returns
// name → content for every generated artifact file.
func artifacts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	repo := filepath.Join(t.TempDir(), "repo-skill")
	analysis := &skill.RepoAnalysis{ProjectName: "demo", PrimaryLang: "Go",
		Languages: []skill.LanguageInfo{{Name: "Go", FileCount: 3}}}
	if err := skill.GenerateRepository(skill.RepositoryOptions{Name: "repo-skill", Output: repo, Analysis: analysis, NoInstall: true}); err != nil {
		t.Fatal(err)
	}
	expl := filepath.Join(t.TempDir(), "explorer-skill")
	if err := skill.GenerateExplorer(skill.ExplorerOptions{Name: "explorer-skill", Output: expl, NoInstall: true}); err != nil {
		t.Fatal(err)
	}
	for label, dir := range map[string]string{"repository": repo, "explorer": expl} {
		for _, f := range []string{"SKILL.md", "agents/openai.yaml", "agents/claude-code.md"} {
			out[label+"/"+f] = read(t, filepath.Join(dir, f))
		}
	}
	return out
}

// bodyOf strips the Ark frontmatter of SKILL.md (it carries a timestamp).
func bodyOf(content string) string { return skill.ExtractContentWithoutFrontmatter(content) }

// usageModel asserts the Ark v4 usage model on a piece of guidance text. The
// same contract is applied to the standing instruction and to every skill
// artifact, so the two can differ in depth but never in the model they teach.
// bare reports whether tool names appear without the mcp__ark__ prefix.
func usageModel(t *testing.T, label, text string, bare bool) {
	t.Helper()
	name := func(tool string) string {
		if bare {
			return "`" + tool + "`"
		}
		return "`mcp__ark__" + tool + "`"
	}
	lineWith := func(tool string) string {
		for _, l := range strings.Split(text, "\n") {
			if strings.Contains(l, name(tool)) {
				return strings.ToLower(l)
			}
		}
		return ""
	}
	fail := func(format string, args ...any) { t.Helper(); t.Errorf("["+label+"] "+format, args...) }

	// Repository map is architecture-oriented and comes before the directory tree,
	// which is conditional on the layout itself being the point.
	if i, j := strings.Index(text, name("get_repository_map")), strings.Index(text, name("get_directory_tree")); i < 0 || j < 0 || i > j {
		fail("get_repository_map must be introduced before get_directory_tree")
	}
	if l := lineWith("get_repository_map"); !strings.Contains(l, "architecture") && !strings.Contains(l, "organised") {
		fail("get_repository_map is not tied to repository structure/architecture: %q", l)
	}
	if l := lineWith("get_directory_tree"); !strings.Contains(l, "layout") {
		fail("get_directory_tree is not tied to the directory layout: %q", l)
	}
	// Context is preferred for understanding/modifying a symbol; get_symbol is the exact-source tool.
	if l := lineWith("get_context"); !strings.Contains(l, "understand") && !strings.Contains(l, "modif") {
		fail("get_context is not tied to understanding/modifying a symbol: %q", l)
	}
	if !strings.Contains(strings.ToLower(text), "exact") || lineWith("get_symbol") == "" {
		fail("get_symbol / exact source guidance missing")
	}
	// Graph tools.
	for _, tool := range []string{"get_relations", "get_callers", "get_callees"} {
		if lineWith(tool) == "" {
			fail("%s is not described", tool)
		}
	}
	// Impact analysis is conditional, and heuristic results are not facts (skill) / small changes skip it.
	if l := lineWith("analyze_change_impact"); !strings.Contains(l, "downstream") && !strings.Contains(l, "beyond") && !strings.Contains(l, "affect") {
		fail("analyze_change_impact is not tied to downstream effects: %q", l)
	}
	if !strings.Contains(text, "Skip it for small, local changes") {
		fail("analyze_change_impact must be conditional (skip for small, local changes)")
	}
	// Search tools are distinguished.
	if lineWith("search_code") == "" || lineWith("search_in_files") == "" {
		fail("search_code / search_in_files are not both described")
	}
	// Whole-file reads are allowed when useful, not banned.
	if l := lineWith("get_file_content"); !strings.Contains(l, "genuinely useful") && !strings.Contains(text, "genuinely useful") {
		fail("whole-file reads must be allowed when genuinely useful")
	}
	// Ambiguity is never guessed away.
	for _, want := range []string{"ambiguous", "qualified name", "file filter"} {
		if !strings.Contains(text, want) {
			fail("uncertainty guidance lacks %q", want)
		}
	}
	// No absolute / ritual phrasing (the old model's: tree-first, never-read, last resort).
	for _, bad := range []string{"NEVER", "ALWAYS", "last resort", "First step", "absolutely necessary", "mandatory", "must start", "Start with get_directory_tree"} {
		if strings.Contains(text, bad) {
			fail("absolute/obsolete phrasing %q", bad)
		}
	}
}

// ---- the generated artifacts -----------------------------------------------------

func TestGeneratedSkills_FollowTheArkV4Model(t *testing.T) {
	for name, content := range artifacts(t) {
		bare := !strings.HasSuffix(name, "claude-code.md")
		usageModel(t, name, bodyOf(content), bare)
	}
}

// The old fixed navigation model is gone everywhere: no tree-first sequence in
// any form (numbered rule, arrow chain, diagram, tool table).
func TestGeneratedSkills_NoFixedTreeFirstWorkflow(t *testing.T) {
	chain := regexp.MustCompile("get_directory_tree`?\\s*(→|->|\\n\\s*↓)")
	for name, content := range artifacts(t) {
		body := bodyOf(content)
		if chain.MatchString(body) {
			t.Errorf("%s: get_directory_tree starts a tool chain", name)
		}
		for _, l := range strings.Split(body, "\n") {
			trim := strings.TrimSpace(l)
			if regexp.MustCompile(`^\d+\.\s`).MatchString(trim) && strings.Contains(trim, "get_directory_tree") {
				t.Errorf("%s: numbered step involving get_directory_tree: %q", name, trim)
			}
		}
		// Worked examples never begin with the directory tree.
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, "- **") && strings.Contains(l, ":**") {
				first := backtickedTool.FindStringSubmatch(l)
				if first != nil && first[1] == "get_directory_tree" && strings.Contains(l, " then ") {
					t.Errorf("%s: an example sequence starts with get_directory_tree: %q", name, l)
				}
			}
		}
		for _, old := range []string{"Do Not Read Entire Files Immediately", "Explore hierarchically", "Start exploration with", "Be specific", "Search smart"} {
			if strings.Contains(body, old) {
				t.Errorf("%s: still contains the old model's %q", name, old)
			}
		}
	}
}

// Tool-call inflation is discouraged explicitly, and examples are labelled as
// non-binding.
func TestGeneratedSkills_DiscourageToolRitual(t *testing.T) {
	for name, content := range artifacts(t) {
		body := bodyOf(content)
		for _, want := range []string{"smallest set", "stop exploring", "Nothing here is a required sequence", "not sequences to follow"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
	}
}

// Impact results that are heuristic are leads, never established dependencies.
func TestGeneratedSkills_HeuristicImpactIsNotCertain(t *testing.T) {
	for name, content := range artifacts(t) {
		body := bodyOf(content)
		if !strings.Contains(body, "possible dependents") || !strings.Contains(body, "not as guaranteed impact") {
			t.Errorf("%s: heuristic impact is not described as uncertain", name)
		}
		if !strings.Contains(body, "do not pick the first result") {
			t.Errorf("%s: ambiguity guidance does not forbid picking the first result", name)
		}
	}
}

// ---- consistency with the standing instruction ---------------------------------------

// Skill and instruction are different artifacts with different text, but they
// teach the same usage model: the one contract is applied to both.
func TestSkillAndInstructionShareTheUsageModel(t *testing.T) {
	usageModel(t, "instruction", instruction.Guidance(), true)
	cl, err := instruction.Render("claude")
	if err != nil {
		t.Fatal(err)
	}
	usageModel(t, "instruction(claude)", cl, false)

	// Same tool vocabulary in the guidance bodies: the skill covers every tool
	// the instruction recommends.
	skillBody := bodyOf(artifacts(t)["repository/SKILL.md"])
	for _, tool := range instruction.GuidanceTools() {
		if !strings.Contains(skillBody, "`"+tool+"`") {
			t.Errorf("the skill does not cover %s, which the instruction recommends", tool)
		}
	}
	// The skill is the richer artifact (examples, goal table), not a copy.
	if strings.Contains(skillBody, strings.TrimSpace(instruction.Guidance())) {
		t.Error("the skill must not embed the instruction text verbatim")
	}
}

// ---- artifact formats -------------------------------------------------------------------

func TestGeneratedSkills_ValidFormats(t *testing.T) {
	for label, parts := range map[string][]string{"repository": nil, "explorer": nil} {
		_ = parts
		a := artifacts(t)
		md := a[label+"/SKILL.md"]
		meta, err := skill.ParseFrontmatter(md)
		if err != nil || meta == nil || !meta.ArkManaged || meta.SkillType != label {
			t.Fatalf("%s SKILL.md frontmatter: %+v %v", label, meta, err)
		}
		if modified, err := skill.HasUserModifications(md); err != nil || modified {
			t.Errorf("%s SKILL.md: stored content-hash does not match the generated body (modified=%v err=%v)", label, modified, err)
		}

		// agents/openai.yaml: top-level keys, one literal block, a tools list.
		y := a[label+"/agents/openai.yaml"]
		if !strings.Contains(y, "\ninstructions: |\n") || !strings.Contains(y, "\ntools:\n") {
			t.Fatalf("%s openai.yaml lacks instructions/tools:\n%s", label, y)
		}
		block := y[strings.Index(y, "instructions: |\n")+len("instructions: |\n"):]
		block = block[:strings.Index(block, "\ntools:\n")]
		for _, l := range strings.Split(block, "\n") {
			if l != "" && !strings.HasPrefix(l, "  ") {
				t.Errorf("%s openai.yaml: instruction line is not indented: %q", label, l)
			}
		}
		for _, l := range strings.Split(y[strings.Index(y, "\ntools:\n")+len("\ntools:\n"):], "\n") {
			if l != "" && !strings.HasPrefix(l, "  - ") {
				t.Errorf("%s openai.yaml: bad tools entry %q", label, l)
			}
		}

		// agents/claude-code.md: frontmatter with prefixed tools, closed by ---.
		c := a[label+"/agents/claude-code.md"]
		if !strings.HasPrefix(c, "---\ndescription: ") || strings.Count(c, "\n---\n") < 1 {
			t.Fatalf("%s claude-code.md frontmatter malformed:\n%s", label, c[:200])
		}
		front := c[:strings.Index(c[4:], "\n---\n")+4]
		for _, l := range strings.Split(front, "\n") {
			if strings.HasPrefix(l, "  - ") && !strings.HasPrefix(l, "  - mcp__ark__") {
				t.Errorf("%s claude-code.md: tool not prefixed: %q", label, l)
			}
		}
		for _, tool := range []string{"get_context", "get_repository_map", "get_relations", "get_callers", "get_callees", "analyze_change_impact", "search_code"} {
			if !strings.Contains(front, "  - mcp__ark__"+tool+"\n") || !strings.Contains(y, "  - "+tool+"\n") {
				t.Errorf("%s: tool list does not grant %s", label, tool)
			}
		}
		// Claude presentation: no bare tool names left in the body.
		body := c[len(front):]
		for _, m := range regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`").FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "mcp__ark__") {
				t.Errorf("%s claude-code.md: bare tool name %q", label, m[1])
			}
		}
	}
}

// Same input, same bytes (SKILL.md ignoring its generation timestamp).
func TestGeneratedSkills_Deterministic(t *testing.T) {
	a, b := artifacts(t), artifacts(t)
	for name := range a {
		x, y := a[name], b[name]
		if strings.HasSuffix(name, "SKILL.md") {
			x, y = bodyOf(x), bodyOf(y)
		}
		if x != y {
			t.Errorf("%s is not deterministic", name)
		}
	}
}

// The slash command installed for Claude Code is the generated claude-code.md.
func TestRepositorySkill_InstalledSlashCommandMatches(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	out := filepath.Join(cwd, "skills", "demo")
	if err := skill.GenerateRepository(skill.RepositoryOptions{Name: "demo", Output: out}); err != nil {
		t.Fatal(err)
	}
	cmd := read(t, filepath.Join(cwd, ".claude", "commands", "demo.md"))
	if cmd != read(t, filepath.Join(out, "agents", "claude-code.md")) {
		t.Error("installed slash command differs from agents/claude-code.md")
	}
	usageModel(t, "slash command", bodyOf(cmd), false)
}

// ---- MCP inventory --------------------------------------------------------------------------

// Every tool a skill mentions or grants is registered by the MCP server, so the
// generated text cannot keep naming a removed or renamed tool.
func TestSkillToolsExistInMCPServer(t *testing.T) {
	registered := map[string]bool{}
	for _, tool := range mcp.NewToolsHandler(".", nil).ListTools() {
		registered[tool.Name] = true
	}
	if len(registered) == 0 {
		t.Fatal("no MCP tools registered")
	}
	seen := map[string]bool{}
	for name, content := range artifacts(t) {
		for _, m := range backtickedTool.FindAllStringSubmatch(content, -1) {
			seen[m[1]] = true
			if !registered[m[1]] {
				t.Errorf("%s names %q, which the MCP server does not register", name, m[1])
			}
		}
		for _, l := range strings.Split(content, "\n") {
			if tool, ok := strings.CutPrefix(strings.TrimSpace(l), "- "); ok && regexp.MustCompile(`^(mcp__ark__)?[a-z_]+$`).MatchString(tool) {
				if !registered[strings.TrimPrefix(tool, "mcp__ark__")] {
					t.Errorf("%s grants unregistered tool %q", name, tool)
				}
			}
		}
	}
	if len(seen) < 12 {
		t.Errorf("only %d distinct tools referenced: %v", len(seen), seen)
	}
}

// ---- update path & legacy generator ----------------------------------------------------------

// A skill generated by an older Ark (never edited) is brought to the v4 content
// by the existing `ark skill update`; no migration logic is involved.
func TestUpdate_BringsOldUneditedSkillToV4(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "ark-code-explorer")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "# ark-code-explorer\n\n## Core Workflow\n\nget_directory_tree\n    ↓\nget_symbols\n\n1. **Never read entire files first**\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill.WrapWithFrontmatter(old, skill.SkillTypeExplorer)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.NewUpdater(root).Update(false); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(dir, "SKILL.md"))
	usageModel(t, "updated SKILL.md", bodyOf(got), true)
	if strings.Contains(got, "Never read entire files first") {
		t.Error("old content survived the update")
	}
	if modified, _ := skill.HasUserModifications(got); modified {
		t.Error("updated SKILL.md has an inconsistent content-hash")
	}
}

// The legacy generator is still exported; it must not keep teaching the old model.
func TestLegacyGenerate_AlsoV4(t *testing.T) {
	out := filepath.Join(t.TempDir(), "legacy")
	if err := skill.Generate(skill.Options{Name: "legacy", Output: out}); err != nil {
		t.Fatal(err)
	}
	usageModel(t, "legacy SKILL.md", read(t, filepath.Join(out, "SKILL.md")), true)
	y := read(t, filepath.Join(out, "agents", "openai.yaml"))
	if strings.Contains(y, "NEVER") || !strings.Contains(y, "get_context") {
		t.Errorf("legacy openai.yaml is not v4:\n%s", y)
	}
}
