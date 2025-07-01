package secrets

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var defaultPatterns = []*regexp.Regexp{
	// AWS
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
	regexp.MustCompile(`A3T[A-Z0-9]{16}`),
	regexp.MustCompile(`AGPA[A-Z0-9]{16}`),
	regexp.MustCompile(`AIDA[A-Z0-9]{16}`),
	regexp.MustCompile(`AROA[A-Z0-9]{16}`),
	regexp.MustCompile(`AIPA[A-Z0-9]{16}`),
	regexp.MustCompile(`ANPA[A-Z0-9]{16}`),
	regexp.MustCompile(`ANVA[A-Z0-9]{16}`),
	// GCP
	regexp.MustCompile(`"type":\s*"service_account".*"private_key_id":\s*"[a-f0-9]+".*"private_key":\s*"-----BEGIN PRIVATE KEY-----[^"]+"`),
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),
	// GCP service account keys
	regexp.MustCompile(`private_key_id\s*=\s*"[a-f0-9]{32}"`),
	regexp.MustCompile(`"private_key_id"\s*:\s*"[a-f0-9]{32}"`),
	regexp.MustCompile(`"private_key"\s*:\s*"(-----BEGIN PRIVATE KEY-----[^"]+)"`),
	// Azure
	regexp.MustCompile(`AccountKey=([A-Za-z0-9+/=]{88})`),
	regexp.MustCompile(`[0-9a-f]{32}-[0-9a-f]{32}-[0-9a-f]{32}`),
	// GitHub/GitLab
	regexp.MustCompile(`ghp_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`ghu_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`ghs_[A-Za-z0-9_]{36,255}`),
	regexp.MustCompile(`glpat-[0-9a-zA-Z\-\_]{20}`),
	// Slack
	regexp.MustCompile(`xox[baprs]-([0-9a-zA-Z]{10,48})?`),
	// Stripe
	regexp.MustCompile(`sk_live_[0-9a-zA-Z]{24}`),
	regexp.MustCompile(`pk_live_[0-9a-zA-Z]{24}`),
	// SendGrid
	regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`),
	// Google OAuth/Client
	regexp.MustCompile(`ya29\.[0-9A-Za-z\-_]+`),
	regexp.MustCompile(`[0-9]+-([0-9A-Za-z_]{32})\.apps\.googleusercontent\.com`),
	// Private Keys
	regexp.MustCompile(`-----BEGIN (RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`),
	// JWT
	regexp.MustCompile(`JWT_SECRET(.{0,10})?([=:])(.{0,100})`),
	regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`),
}

// ===== Allow-list patterns (to avoid false positives for known test/demo values) =====
var defaultAllowed = []*regexp.Regexp{
	regexp.MustCompile(`AKIAIOSFODNN7EXAMPLE`),
	regexp.MustCompile(`wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`),
}

type SecretMatch struct {
	File    string
	Line    int
	Text    string
	Pattern string
}

type ValueMaskRule struct {
	KeyPattern *regexp.Regexp
}

func defaultValueMaskRule() *ValueMaskRule {
	pat := `(?i)(\b[A-Za-z0-9_\-\$]*(?:secret|password|passwd|pass|pw|token|api[_\-]?key|access[_\-]?key|private[_\-]?key)\b)\s*(:=|==|\|\||&&|=|:|\-)\s*(['"]?)`
	return &ValueMaskRule{
		KeyPattern: regexp.MustCompile(pat),
	}
}

func (r *ValueMaskRule) MaskLine(line string) string {
	result := ""
	rest := line
	for {
		m := r.KeyPattern.FindStringSubmatchIndex(rest)
		if m == nil {
			result += rest
			break
		}
		// m[0] = full match start, m[1] = full match end,
		// m[2]=key start, m[3]=key end, m[4]=delimiter start,
		// m[5]=delimiter end, m[6]=quote start, m[7]=quote end

		// 1. llways output the part after the previous match up to the key (commas, spaces, etc. can be entered here)
		result += rest[:m[2]]

		// 2. print key, separator, leading space, and opening quote
		result += rest[m[2]:m[6]]
		quote := ""
		if m[6] != -1 && m[7] != -1 {
			quote = rest[m[6]:m[7]]
			result += quote
		}

		maskedValue := "*****MASKED*****"
		valStart := m[7] // starting index of the value body (immediately after the quote)
		val := rest[valStart:]

		if quote == `"` || quote == `'` {
			//With quotes: up to closing quote
			runes := []rune(val)
			escaped := false
			closingIdx := -1
			for i := range len(runes) {
				c := runes[i]
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if string(c) == quote {
					closingIdx = i
					break
				}
			}
			result += maskedValue
			if closingIdx != -1 {
				result += quote
				valStart += len(string(runes[:closingIdx+1]))
			} else {
				valStart += len(val)
			}
		} else {
			// no quoted
			end := len(val)
			for i, c := range val {
				if c == ',' || c == ';' || c == ' ' {
					end = i
					break
				}
			}
			result += maskedValue
			valStart += end
		}

		// remainder is rest.
		rest = rest[valStart:]
	}
	return result
}

