package toml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

func extract(t testing.TB, file, src string) language.Extraction {
	t.Helper()
	ex, err := NewProvider().Extract(context.Background(), source.FileID(file), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func fixture(t testing.TB, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureRepo, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// symbolLines renders "kind name @line:col-line:col <parent> signature".
func symbolLines(ex language.Extraction, kinds ...string) []string {
	var out []string
	for _, s := range ex.Symbols {
		if len(kinds) > 0 && !contains(kinds, string(s.Kind)) {
			continue
		}
		r := s.Location.Range
		out = append(out, fmt.Sprintf("%s %s @%d:%d-%d:%d <%s> %s", s.Kind, s.Name, r.Start.Line, r.Start.Column, r.End.Line, r.End.Column, s.Parent, s.Signature))
	}
	return out
}

// refLines renders "kind container -> name [nameQualified] flags", but
// contains.
func refLines(ex language.Extraction) []string {
	var out []string
	for _, r := range ex.References {
		if r.Kind == "contains" {
			continue
		}
		flags := ""
		if r.IdentityInRepository {
			flags += " in-repo"
		}
		if r.ConfidenceCap != "" {
			flags += " cap=" + r.ConfidenceCap
		}
		out = append(out, fmt.Sprintf("%s %s -> %s [%s]%s @%d:%d", r.Kind, r.Container, r.Name, r.NameQualified, flags, r.Location.Range.Start.Line, r.Location.Range.Start.Column))
	}
	return out
}

func diagLines(ex language.Extraction) []string {
	var out []string
	for _, d := range ex.Diagnostics {
		r := d.Location.Range
		out = append(out, fmt.Sprintf("%s %s @%d:%d-%d:%d %s", d.Severity, d.Code, r.Start.Line, r.Start.Column, r.End.Line, r.End.Column, d.Message))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Scenario: the tables and keys of any TOML file as symbols — a header
// table, a table a header implies, a dotted-key table, an inline table, a
// quoted key with "/" (~1), an array of tables named by name (else #<n>),
// a multi-line string, a date-time — each contained by its table.
func TestScenario_AnyTOML(t *testing.T) {
	ex := extract(t, "config/app.toml", fixture(t, "config/app.toml"))
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document app.toml @1:1-33:1 <> ",
		"key /title @1:1-1:23 <config/app.toml> string",
		"table /owner @3:1-5:32 <config/app.toml> [owner]",
		"key /owner/name @4:1-4:13 <config/app.toml#/owner> string",
		"key /owner/dob @5:1-5:32 <config/app.toml#/owner> offset-date-time",
		"table /database @7:1-10:43 <config/app.toml> [database]",
		"key /database/enabled @8:1-8:15 <config/app.toml#/database> boolean",
		"key /database/ports @9:1-9:21 <config/app.toml#/database> array",
		"table /database/temp_targets @10:1-10:43 <config/app.toml#/database> inline table",
		"key /database/temp_targets/cpu @10:18-10:28 <config/app.toml#/database/temp_targets> float",
		"key /database/temp_targets/case @10:30-10:41 <config/app.toml#/database/temp_targets> float",
		"table /servers @12:1-16:16 <config/app.toml> table (implicit)",
		"table /servers/alpha @12:1-13:16 <config/app.toml#/servers> [servers.alpha]",
		"key /servers/alpha/ip @13:1-13:16 <config/app.toml#/servers/alpha> string",
		"table /servers/beta~1gamma @15:1-16:16 <config/app.toml#/servers> [servers.\"beta/gamma\"]",
		"key /servers/beta~1gamma/ip @16:1-16:16 <config/app.toml#/servers/beta~1gamma> string",
		"key /products @18:1-26:15 <config/app.toml> array of tables",
		"table /products/Hammer @18:1-20:16 <config/app.toml#/products> [[products]]",
		"key /products/Hammer/name @19:1-19:16 <config/app.toml#/products/Hammer> string",
		"key /products/Hammer/sku @20:1-20:16 <config/app.toml#/products/Hammer> integer",
		"table /products/#1 @22:1-22:13 <config/app.toml#/products> [[products]]",
		"table /products/Nail @24:1-26:15 <config/app.toml#/products> [[products]]",
		"key /products/Nail/name @25:1-25:14 <config/app.toml#/products/Nail> string",
		"key /products/Nail/color @26:1-26:15 <config/app.toml#/products/Nail> string",
		"table /text @28:1-32:33 <config/app.toml> [text]",
		"key /text/poem @29:1-31:20 <config/app.toml#/text> string",
		"table /text/site @32:1-32:33 <config/app.toml#/text> table (dotted keys)",
		"key /text/site/url @32:1-32:33 <config/app.toml#/text/site> string",
	})
	equalLines(t, "references", refLines(ex), nil)
	equalLines(t, "diagnostics", diagLines(ex), nil)
}

// Scenarios: a Cargo package's name (the [package] table, kind package)
// and its dependencies (kind dependency, named by their keys) — normal,
// renamed (package =), git (credentials removed), path (in and out of the
// repository), registry, dev, build, target — with the package's
// depends_on each and each one's depends_on what it names; features
// reference what they enable.
func TestScenario_CargoPackageAndDependencies(t *testing.T) {
	ex := extract(t, "crates/app/Cargo.toml", fixture(t, "crates/app/Cargo.toml"))
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document Cargo.toml @1:1-39:1 <> ",
		"package app @1:1-4:17 <crates/app/Cargo.toml> package app 0.3.1",
		"key /package/name @2:1-2:13 <crates/app/Cargo.toml#/package> string",
		"key /package/version @3:1-3:18 <crates/app/Cargo.toml#/package> string",
		"key /package/edition @4:1-4:17 <crates/app/Cargo.toml#/package> string",
		"table /dependencies @6:1-17:22 <crates/app/Cargo.toml> [dependencies]",
		"dependency core @7:1-7:50 <crates/app/Cargo.toml#/dependencies> dependency core-lib path ../core",
		"key /dependencies/core/path @7:10-7:26 <crates/app/Cargo.toml#/dependencies/core> string",
		"key /dependencies/core/package @7:28-7:48 <crates/app/Cargo.toml#/dependencies/core> string",
		"dependency serde @8:1-8:29 <crates/app/Cargo.toml#/dependencies> dependency workspace",
		"key /dependencies/serde/workspace @8:11-8:27 <crates/app/Cargo.toml#/dependencies/serde> boolean",
		"dependency regex @9:1-9:12 <crates/app/Cargo.toml#/dependencies> dependency crates.io",
		"dependency json @10:1-10:49 <crates/app/Cargo.toml#/dependencies> dependency serde_json crates.io",
		"key /dependencies/json/package @10:10-10:32 <crates/app/Cargo.toml#/dependencies/json> string",
		"key /dependencies/json/version @10:34-10:47 <crates/app/Cargo.toml#/dependencies/json> string",
		"dependency private @11:1-11:95 <crates/app/Cargo.toml#/dependencies> dependency git",
		"key /dependencies/private/git @11:13-11:76 <crates/app/Cargo.toml#/dependencies/private> string",
		"key /dependencies/private/branch @11:78-11:93 <crates/app/Cargo.toml#/dependencies/private> string",
		"dependency outside @12:1-12:40 <crates/app/Cargo.toml#/dependencies> dependency path ../../../outside",
		"key /dependencies/outside/path @12:13-12:38 <crates/app/Cargo.toml#/dependencies/outside> string",
		"dependency libc @13:1-13:44 <crates/app/Cargo.toml#/dependencies> dependency crates.io",
		"key /dependencies/libc/version @13:10-13:25 <crates/app/Cargo.toml#/dependencies/libc> string",
		"key /dependencies/libc/optional @13:27-13:42 <crates/app/Cargo.toml#/dependencies/libc> boolean",
		"dependency log @15:1-17:22 <crates/app/Cargo.toml#/dependencies> dependency registry internal",
		"key /dependencies/log/version @16:1-16:16 <crates/app/Cargo.toml#/dependencies/log> string",
		"key /dependencies/log/registry @17:1-17:22 <crates/app/Cargo.toml#/dependencies/log> string",
		"table /dev-dependencies @19:1-20:15 <crates/app/Cargo.toml> [dev-dependencies]",
		"dependency tempfile @20:1-20:15 <crates/app/Cargo.toml#/dev-dependencies> dependency crates.io",
		"table /build-dependencies @22:1-23:9 <crates/app/Cargo.toml> [build-dependencies]",
		"dependency cc @23:1-23:9 <crates/app/Cargo.toml#/build-dependencies> dependency crates.io",
		"table /target @25:1-26:13 <crates/app/Cargo.toml> table (implicit)",
		"table /target/cfg(unix) @25:1-26:13 <crates/app/Cargo.toml#/target> table (implicit)",
		"table /target/cfg(unix)/dependencies @25:1-26:13 <crates/app/Cargo.toml#/target/cfg(unix)> [target.'cfg(unix)'.dependencies]",
		"dependency libc @26:1-26:13 <crates/app/Cargo.toml#/target/cfg(unix)/dependencies> dependency crates.io",
		"table /features @28:1-31:19 <crates/app/Cargo.toml> [features]",
		"key /features/default @29:1-29:18 <crates/app/Cargo.toml#/features> array",
		"key /features/std @30:1-30:46 <crates/app/Cargo.toml#/features> array",
		"key /features/fast @31:1-31:19 <crates/app/Cargo.toml#/features> array",
		"key /bin @33:1-38:22 <crates/app/Cargo.toml> array of tables",
		"table /bin/app-cli @33:1-35:21 <crates/app/Cargo.toml#/bin> [[bin]]",
		"key /bin/app-cli/name @34:1-34:17 <crates/app/Cargo.toml#/bin/app-cli> string",
		"key /bin/app-cli/path @35:1-35:21 <crates/app/Cargo.toml#/bin/app-cli> string",
		"table /bin/#1 @37:1-38:22 <crates/app/Cargo.toml#/bin> [[bin]]",
		"key /bin/#1/path @38:1-38:22 <crates/app/Cargo.toml#/bin/#1> string",
	})
	equalLines(t, "references", refLines(ex), []string{
		"depends_on crates/app/Cargo.toml#/dependencies/core -> core-lib [crates/core/Cargo.toml#/package] in-repo @7:1",
		"depends_on crates/app/Cargo.toml#/package -> core [crates/app/Cargo.toml#/dependencies/core] in-repo @7:1",
		"depends_on crates/app/Cargo.toml#/dependencies/serde -> serde [cargo:workspace-root/dependencies/serde] in-repo @8:1",
		"depends_on crates/app/Cargo.toml#/package -> serde [crates/app/Cargo.toml#/dependencies/serde] in-repo @8:1",
		"depends_on crates/app/Cargo.toml#/dependencies/regex -> regex [cargo:crates.io/regex] @9:1",
		"depends_on crates/app/Cargo.toml#/package -> regex [crates/app/Cargo.toml#/dependencies/regex] in-repo @9:1",
		"depends_on crates/app/Cargo.toml#/dependencies/json -> serde_json [cargo:crates.io/serde_json] @10:1",
		"depends_on crates/app/Cargo.toml#/package -> json [crates/app/Cargo.toml#/dependencies/json] in-repo @10:1",
		"depends_on crates/app/Cargo.toml#/dependencies/private -> private [cargo:git:https://example.com/org/private.git#private] @11:1",
		"depends_on crates/app/Cargo.toml#/package -> private [crates/app/Cargo.toml#/dependencies/private] in-repo @11:1",
		"depends_on crates/app/Cargo.toml#/dependencies/outside -> outside [cargo:path:outside] @12:1",
		"depends_on crates/app/Cargo.toml#/package -> outside [crates/app/Cargo.toml#/dependencies/outside] in-repo @12:1",
		"depends_on crates/app/Cargo.toml#/dependencies/libc -> libc [cargo:crates.io/libc] @13:1",
		"depends_on crates/app/Cargo.toml#/package -> libc [crates/app/Cargo.toml#/dependencies/libc] in-repo @13:1",
		"depends_on crates/app/Cargo.toml#/dependencies/log -> log [cargo:registry:internal/log] @15:15",
		"depends_on crates/app/Cargo.toml#/package -> log [crates/app/Cargo.toml#/dependencies/log] in-repo @15:15",
		"depends_on crates/app/Cargo.toml#/dev-dependencies/tempfile -> tempfile [cargo:crates.io/tempfile] @20:1",
		"depends_on crates/app/Cargo.toml#/package -> tempfile [crates/app/Cargo.toml#/dev-dependencies/tempfile] in-repo @20:1",
		"depends_on crates/app/Cargo.toml#/build-dependencies/cc -> cc [cargo:crates.io/cc] @23:1",
		"depends_on crates/app/Cargo.toml#/package -> cc [crates/app/Cargo.toml#/build-dependencies/cc] in-repo @23:1",
		"depends_on crates/app/Cargo.toml#/target/cfg(unix)/dependencies/libc -> libc [cargo:crates.io/libc] @26:1",
		"depends_on crates/app/Cargo.toml#/package -> libc [crates/app/Cargo.toml#/target/cfg(unix)/dependencies/libc] in-repo @26:1",
		"value_reference crates/app/Cargo.toml#/features/default -> std [crates/app/Cargo.toml#/features/std] in-repo @29:12",
		"value_reference crates/app/Cargo.toml#/features/std -> dep:libc [crates/app/Cargo.toml#/dependencies/libc] in-repo cap=candidate @30:8",
		"value_reference crates/app/Cargo.toml#/features/std -> regex/std [crates/app/Cargo.toml#/dependencies/regex] in-repo @30:20",
		"value_reference crates/app/Cargo.toml#/features/std -> serde?/std [crates/app/Cargo.toml#/dependencies/serde] in-repo @30:33",
		"value_reference crates/app/Cargo.toml#/features/fast -> missing [] @31:9",
	})
	equalLines(t, "diagnostics", diagLines(ex), nil)
}

// Scenario: a workspace's structure — the [workspace] table, its members
// (contains, in TestGraph_Fixture) and [workspace.dependencies].
func TestScenario_CargoWorkspace(t *testing.T) {
	ex := extract(t, "Cargo.toml", fixture(t, "Cargo.toml"))
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document Cargo.toml @1:1-10:1 <> ",
		"table /workspace @2:1-9:36 <Cargo.toml> [workspace]",
		"key /workspace/members @3:1-3:102 <Cargo.toml#/workspace> array",
		"key /workspace/exclude @4:1-4:25 <Cargo.toml#/workspace> array",
		"key /workspace/resolver @5:1-5:15 <Cargo.toml#/workspace> string",
		"table /workspace/dependencies @7:1-9:36 <Cargo.toml#/workspace> [workspace.dependencies]",
		"dependency serde @8:1-8:51 <Cargo.toml#/workspace/dependencies> dependency crates.io",
		"key /workspace/dependencies/serde/version @8:11-8:26 <Cargo.toml#/workspace/dependencies/serde> string",
		"key /workspace/dependencies/serde/features @8:28-8:49 <Cargo.toml#/workspace/dependencies/serde> array",
		"dependency core-lib @9:1-9:36 <Cargo.toml#/workspace/dependencies> dependency path crates/core",
		"key /workspace/dependencies/core-lib/path @9:14-9:34 <Cargo.toml#/workspace/dependencies/core-lib> string",
	})
	equalLines(t, "references", refLines(ex), []string{
		"depends_on Cargo.toml#/workspace/dependencies/serde -> serde [cargo:crates.io/serde] @8:1",
		"depends_on Cargo.toml#/workspace/dependencies/core-lib -> core-lib [crates/core/Cargo.toml#/package] in-repo @9:1",
	})
	equalLines(t, "diagnostics", diagLines(ex), nil)
}

