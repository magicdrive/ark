package secrets

import "regexp"

// The built-in rules: Ark's recommended defaults (Recommended). Each has a
// stable ID — the name a configuration disables it by and a scan reports it
// under — documented in docs/secret-rules.md and pinned by
// TestBuiltinRules_StableIDs. A rule's ID never changes meaning; a rule
// retired is removed, never reused.

// RuleKind is how a rule finds what it masks.
type RuleKind string

const (
	// KindPattern masks every match of a regular expression (a known token
	// format), or one capture group of it.
	KindPattern RuleKind = "pattern"
	// KindBlock masks a whole multi-line block (a PEM private key).
	KindBlock RuleKind = "block"
	// KindAssignment masks the value of an assignment whose key ends in one
	// of the rule's keywords (password = …), the value's extent decided by
	// its syntax: quotes, a line's end, a heredoc or YAML block scalar body.
	KindAssignment RuleKind = "assignment"
	// KindReference keeps a value that only names another value (a GitHub
	// Actions or Terraform reference) unmasked. Disabling one masks more.
	KindReference RuleKind = "reference"
)

// RuleSource is where a rule comes from.
type RuleSource string

const (
	SourceBuiltin RuleSource = "builtin"
	SourceConfig  RuleSource = "config"
)

// Built-in rule IDs that are not patterns.
const (
	RulePEMBlock               = "pem-private-key-block"
	RuleKeepActionsReference   = "keep-actions-reference"
	RuleKeepTerraformReference = "keep-terraform-reference"
	// RuleGenericAssignment names every generic-assignment-* rule at once: a
	// configuration may disable them together.
	RuleGenericAssignment = "generic-assignment"
)

// builtinPattern is one regular expression of a built-in pattern rule. A rule
// may have several; they run in this order, which is v6.1.0's.
type builtinPattern struct {
	rule string
	re   *regexp.Regexp
}