type RuleSet struct {
	FullMaskPatterns []*regexp.Regexp // Default+Added patterns (order matters!)
	Allowed          []*regexp.Regexp
	ValueMaskRules   []*ValueMaskRule
}

func DefaultRuleSet() *RuleSet {
	return &RuleSet{
		FullMaskPatterns: slices.Clone(defaultPatterns),
		Allowed:          slices.Clone(defaultAllowed),
		ValueMaskRules:   []*ValueMaskRule{defaultValueMaskRule()},
	}
}

func (r *RuleSet) AddPattern(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.FullMaskPatterns = append(r.FullMaskPatterns, re)
	return nil
}

func (r *RuleSet) AddAllowed(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	r.Allowed = append(r.Allowed, re)
	return nil
}

func (r *RuleSet) AddValueMaskKey(keywordList []string) {
	var quoted []string
	for _, k := range keywordList {
		if !regexp.MustCompile(`^[A-Za-z0-9_\-\$]+$`).MatchString(k) {
			continue
		}
		quoted = append(quoted, regexp.QuoteMeta(k))
	}
	keyword := strings.Join(quoted, "|")
	// e.g.  (?i)(secret|password|api_key)\s*([=:\-])\s*(['"]?)([^\s'"]+)(['"]?)
	pat := fmt.Sprintf(`(?i)(%s)\s*([=:\-])\s*(['"]?)([^'"]+)(['"]?)`, keyword)
	r.ValueMaskRules = append(r.ValueMaskRules, &ValueMaskRule{
		KeyPattern: regexp.MustCompile(pat),
	})
}

func (r *RuleSet) isAllowed(s string) bool {
	for _, a := range r.Allowed {
		if a.MatchString(s) {
			return true
		}
	}
	return false
}

func (r *RuleSet) ScanReader(rd io.Reader, filename string) ([]SecretMatch, error) {
	var matches []SecretMatch
	scanner := bufio.NewScanner(rd)
	lineNum := 0

LINE:
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if r.isAllowed(line) {
			continue
		}
		for _, pat := range r.FullMaskPatterns {
			if pat.MatchString(line) {
				matches = append(matches, SecretMatch{
					File:    filename,
					Line:    lineNum,
					Text:    line,
					Pattern: pat.String(),
				})
				continue LINE
			}
		}
		for _, vmr := range r.ValueMaskRules {
			if vmr.KeyPattern.MatchString(line) {
				matches = append(matches, SecretMatch{
					File:    filename,
					Line:    lineNum,
					Text:    line,
					Pattern: "value-masking",
				})
				break
			}
		}
	}
	return matches, scanner.Err()
}

func (r *RuleSet) ScanFile(path string) ([]SecretMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return r.ScanReader(f, path)
}

func (r *RuleSet) ScanDir(root string, recursive bool) ([]SecretMatch, error) {
	var results []SecretMatch
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !IsTextFile(path) {
			return nil
		}
		m, err := r.ScanFile(path)
		if err == nil && len(m) > 0 {
			results = append(results, m...)
		}
		return nil
	})
	return results, err
}

func IsTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 800)
	n, _ := f.Read(buf)
	for i := range n {
		b := buf[i]
		if b < 0x09 || (b > 0x0D && b < 0x20) {
			return false
		}
	}
	return true
}

func MaskSecretKeyBlocks(content string) string {
	re := regexp.MustCompile(`(?ms)-----BEGIN (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----.*?-----END (?:RSA|DSA|EC|OPENSSH|PRIVATE|ENCRYPTED) PRIVATE KEY-----`)
	return re.ReplaceAllString(content, "*****MASKED*****")
}

func MaskAll(content string) string {
	content = MaskSecretKeyBlocks(content)
	rules := DefaultRuleSet()
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, vmr := range rules.ValueMaskRules {
			line = vmr.MaskLine(line)
		}
		for _, pat := range rules.FullMaskPatterns {
			//if pat.MatchString(line) {
			//	log.Printf("Matched pattern: %s | line: %s\n", pat.String(), line)
			//}
			line = pat.ReplaceAllString(line, "*****MASKED*****")
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