// Scenarios: a pyproject.toml's project (name, version: the [project]
// table, kind package) and its build-system requirements — every PEP 508
// requirement a dependency named by its distribution name (two of one
// name: #2), outside the repository on PyPI; one that is none reported.
func TestScenario_PyprojectMetadataAndBuildSystem(t *testing.T) {
	ex := extract(t, "py/pyproject.toml", fixture(t, "py/pyproject.toml"))
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document pyproject.toml @1:1-27:1 <> ",
		"table /build-system @1:1-3:40 <py/pyproject.toml> [build-system]",
		"key /build-system/requires @2:1-2:39 <py/pyproject.toml#/build-system> array",
		"dependency setuptools @2:13-2:29 <py/pyproject.toml#/build-system/requires> dependency pypi setuptools",
		"dependency wheel @2:31-2:38 <py/pyproject.toml#/build-system/requires> dependency pypi wheel",
		"key /build-system/build-backend @3:1-3:40 <py/pyproject.toml#/build-system> string",
		"package sample-pkg @5:1-17:28 <py/pyproject.toml> project sample-pkg 1.2.0",
		"key /project/name @6:1-6:20 <py/pyproject.toml#/project> string",
		"key /project/version @7:1-7:18 <py/pyproject.toml#/project> string",
		"key /project/requires-python @8:1-8:26 <py/pyproject.toml#/project> string",
		"key /project/dependencies @9:1-14:2 <py/pyproject.toml#/project> array",
		"dependency requests @10:3-10:19 <py/pyproject.toml#/project/dependencies> dependency pypi requests",
		"dependency Regex @11:3-11:34 <py/pyproject.toml#/project/dependencies> dependency pypi regex",
		"dependency regex @12:3-12:16 <py/pyproject.toml#/project/dependencies> dependency pypi regex",
		"table /project/optional-dependencies @16:1-17:28 <py/pyproject.toml#/project> [project.optional-dependencies]",
		"key /project/optional-dependencies/dev @17:1-17:28 <py/pyproject.toml#/project/optional-dependencies> array",
		"dependency pytest @17:8-17:19 <py/pyproject.toml#/project/optional-dependencies/dev> dependency pypi pytest",
		"dependency ruff @17:21-17:27 <py/pyproject.toml#/project/optional-dependencies/dev> dependency pypi ruff",
		"table /dependency-groups @19:1-20:53 <py/pyproject.toml> [dependency-groups]",
		"key /dependency-groups/test @20:1-20:53 <py/pyproject.toml#/dependency-groups> array",
		"dependency coverage @20:9-20:25 <py/pyproject.toml#/dependency-groups/test> dependency pypi coverage",
		"table /tool @22:1-26:20 <py/pyproject.toml> table (implicit)",
		"table /tool/ruff @22:1-26:20 <py/pyproject.toml#/tool> [tool.ruff]",
		"key /tool/ruff/line-length @23:1-23:18 <py/pyproject.toml#/tool/ruff> integer",
		"table /tool/ruff/lint @25:1-26:20 <py/pyproject.toml#/tool/ruff> [tool.ruff.lint]",
		"key /tool/ruff/lint/select @26:1-26:20 <py/pyproject.toml#/tool/ruff/lint> array",
	})
	equalLines(t, "references", refLines(ex), []string{
		"depends_on py/pyproject.toml#/project/dependencies/requests -> requests [pypi:requests] @10:3",
		"depends_on py/pyproject.toml#/project -> requests [py/pyproject.toml#/project/dependencies/requests] in-repo @10:3",
		"depends_on py/pyproject.toml#/project/dependencies/regex -> Regex [pypi:regex] @11:3",
		"depends_on py/pyproject.toml#/project -> Regex [py/pyproject.toml#/project/dependencies/regex] in-repo @11:3",
		"depends_on py/pyproject.toml#/project/dependencies/regex#2 -> regex [pypi:regex] @12:3",
		"depends_on py/pyproject.toml#/project -> regex [py/pyproject.toml#/project/dependencies/regex#2] in-repo @12:3",
		"depends_on py/pyproject.toml#/project/optional-dependencies/dev/pytest -> pytest [pypi:pytest] @17:8",
		"depends_on py/pyproject.toml#/project -> pytest [py/pyproject.toml#/project/optional-dependencies/dev/pytest] in-repo @17:8",
		"depends_on py/pyproject.toml#/project/optional-dependencies/dev/ruff -> ruff [pypi:ruff] @17:21",
		"depends_on py/pyproject.toml#/project -> ruff [py/pyproject.toml#/project/optional-dependencies/dev/ruff] in-repo @17:21",
		"depends_on py/pyproject.toml#/dependency-groups/test/coverage -> coverage [pypi:coverage] @20:9",
		"depends_on py/pyproject.toml#/project -> coverage [py/pyproject.toml#/dependency-groups/test/coverage] in-repo @20:9",
		"depends_on py/pyproject.toml#/build-system/requires/setuptools -> setuptools [pypi:setuptools] @2:13",
		"depends_on py/pyproject.toml#/project -> setuptools [py/pyproject.toml#/build-system/requires/setuptools] in-repo @2:13",
		"depends_on py/pyproject.toml#/build-system/requires/wheel -> wheel [pypi:wheel] @2:31",
		"depends_on py/pyproject.toml#/project -> wheel [py/pyproject.toml#/build-system/requires/wheel] in-repo @2:31",
	})
	equalLines(t, "diagnostics", diagLines(ex), []string{
		"warning pyproject_invalid_requirement @13:3-13:12 a requirement that does not start with a distribution name (PEP 508) declares nothing",
	})
}

