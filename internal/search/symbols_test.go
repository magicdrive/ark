package search

import (
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

func sym(file, qualified string, kind symbol.SymbolKind, line uint32) symbol.Symbol {
	name := qualified
	if i := strings.LastIndexAny(qualified, `.\`); i >= 0 {
		name = qualified[i+1:]
	}
	return symbol.Symbol{
		ID:        symbol.NewSymbolID("go", file, kind, qualified),
		Name:      name,
		Qualified: qualified,
		Kind:      kind,
		Language:  "go",
		Location:  source.Location{File: source.FileID(file), Range: source.Range{Start: source.Position{Line: line, Column: 1}}},
	}
}

type got struct {
	Qualified string
	Type      MatchType
}

func ranked(q string, syms []symbol.Symbol) []got {
	var out []got
	for _, m := range MatchSymbols(q, syms) {
		out = append(out, got{m.Symbol.Qualified, m.Type})
	}
	return out
}

func TestMatchSymbols_AuthExample(t *testing.T) {
	syms := []symbol.Symbol{
		sym("sec/sec.go", "Security.AuthService", symbol.KindStruct, 3),
		sym("a/user.go", "UserAuthenticator", symbol.KindStruct, 1),
		sym("a/auth.go", "AuthenticateUser", symbol.KindFunction, 9),
		sym("a/auth.go", "AuthService", symbol.KindStruct, 2),
		sym("a/auth.go", "Auth", symbol.KindInterface, 1),
		sym("a/other.go", "Unrelated", symbol.KindFunction, 1),
		sym("a/oauth.go", "Oauthy", symbol.KindFunction, 1),
	}
	want := []got{
		{"Auth", MatchExactCaseInsensitive},
		{"AuthService", MatchPrefix},
		{"Security.AuthService", MatchPrefix},
		{"AuthenticateUser", MatchPrefix},
		{"UserAuthenticator", MatchWordBoundary},
		{"Oauthy", MatchSubstring},
	}
	if g := ranked("auth", syms); !reflect.DeepEqual(g, want) {
		t.Errorf("auth:\n got %v\nwant %v", g, want)
	}
}

func TestMatchSymbols_GetUserExample(t *testing.T) {
	syms := []symbol.Symbol{
		sym("svc.ts", "UserService.getUser", symbol.KindMethod, 5),
		sym("api.ts", "GetUserProfile", symbol.KindFunction, 1),
		sym("api.ts", "getUserById", symbol.KindFunction, 9),
		sym("api.ts", "getUser", symbol.KindFunction, 3),
		sym("api.ts", "get_user_name", symbol.KindFunction, 12),
	}
	want := []got{
		{"getUser", MatchExact},
		{"UserService.getUser", MatchExact},
		{"getUserById", MatchPrefix},
		{"GetUserProfile", MatchPrefix},
		{"get_user_name", MatchWordBoundary},
	}
	if g := ranked("getUser", syms); !reflect.DeepEqual(g, want) {
		t.Errorf("getUser:\n got %v\nwant %v", g, want)
	}
}

func TestMatchSymbols_MatchTypes(t *testing.T) {
	cases := []struct {
		query, qualified string
		want             MatchType // "" = no match
	}{
		{"Create", "Create", MatchExact},
		{"create", "Create", MatchExactCaseInsensitive},
		{"UserService.Create", "UserService.Create", MatchExact},
		{"userservice.create", "UserService.Create", MatchExactCaseInsensitive},
		{"Cre", "Create", MatchPrefix},
		{"cre", "Create", MatchPrefix},
		{"user_profile", "getUserProfile", MatchWordBoundary},
		{"userProf", "get_user_profile", MatchWordBoundary},
		{"server", "HTTPServer", MatchWordBoundary},
		{"http", "newHTTPClient", MatchWordBoundary},
		{"token", "OAuth2Token", MatchWordBoundary},
		{"Service.Cre", "UserService.Create", MatchSubstring}, // "Service" is not a whole segment
		{"UserService.Cre", "app.UserService.Create", MatchQualified},
		{"UserService::create", "UserService.Create", MatchQualified},
		{`Auth\LoginPolicy.show`, `App\Auth\LoginPolicy.showsButton`, MatchQualified},
		{"Service.run", "FooService.run", MatchSubstring},
		{"UserService", "UserService.Create", ""}, // members do not match their container's name
		{"serv", "observer", MatchSubstring},
		{"xyz", "Create", ""},
		// Unicode identifiers.
		{"ユーザー", "ユーザー取得", MatchPrefix},
		{"取得", "ユーザー取得", MatchSubstring},
		{"ÉCOLE", "école", MatchExactCaseInsensitive},
		{"straße", "StraßeFinden", MatchPrefix},
	}
	for _, c := range cases {
		m := MatchSymbols(c.query, []symbol.Symbol{sym("f.go", c.qualified, symbol.KindFunction, 1)})
		var g MatchType
		if len(m) == 1 {
			g = m[0].Type
		}
		if g != c.want {
			t.Errorf("query %q vs %q: got %q, want %q", c.query, c.qualified, g, c.want)
		}
	}
}

// Same-named symbols are separate candidates, never merged: different files,
// kinds and namespaces each appear, in a stable order.
func TestMatchSymbols_SameNameStaysDistinct(t *testing.T) {
	syms := []symbol.Symbol{
		sym("b/user.go", "User", symbol.KindStruct, 1),
		sym("a/user.go", "User", symbol.KindStruct, 1),
		sym("a/user.go", "User", symbol.KindInterface, 7),
		sym("c/x.php", `App\Models\User`, symbol.KindClass, 1),
		sym("c/y.php", `Legacy\User`, symbol.KindClass, 1),
	}
	m := MatchSymbols("User", syms)
	if len(m) != len(syms) {
		t.Fatalf("%d matches, want %d", len(m), len(syms))
	}
	var order []string
	for _, x := range m {
		order = append(order, string(x.Symbol.Location.File)+"/"+string(x.Symbol.Kind)+"/"+x.Symbol.Qualified)
	}
	want := []string{"a/user.go/interface/User", "a/user.go/struct/User", "b/user.go/struct/User", `c/x.php/class/App\Models\User`, `c/y.php/class/Legacy\User`}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order:\n got %v\nwant %v", order, want)
	}
}

func TestMatchSymbols_TestFilesRankAfterEqualMatches(t *testing.T) {
	syms := []symbol.Symbol{
		sym("a/auth_test.go", "AuthCase", symbol.KindFunction, 1),
		sym("z/auth.go", "AuthCore", symbol.KindFunction, 1),
	}
	m := MatchSymbols("auth", syms)
	if m[0].Symbol.Qualified != "AuthCore" {
		t.Errorf("test-file symbol outranks an equal non-test match: %v", ranked("auth", syms))
	}
}

// The ranking is a function of the symbol set: any input order gives the same
// result, and a duplicate symbol appears once.
func TestMatchSymbols_DeterministicUnderInputOrder(t *testing.T) {
	var syms []symbol.Symbol
	for _, f := range []string{"a.go", "b.go", "c/d.go"} {
		for i, q := range []string{"Auth", "auth", "AuthX", "XAuth", "x.Auth", "doAuth", "Author", "AUTH_KEY"} {
			syms = append(syms, sym(f, q, symbol.KindFunction, uint32(i+1)))
			syms = append(syms, sym(f, q, symbol.KindVariable, uint32(i+1)))
		}
	}
	syms = append(syms, syms[0]) // duplicate ID
	want := MatchSymbols("auth", syms)
	if len(want) != len(syms)-1 {
		t.Fatalf("%d matches, want %d", len(want), len(syms)-1)
	}
	r := rand.New(rand.NewSource(1))
	for range 50 {
		shuffled := slices.Clone(syms)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if g := MatchSymbols("auth", shuffled); !reflect.DeepEqual(g, want) {
			t.Fatal("ranking depends on input order")
		}
	}
}

func TestNormalizeQuery(t *testing.T) {
	for _, bad := range []string{"", "   ", "\t\n", strings.Repeat("a", MaxQueryBytes+1), "a\x00b", "a\nb", "\xff"} {
		if _, err := NormalizeQuery(bad); err == nil {
			t.Errorf("query %q accepted", bad)
		}
	}
	for in, want := range map[string]string{" auth ": "auth", "a.b": "a.b", strings.Repeat("a", MaxQueryBytes): strings.Repeat("a", MaxQueryBytes), "ユーザー": "ユーザー"} {
		if g, err := NormalizeQuery(in); err != nil || g != want {
			t.Errorf("NormalizeQuery(%q) = %q, %v", in, g, err)
		}
	}
}

func TestSplitWords(t *testing.T) {
	for in, want := range map[string][]string{
		"getUserById":   {"get", "user", "by", "id"},
		"HTTPServer":    {"http", "server"},
		"get_user_name": {"get", "user", "name"},
		"OAuth2Token":   {"o", "auth2", "token"},
		"v2Client":      {"v2", "client"},
		"UserService.x": {"user", "service", "x"},
		"ユーザー取得":        {"ユーザー取得"},
		"__init__":      {"init"},
		"":              nil,
	} {
		if g := SplitWords(in); !reflect.DeepEqual(g, want) {
			t.Errorf("SplitWords(%q) = %v, want %v", in, g, want)
		}
	}
}

func FuzzMatchSymbols(f *testing.F) {
	for _, s := range []string{"auth", "a.b", "::", "ユーザー", "HTTPServer", "_"} {
		f.Add(s, "AuthService.getHTTPServer")
	}
	f.Fuzz(func(t *testing.T, query, qualified string) {
		q, err := NormalizeQuery(query)
		if err != nil {
			return
		}
		s := sym("f.go", qualified, symbol.KindFunction, 1)
		s.Name = qualified
		m := MatchSymbols(q, []symbol.Symbol{s, s})
		if len(m) > 1 {
			t.Fatalf("duplicate symbol returned twice")
		}
		if len(m) == 1 && m[0].Type == MatchExact && s.Name != q && s.Qualified != q {
			t.Fatalf("exact match without equality: %q vs %q", q, qualified)
		}
	})
}

// Two declarations that share a SymbolID (same language, file, kind and
// qualified name — e.g. two Go init functions) are two candidates.
func TestMatchSymbols_SharedIDDeclarationsStaySeparate(t *testing.T) {
	a := sym("boot.go", "init", symbol.KindFunction, 3)
	b := sym("boot.go", "init", symbol.KindFunction, 5)
	if a.ID != b.ID {
		t.Fatal("fixture: IDs differ")
	}
	m := MatchSymbols("init", []symbol.Symbol{b, a, a})
	if len(m) != 2 || m[0].Symbol.Location.Range.Start.Line != 3 || m[1].Symbol.Location.Range.Start.Line != 5 {
		t.Errorf("got %v", m)
	}
}
