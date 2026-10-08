// Package terraform provides a Tree-sitter-based extraction Provider for
// Terraform configuration (`.tf`) and variable-definition (`.tfvars`) files.
//
// HCL is the syntax; Terraform is the configuration language built on it, and
// everything this package states is Terraform semantics. It is never applied
// to other HCL products (Packer, Nomad, Consul, Terragrunt): `.hcl` files are
// not registered, because an extension cannot tell a Terraform test file from
// a Packer template, and generic HCL has no shared declaration semantics.
// `.tf.json` / `.tfvars.json` (Terraform's JSON syntax) are not extracted.
//
// # Symbols and identity
//
// Every top-level declaration of a `.tf` file is one symbol. Its Name is the
// Terraform address within its module (`aws_vpc.main`, `data.aws_ami.ubuntu`,
// `ephemeral.T.N`, `module.network`, `var.region`, `local.region`,
// `output.vpc_id`, `provider.aws` / `provider.aws.west`, `check.health`,
// `terraform`). An address is unique only within its module, and a Terraform
// module is a directory: every `.tf` file of one directory belongs to it, a
// subdirectory is another module. Qualified therefore prefixes the address
// with the module directory (`modules/network/aws_vpc.main`; a declaration of
// the root directory has the bare address). The directory is derived lexically
// from the file's own repository-relative path; no other file is consulted.
//
// # References
//
// A reference is a static traversal in an expression (implicit dependency,
// reference.KindValueReference), an element of `depends_on` (explicit
// dependency, KindExplicitDependency), or a `provider` / `providers`
// meta-argument (the provider configuration a declaration uses). Its Name is
// the address it denotes; the attribute path after it (`.id`, `[0]`, `[*]`)
// is not part of it. NameQualified is the address qualified by the module the
// reference is written in, with IdentityInRepository: the language fixes the
// scope, so the resolver finds the one declaration of that module (Exact),
// several (Candidate: an invalid duplicate), or none (Unresolved — undeclared,
// or declared in a `.tf.json` file — never another module's declaration and
// never OutsideRepository). The file is IdentityOnly, so no name heuristic
// reaches a Terraform symbol and no Terraform name reaches another
// language's symbol.
//
// `module.NAME.OUTPUT` is two references: the module call `module.NAME`, and
// the output OUTPUT through the module call (ReceiverTypeQualified). Each
// input argument of a module block (every attribute but source, version,
// count, for_each, providers and depends_on) is a NamedArgument reference
// from the call to the child's `variable` of that name, through the call's
// ParameterScope: the call refers to the variable's declaration as it does to
// an output's. The value flowing from the argument expression into the child
// is not modelled; neither is an input the call omits. A module
// call whose `source` is a local path ("./", "../") states the child module
// directory lexically (SymbolDraft.MemberScope, ParameterScope), so the
// output resolves to the child's `output.OUTPUT` and an argument to its
// `var.NAME`; any other source is fetched by Terraform from
// outside the repository (MembersOutside), as is a local path leaving the
// indexed root. Remote sources are never fetched; nothing is executed.
//
// Not references (never observed): built-in and contextual values (`path.*`,
// `terraform.*`, `count.*`, `each.*`, `self.*`, `caller`), roots Terraform
// reserves and rejects (`plan`, `state`, `template`, `lazy`, `arg`), for-expression and template
// `for` variables, `dynamic` block iterators, object keys written as bare
// names, function names, `lifecycle.ignore_changes` (attribute paths of the
// resource itself), `variable.type` (type constraints), the `terraform`
// block, `moved` / `import` / `removed` blocks (address metadata), block types
// Terraform does not define at the top level, and anything inside a syntax
// error. A traversal whose shape is no address (`foo`, `aws_vpc[0]`) denotes
// no declaration and is not observed either.
//
// A managed resource whose type would read as another reference root or as
// one of the namespaces above (`resource "output" "x"`, `resource "var"
// "x"`) has the address `resource.TYPE.NAME`, the only spelling Terraform
// accepts for it; conversely `output.x`, `check.x` and `provider.x` in an
// expression are such resources (they are no reference roots in a
// configuration file) and never denote the output, check or provider block.
//
// A local module source starts with `./`, `../`, `.\` or `..\`; backslashes
// are separators on every platform and the path is cleaned lexically, as
// Terraform does before it evaluates symlinks. The index walks real
// directories only, so a module reached only through a symlinked directory
// is Unresolved.
//
// Override files (`override.tf`, `*_override.tf`) merge into declarations of
// other files: they declare no symbols, and their references have no
// container — except a module block that replaces the call's `source`. It is
// declared beside the original call, so the call and its outputs are
// ambiguous (Candidate) instead of resolved through the original source,
// which the merge may have replaced. Other overridden arguments can leave
// edges of the original declaration that the merge removes: an
// over-approximation, never another target. In a `.tfvars` file each top-level assignment is a write of the
// input variable it names; which module receives the values is decided on the
// command line, so the write carries no identity.
//
// Invariants: Extract never panics, never returns a Tree-sitter node, keeps
// every byte range inside the source, and bounds its recursion
// (maxExprDepth).
package terraform