// A syntax error is located where the parser stopped; what precedes it is
// declared.
func TestDiagnostics_SyntaxError(t *testing.T) {
	ex := extract(t, "config/broken.toml", fixture(t, "config/broken.toml"))
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document broken.toml @1:1-4:1 <> ",
		"key /a @1:1-1:6 <config/broken.toml> integer",
	})
	equalLines(t, "diagnostics", diagLines(ex), []string{
		"error toml_syntax_error @2:8-3:1 invalid TOML: expected ']' to close table name: the rest of the file is not read",
	})
}

func TestDiagnostics_DuplicatesAndInvalid(t *testing.T) {
	ex := extract(t, "a.toml", "a = 1\r\nb = 2\r\na = 3\r\n[t]\r\n[t]\r\nx = 1\r\n")
	equalLines(t, "diagnostics", diagLines(ex), []string{
		"error toml_duplicate_key @3:1-3:2 the key \"a\" is already defined at line 1",
		"error toml_table_redefined @5:2-5:3 the table \"t\" is already defined at line 4",
	})
	// The second definition declares nothing; nor do the key/values under
	// a repeated header.
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document a.toml @1:1-7:1 <> ",
		"key /a @1:1-1:6 <a.toml> integer",
		"key /b @2:1-2:6 <a.toml> integer",
		"table /t @4:1-4:4 <a.toml> [t]",
	})
	ex = extract(t, "a.toml", "d = 2024-02-30\n")
	equalLines(t, "diagnostics", diagLines(ex), []string{
		"error toml_invalid @1:5-1:15 invalid TOML: invalid datetime: \"2024-02-30\"",
	})
}

