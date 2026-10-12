package secrets

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
)

// ruleOf names the keyword the key pattern itself chose: compared with the
// same pattern in which each keyword alternative is a named group, on keys
// built from keywords, their prefixes and each other.
func TestAssignment_RuleOfIsThePatternsChoice(t *testing.T) {
	kws := []keyword{}
	for i, k := range builtinKeywords {
		kws = append(kws, keyword{fragment: k.fragment, rule: i})
	}
	for i, k := range []string{"key", "dsn", "word", "en", "acme_secret"} {
		kws = append(kws, keyword{fragment: regexp.QuoteMeta(k), rule: 100 + i})
	}
	a := newAssignment(kws, true, true)
	alts := make([]string, len(kws))
	for i, k := range kws {
		alts[i] = fmt.Sprintf(`(?P<k%d>%s)`, i, k.fragment)
	}
	named := regexp.MustCompile(`(?i)(\b[A-Za-z0-9_\-\$]*(?:` + strings.Join(alts, "|") + `)\b)["']?\]?\s*(=>|:=|==|\|\||&&|=|:|\-)\s*(['"]?)`)
	parts := []string{"pass", "password", "passwd", "pw", "token", "secret", "api_key", "api-key", "apikey", "access_key",
		"private_key", "key", "dsn", "word", "en", "acme_secret", "my", "by", "x", "_", "-", "db", "PASS", "Token"}
	r := rand.New(rand.NewPCG(1, 2))
	checked := 0
	for range 20000 {
		var key strings.Builder
		for range 1 + r.IntN(4) {
			key.WriteString(parts[r.IntN(len(parts))])
		}
		line := "  " + key.String() + " = v"
		m := a.key.FindStringSubmatchIndex(line)
		n := named.FindStringSubmatchIndex(line)
		if (m == nil) != (n == nil) {
			t.Fatalf("%q: the patterns disagree on matching", line)
		}
		if m == nil {
			continue
		}
		want := -1
		for i := range kws {
			if g := named.SubexpIndex(fmt.Sprintf("k%d", i)); n[2*g] >= 0 {
				want = kws[i].rule
			}
		}
		if got := a.ruleOf(line, m); got != want {
			t.Errorf("%q: ruleOf = %d, the pattern chose %d", line, got, want)
		}
		checked++
	}
	if checked < 10000 {
		t.Errorf("only %d keys matched", checked)
	}
}