import (
	"context"
	"path"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Provider extracts code intelligence from Terraform files.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) Language() language.Language { return "terraform" }

// Extensions: `.tfvars` also covers `*.auto.tfvars`.
func (p *Provider) Extensions() []string { return []string{".tf", ".tfvars"} }

// CacheVersion must change whenever extraction semantics change.
func (p *Provider) CacheVersion() string { return "terraform-4" }

// maxSyntaxDiagnostics bounds the syntax-error diagnostics of one file.
const maxSyntaxDiagnostics = 5

func (p *Provider) Extract(_ context.Context, file source.FileID, src []byte) (language.Extraction, error) {
	lang := grammars.HclLanguage()
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		return language.Extraction{
			Diagnostics: []language.Diagnostic{{
				Severity: language.SeverityError,
				Message:  "parse failed: " + err.Error(),
				Location: source.Location{File: file},
			}},
			IdentityOnly: true,
		}, nil
	}
	defer tree.Release()

	x := &extractor{
		lang: lang,
		src:  src,
		file: file,
		dir:  moduleDir(file),
		vars: isVarsFile(file),
		seen: make(map[string]bool),
	}
	root := tree.RootNode()
	x.syntaxDiagnostics(root)
	for _, body := range topLevelBodies(root, lang) {
		x.topLevel(body, isOverrideFile(file))
	}
	return language.Extraction{
		Symbols:      x.symbols,
		References:   x.refs,
		Diagnostics:  x.diags,
		IdentityOnly: true,
	}, nil
}

// moduleDir is the repository-relative, slash-separated directory of the
// module file belongs to ("." for the root).
func moduleDir(file source.FileID) string {
	return path.Dir(filepath.ToSlash(string(file)))
}

// qualify composes the qualified identity of a Terraform address declared in
// module directory dir.
func qualify(dir, address string) string {
	if dir == "." || dir == "" {
		return address
	}
	return dir + "/" + address
}

func baseName(file source.FileID) string {
	return strings.ToLower(path.Base(filepath.ToSlash(string(file))))
}

func isVarsFile(file source.FileID) bool {
	return strings.HasSuffix(baseName(file), ".tfvars")
}

// isOverrideFile reports Terraform's override-file naming rule.
func isOverrideFile(file source.FileID) bool {
	b := baseName(file)
	return b == "override.tf" || strings.HasSuffix(b, "_override.tf")
}

// topLevelBodies returns the body nodes holding the file's top-level
// blocks and attributes. A body inside a top-level ERROR node is included:
// the declarations a syntax error did not consume stay extractable.
func topLevelBodies(root *ts.Node, lang *ts.Language) []*ts.Node {
	var out []*ts.Node
	for i := 0; i < root.ChildCount(); i++ {
		c := root.Child(i)
		switch c.Type(lang) {
		case "body":
			out = append(out, c)
		case "ERROR":
			for j := 0; j < c.ChildCount(); j++ {
				if g := c.Child(j); g.Type(lang) == "body" {
					out = append(out, g)
				}
			}
		}
	}
	return out
}

// syntaxDiagnostics reports the first maxSyntaxDiagnostics syntax errors.
func (x *extractor) syntaxDiagnostics(root *ts.Node) {
	if !root.HasError() {
		return
	}
	n := 0
	var walk func(node *ts.Node, depth int)
	walk = func(node *ts.Node, depth int) {
		if n >= maxSyntaxDiagnostics || depth > maxExprDepth {
			return
		}
		if node.IsError() || node.IsMissing() {
			n++
			msg := "syntax error"
			if node.IsMissing() {
				msg = "syntax error: missing " + node.Type(x.lang)
			}
			x.diags = append(x.diags, language.Diagnostic{
				Severity: language.SeverityError,
				Message:  msg,
				Location: nodeLocation(node, x.file),
			})
			return
		}
		if !node.HasError() {
			return
		}
		for i := 0; i < node.ChildCount(); i++ {
			walk(node.Child(i), depth+1)
		}
	}
	walk(root, 0)
}

func nodeLocation(node *ts.Node, file source.FileID) source.Location {
	return spanLocation(node, node, file)
}

// spanLocation is the location from the start of first to the end of last.
func spanLocation(first, last *ts.Node, file source.FileID) source.Location {
	return source.Location{
		File: file,
		Range: source.Range{
			Start: source.Position{Line: first.StartPoint().Row + 1, Column: first.StartPoint().Column + 1},
			End:   source.Position{Line: last.EndPoint().Row + 1, Column: last.EndPoint().Column + 1},
		},
	}
}