// TestPositions_UnicodeAndCRLF: columns count bytes, lines end at '\n'
// (a CRLF's '\r' is the line's last byte).
func TestPositions_UnicodeAndCRLF(t *testing.T) {
	// "ключ" is 8 bytes, "значение" 16: the key/value is 31 bytes; '絵' = 1 is 9.
	ex := extract(t, "u.toml", "\"ключ\" = \"значение\"\r\n[\"表\".'é']\r\n'絵' = 1\r\n")
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document u.toml @1:1-4:1 <> ",
		"key /ключ @1:1-1:32 <u.toml> string",
		"table /表 @2:1-3:10 <u.toml> table (implicit)",
		"table /表/é @2:1-3:10 <u.toml#/表> [\"表\".'é']",
		"key /表/é/絵 @3:1-3:10 <u.toml#/表/é> integer",
	})
	equalLines(t, "diagnostics", diagLines(ex), nil)
	// A bare key is ASCII: the parser stops at the first byte of 絵.
	ex = extract(t, "u.toml", "a = 1\r\n絵 = 1\r\n")
	equalLines(t, "diagnostics", diagLines(ex), []string{
		"error toml_syntax_error @2:1-2:2 invalid TOML: invalid character at start of key: U+00E7 'ç': the rest of the file is not read",
	})
}