var builtinPatterns = []builtinPattern{
	// AWS
	{"aws-access-key-id", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`ASIA[0-9A-Z]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`A3T[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`AGPA[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`AIDA[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`AROA[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`AIPA[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`ANPA[A-Z0-9]{16}`)},
	{"aws-access-key-id", regexp.MustCompile(`ANVA[A-Z0-9]{16}`)},
	// GCP
	{"gcp-service-account-key", regexp.MustCompile(`"type":\s*"service_account".*"private_key_id":\s*"[a-f0-9]+".*"private_key":\s*"-----BEGIN PRIVATE KEY-----[^"]+"`)},
	{"google-api-key", regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`)},
	// GCP service account keys
	{"gcp-private-key-id", regexp.MustCompile(`private_key_id\s*=\s*"[a-f0-9]{32}"`)},
	{"gcp-private-key-id", regexp.MustCompile(`"private_key_id"\s*:\s*"[a-f0-9]{32}"`)},
	{"gcp-private-key", regexp.MustCompile(`"private_key"\s*:\s*"(-----BEGIN PRIVATE KEY-----[^"]+)"`)},
	// Azure
	{"azure-account-key", regexp.MustCompile(`AccountKey=([A-Za-z0-9+/=]{88})`)},
	{"azure-hex-key", regexp.MustCompile(`[0-9a-f]{32}-[0-9a-f]{32}-[0-9a-f]{32}`)},
	// GitHub/GitLab
	{"github-token", regexp.MustCompile(`ghp_[A-Za-z0-9_]{36,255}`)},
	{"github-token", regexp.MustCompile(`ghu_[A-Za-z0-9_]{36,255}`)},
	{"github-token", regexp.MustCompile(`ghs_[A-Za-z0-9_]{36,255}`)},
	{"gitlab-token", regexp.MustCompile(`glpat-[0-9a-zA-Z\-\_]{20}`)},
	// Slack
	{"slack-token", regexp.MustCompile(`xox[baprs]-([0-9a-zA-Z]{10,48})?`)},
	// Stripe
	{"stripe-live-key", regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24}`)},
	{"stripe-live-key", regexp.MustCompile(`pk_live_[0-9a-zA-Z]{24}`)},
	// SendGrid
	{"sendgrid-api-key", regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`)},
	// Google OAuth/Client
	{"google-oauth-token", regexp.MustCompile(`ya29\.[0-9A-Za-z\-_]+`)},
	{"google-oauth-client-id", regexp.MustCompile(`[0-9]+-([0-9A-Za-z_]{32})\.apps\.googleusercontent\.com`)},
	// Private Keys
	{"private-key-header", regexp.MustCompile(`-----BEGIN (RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`)},
	// JWT
	{"jwt-secret-assignment", regexp.MustCompile(`JWT_SECRET(.{0,10})?([=:])(.{0,100})`)},
	{"jwt", regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`)},
}

// builtinKeyword is one keyword of the built-in assignment rules: the
// regular-expression fragment a key must end in, and the rule it reports.
type builtinKeyword struct {
	rule     string
	fragment string
}

// builtinKeywords are the generic-assignment keywords, in v6.1.0's order.
var builtinKeywords = []builtinKeyword{
	{"generic-assignment-secret", `secret`},
	{"generic-assignment-password", `password`},
	{"generic-assignment-passwd", `passwd`},
	{"generic-assignment-pass", `pass`},
	{"generic-assignment-pw", `pw`},
	{"generic-assignment-token", `token`},
	{"generic-assignment-api-key", `api[_\-]?key`},
	{"generic-assignment-access-key", `access[_\-]?key`},
	{"generic-assignment-private-key", `private[_\-]?key`},
}

// builtinRules describes every built-in rule, in the order a listing shows
// them: patterns (in pattern order), the block, the assignment keywords and
// the reference exceptions.
var builtinRules = func() []Rule {
	desc := map[string]string{
		"aws-access-key-id":       "AWS access key ID (AKIA…, ASIA…) and other AWS unique IDs",
		"gcp-service-account-key": "Google Cloud service account key (JSON with a private key)",
		"google-api-key":          "Google API key (AIza…)",
		"gcp-private-key-id":      "Google Cloud service account private_key_id",
		"gcp-private-key":         "Google Cloud service account private_key (JSON)",
		"azure-account-key":       "Azure storage AccountKey= in a connection string",
		"azure-hex-key":           "Three 32-digit hex groups joined by hyphens (Azure keys)",
		"github-token":            "GitHub token (ghp_, ghu_, ghs_)",
		"gitlab-token":            "GitLab personal access token (glpat-)",
		"slack-token":             "Slack token (xoxb-, xoxa-, xoxp-, xoxr-, xoxs-)",
		"stripe-live-key":         "Stripe live secret or publishable key (sk_live_, pk_live_)",
		"sendgrid-api-key":        "SendGrid API key (SG.…)",
		"google-oauth-token":      "Google OAuth access token (ya29.…)",
		"google-oauth-client-id":  "Google OAuth client ID (….apps.googleusercontent.com)",
		"private-key-header":      "PEM private key header line (-----BEGIN … PRIVATE KEY-----)",
		"jwt-secret-assignment":   "JWT_SECRET assignment and up to 100 characters after it",
		"jwt":                     "JSON Web Token (eyJ….….…)",
	}
	var rules []Rule
	seen := map[string]bool{}
	for _, p := range builtinPatterns {
		if !seen[p.rule] {
			seen[p.rule] = true
			rules = append(rules, Rule{ID: p.rule, Description: desc[p.rule], Kind: KindPattern, Source: SourceBuiltin})
		}
	}
	rules = append(rules, Rule{ID: RulePEMBlock, Description: "Whole PEM private key block, from its BEGIN line to its END line", Kind: KindBlock, Source: SourceBuiltin})
	for _, k := range builtinKeywords {
		rules = append(rules, Rule{ID: k.rule, Description: "Value of an assignment whose key ends in " + keywordLabel(k.fragment), Kind: KindAssignment, Source: SourceBuiltin})
	}
	rules = append(rules,
		Rule{ID: RuleKeepActionsReference, Description: "Keep a value that is one GitHub Actions context reference (${{ secrets.NAME }})", Kind: KindReference, Source: SourceBuiltin},
		Rule{ID: RuleKeepTerraformReference, Description: "Keep a value that is one Terraform reference (var.x, TYPE.NAME.attr) in a .tf file", Kind: KindReference, Source: SourceBuiltin},
	)
	return rules
}()

// keywordLabel is a keyword fragment as a reader reads it: "api[_\-]?key"
// is "api_key, api-key or apikey".
func keywordLabel(fragment string) string {
	if base, ok := cutOptionalSeparator(fragment); ok {
		return `"` + base[0] + `_` + base[1] + `", "` + base[0] + `-` + base[1] + `" or "` + base[0] + base[1] + `"`
	}
	return `"` + fragment + `"`
}

func cutOptionalSeparator(fragment string) ([2]string, bool) {
	const sep = `[_\-]?`
	for i := 0; i+len(sep) <= len(fragment); i++ {
		if fragment[i:i+len(sep)] == sep {
			return [2]string{fragment[:i], fragment[i+len(sep):]}, true
		}
	}
	return [2]string{}, false
}