// TestNames_OnlyOneLineNames: a key that is no one-line name declares
// nothing, nor does what it holds; a key with "~" and "/" is escaped.
func TestNames_OnlyOneLineNames(t *testing.T) {
	long := strings.Repeat("k", 300)
	ex := extract(t, "n.toml", "\"a\\nb\" = 1\n\"\\u202e\" = {x = 1}\n\""+long+"\" = 1\n\"~/\" = 1\n\"\" = 2\n")
	equalLines(t, "symbols", symbolLines(ex), []string{
		"document n.toml @1:1-6:1 <> ",
		"key /~0~1 @4:1-4:9 <n.toml> integer",
		"key / @5:1-5:7 <n.toml> integer",
	})
}

// TestBounds_DepthAndCount: declarations stop at maxDepth parts; past
// maxSymbols, the shallower are kept and the rest reported once.
func TestBounds_DepthAndCount(t *testing.T) {
	ex := extract(t, "d.toml", "[a.b.c.d.e.f.g]\nh = 1\n")
	var deepest int
	for _, s := range ex.Symbols {
		if n := strings.Count(s.Name, "/"); n > deepest {
			deepest = n
		}
	}
	if deepest != maxDepth {
		t.Errorf("deepest declaration %d parts, want %d", deepest, maxDepth)
	}
	var b strings.Builder
	for i := 0; i < maxSymbols; i++ {
		fmt.Fprintf(&b, "[t%d]\nk = 1\n", i)
	}
	ex = extract(t, "c.toml", b.String())
	if len(ex.Symbols) != maxSymbols {
		t.Errorf("%d symbols, want %d", len(ex.Symbols), maxSymbols)
	}
	for _, s := range ex.Symbols {
		if strings.Count(s.Name, "/") > 1 {
			t.Fatalf("a key %s kept before every table", s.Name)
		}
	}
	if got := diagLines(ex); len(got) != 1 || !strings.Contains(got[0], CodeTruncated) {
		t.Errorf("diagnostics %v", got)
	}
	declared := map[string]bool{}
	for _, s := range ex.Symbols {
		declared[s.Qualified] = true
	}
	for _, r := range ex.References {
		if !declared[r.Container] || r.Kind == "contains" && !declared[r.NameQualified] {
			t.Fatalf("reference %s of %s to an undeclared declaration", r.Name, r.Container)
		}
	}
}

// TestIdentity_StableUnderInsertion: inserting a named element, or a key,
// renames no other declaration; an unnamed element is named by its place
// among the unnamed ones only.
func TestIdentity_StableUnderInsertion(t *testing.T) {
	before := extract(t, "Cargo.toml", "[[bin]]\nname = \"a\"\n[[bin]]\npath = \"x\"\n[[bin]]\nname = \"b\"\n")
	after := extract(t, "Cargo.toml", "x = 1\n[[bin]]\nname = \"new\"\n[[bin]]\nname = \"a\"\n[[bin]]\npath = \"x\"\n[[bin]]\nname = \"b\"\n")
	q := func(ex language.Extraction) map[string]bool {
		m := map[string]bool{}
		for _, s := range ex.Symbols {
			m[s.Qualified] = true
		}
		return m
	}
	b, a := q(before), q(after)
	for k := range b {
		if !a[k] {
			t.Errorf("%s renamed by the insertion", k)
		}
	}
	for _, want := range []string{"Cargo.toml#/bin/a", "Cargo.toml#/bin/#1", "Cargo.toml#/bin/b"} {
		if !b[want] {
			t.Errorf("no %s", want)
		}
	}
	dup := extract(t, "x.toml", "[[t]]\nname = \"a\"\n[[t]]\nname = \"a\"\n[[t]]\nname = \"a#2\"\n")
	equalLines(t, "elements", symbolLines(dup, "table"), []string{
		"table /t/a @1:1-2:11 <x.toml#/t> [[t]]",
		"table /t/a#2 @3:1-4:11 <x.toml#/t> [[t]]",
		"table /t/#1 @5:1-6:13 <x.toml#/t> [[t]]",
	})
}

// TestManifest_WindowsPaths: a path dependency's identity is spelled as
// the index spells file IDs on Windows.
func TestManifest_WindowsPaths(t *testing.T) {
	ex := extract(t, `crates\app\Cargo.toml`, "[package]\nname = \"app\"\n[dependencies]\ncore = { path = \"../core\" }\n")
	equalLines(t, "references", refLines(ex), []string{
		`depends_on crates\app\Cargo.toml#/dependencies/core -> core [crates\core\Cargo.toml#/package] in-repo @4:1`,
		`depends_on crates\app\Cargo.toml#/package -> core [crates\app\Cargo.toml#/dependencies/core] in-repo @4:1`,
	})
	if ex.Symbols[0].Name != "Cargo.toml" {
		t.Errorf("document named %q", ex.Symbols[0].Name)
	}
}

// TestManifest_OnlyByFileName: a file is a manifest by its name only; the
// same content elsewhere is plain TOML (no package, no dependency).
func TestManifest_OnlyByFileName(t *testing.T) {
	src := "[package]\nname = \"x\"\n[dependencies]\nserde = \"1\"\n[project]\nname = \"y\"\ndependencies = [\"z\"]\n"
	for _, f := range []string{"cargo.toml", "Cargo.toml.bak.toml", "x/pyproject.toml.toml", "config.toml"} {
		for _, s := range extract(t, f, src).Symbols {
			if s.Kind == "package" || s.Kind == "dependency" {
				t.Errorf("%s: %s %s", f, s.Kind, s.Name)
			}
		}
	}
	// A pyproject's [package] and a Cargo manifest's [project] are tables.
	equalLines(t, "pyproject", symbolLines(extract(t, "pyproject.toml", src), "package", "dependency"), []string{
		"package y @5:1-7:21 <pyproject.toml> project y",
		"dependency z @7:17-7:20 <pyproject.toml#/project/dependencies> dependency pypi z",
	})
	equalLines(t, "cargo", symbolLines(extract(t, "Cargo.toml", src), "package", "dependency"), []string{
		"package x @1:1-2:11 <Cargo.toml> package x",
		"dependency serde @4:1-4:12 <Cargo.toml#/dependencies> dependency crates.io",
	})
}

// TestManifest_WorkspaceSelf: in a manifest that is its own workspace
// root, workspace = true names its own [workspace.dependencies] entry.
func TestManifest_WorkspaceSelf(t *testing.T) {
	ex := extract(t, "Cargo.toml", "[workspace]\n[workspace.dependencies]\nlog = \"0.4\"\n[package]\nname = \"r\"\n[dependencies]\nlog.workspace = true\n")
	equalLines(t, "references", refLines(ex), []string{
		"depends_on Cargo.toml#/dependencies/log -> log [Cargo.toml#/workspace/dependencies/log] in-repo @7:1",
		"depends_on Cargo.toml#/package -> log [Cargo.toml#/dependencies/log] in-repo @7:1",
		"depends_on Cargo.toml#/workspace/dependencies/log -> log [cargo:crates.io/log] @3:1",
	})
}

func TestExtract_Deterministic(t *testing.T) {
	for _, f := range []string{"config/app.toml", "crates/app/Cargo.toml", "py/pyproject.toml"} {
		a, b := extract(t, f, fixture(t, f)), extract(t, f, fixture(t, f))
		if fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s: two extractions differ", f)
		}
	}
}

func TestExtract_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProvider().Extract(ctx, "a.toml", []byte("a = 1\n")); err == nil {
		t.Error("a canceled extraction succeeded")
	}
}
