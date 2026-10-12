# Secret masking rules

Ark masks the secrets it detects in what it returns: MCP responses, the
repository dump and its formats. The rules it uses are a recommended
default, not an enforced policy: a repository can disable the ones that mask
what is no secret to it and add its own. `ark secrets-scan` shows what the
rules mask, where and why, without showing the values.

Masking is a best-effort safety net for Ark's own output. It is not
data-loss prevention: it finds what its rules know, and it does not keep any
other tool — git, an editor, an agent's shell — from reading a file. To keep
a file out of Ark's output entirely, exclude it with `.arkignore`
([SECURITY.md](../SECURITY.md#file-access-policy-arkignore)).

## The configuration file

Put the configuration at the repository root, as `.ark-secrets-rules.toml`
or `ark-secrets-rules.toml` — one of them: both is an error. It is read from
the root only (a file in a subdirectory is not), as a regular file: a
symlink, even to a file inside the repository, is an error. Without one, Ark
uses its recommended rules, which mask exactly what Ark 6.1.0 masked.

```toml
version = 1                # required
preset = "recommended"     # optional; "recommended" is the only preset

# Disable a built-in rule (by its ID, below). `reason` is required.
[[override]]
id = "generic-assignment-pass"
enabled = false
reason = "bypass = true and similar flags in our Go code are no secrets"

# Add a pattern rule: every match of `pattern` is masked …
[[rule]]
id = "acme-internal-token"
description = "ACME internal API token"
pattern = 'acme_tok_[A-Za-z0-9]{32}'

# … or only one capture group of every match.
[[rule]]
id = "acme-session"
description = "ACME session ID in a cookie"
pattern = 'session=([0-9a-f]{32})'
mask = "group"
capture_group = 1

# Add assignment keys: the value of an assignment whose key ends in one of
# them is masked as the built-in generic-assignment rules mask theirs.
[[assignment]]
id = "acme-assignment"
description = "ACME license keys and DSNs"
keys = ["license_key", "dsn"]
```

| Field | Meaning |
|---|---|
| `version` | The schema version: `1`. Required; another version is an error. |
| `preset` | The rule set the file adjusts: `"recommended"` (the default and the only one). |
| `[[override]]` `id` | A built-in rule's ID, or `generic-assignment` for every `generic-assignment-*` rule. |
| `[[override]]` `enabled` | `false`: the rule is disabled. Required. |
| `[[override]]` `reason` | Why. Required; shown by `ark secrets-scan --list-rules`. |
| `[[rule]]` `id` | The rule's ID: lower-case letters and digits in hyphen-separated words, at most 64 bytes, no built-in ID. |
| `[[rule]]` `pattern` | A [Go regular expression](https://pkg.go.dev/regexp/syntax) (RE2: linear time, no backtracking). It must not match the empty string. At most 4096 bytes. |
| `[[rule]]` `mask` | `"match"` (the default): mask every match. `"group"`: mask capture group `capture_group` of every match. |
| `[[rule]]` `capture_group` | With `mask = "group"`: the group, from 1. |
| `[[assignment]]` `id` | As a rule's. |
| `[[assignment]]` `keys` | 1 to 100 keys: letters, digits and underscores, in hyphen-separated words. A key matches when an assignment's key ends in it, in any case (`dsn` matches `DATABASE_DSN`). |
| `description` | Optional, at most 200 bytes; shown in listings. |

Everything else is an error: an unknown field (a misspelled one would
silently change nothing), a duplicate ID, a pattern that does not compile, a
capture group the pattern lacks, more than 1000 rules. Errors name the file,
the line when the parser knows it, and the field — never a value:

```text
.ark-secrets-rules.toml: [[rule]] #2 (id "acme-session"): mask = "group" needs capture_group = 1 or more
.ark-secrets-rules.toml: rule "acme": pattern does not compile: missing closing )
.ark-secrets-rules.toml: unknown field rule.flags (fields: …)
```

A configuration that cannot be read or compiled is never replaced by the
recommended rules: rules you added would be dropped silently. The dump and
`ark secrets-scan` fail, and the MCP server refuses every result with
repository text in it until the file is fixed (see below).

## How rules apply

The rules apply in a fixed order, the same everywhere:

1. **Whole PEM private-key blocks**, across lines (`pem-private-key-block`).
2. Line by line: the **pattern rules** — the built-in ones in the order of the
   table below, then the configuration's — each to the line as written, then
   the **assignment rules** (built-in keywords and the configuration's keys
   together), which mask a value's whole extent: every word of it, a quoted
   value to the quote that ends it, a heredoc or YAML block scalar body on
   every line.
3. **Reference exceptions** keep a value that only names another value — one
   GitHub Actions context reference (`${{ secrets.NAME }}`), and in a `.tf`
   file one Terraform reference (`var.x`, `TYPE.NAME.attr`) — unmasked.

Spans that overlap are masked once (`*****MASKED*****`). A span is still
reported under every rule that found it.

Disabling a rule removes only its own masks: what the other rules mask stays
masked. Two rules depend on others: a heredoc or block scalar body is masked
by the assignment rule of the key that opens it, so disabling that rule
leaves its body unmasked too; and disabling `pem-private-key-block` leaves
`private-key-header` masking only the BEGIN line. Disabling a reference
exception masks more, never less.

A file's syntax (Terraform for a `.tf` file) is decided from the regular file
read, never from a name asked for: a `.tf` symlink to another file is read as
text of no known syntax.

## Built-in rules

The IDs are stable: a configuration names a rule by its ID and a scan reports
it, so an ID never changes meaning, and a rule retired is removed, never
reused. `ark secrets-scan --list-rules` lists them with their state.

| ID | Kind | What it masks |
|---|---|---|
| `aws-access-key-id` | pattern | AWS access key IDs (`AKIA…`, `ASIA…`) and other AWS unique IDs |
| `gcp-service-account-key` | pattern | A Google Cloud service account key (JSON with a private key) |
| `google-api-key` | pattern | Google API keys (`AIza…`) |
| `gcp-private-key-id` | pattern | A service account's `private_key_id` |
| `gcp-private-key` | pattern | A service account's `private_key` (JSON) |
| `azure-account-key` | pattern | `AccountKey=` in an Azure storage connection string |
| `azure-hex-key` | pattern | Three 32-digit hex groups joined by hyphens |
| `github-token` | pattern | GitHub tokens (`ghp_`, `ghu_`, `ghs_`) |
| `gitlab-token` | pattern | GitLab personal access tokens (`glpat-`) |
| `slack-token` | pattern | Slack tokens (`xoxb-`, `xoxa-`, `xoxp-`, `xoxr-`, `xoxs-`) |
| `stripe-live-key` | pattern | Stripe live keys (`sk_live_`, `pk_live_`) |
| `sendgrid-api-key` | pattern | SendGrid API keys (`SG.…`) |
| `google-oauth-token` | pattern | Google OAuth access tokens (`ya29.…`) |
| `google-oauth-client-id` | pattern | Google OAuth client IDs (`….apps.googleusercontent.com`) |
| `private-key-header` | pattern | A PEM private key's BEGIN line |
| `jwt-secret-assignment` | pattern | `JWT_SECRET` and up to 100 characters after it |
| `jwt` | pattern | JSON Web Tokens (`eyJ….….…`) |
| `pem-private-key-block` | block | A whole PEM private key block |
| `generic-assignment-secret` | assignment | The value of a key ending in `secret` |
| `generic-assignment-password` | assignment | … in `password` |
| `generic-assignment-passwd` | assignment | … in `passwd` |
| `generic-assignment-pass` | assignment | … in `pass` (`bypass` too) |
| `generic-assignment-pw` | assignment | … in `pw` |
| `generic-assignment-token` | assignment | … in `token` |
| `generic-assignment-api-key` | assignment | … in `api_key`, `api-key` or `apikey` |
| `generic-assignment-access-key` | assignment | … in `access_key`, `access-key` or `accesskey` |
| `generic-assignment-private-key` | assignment | … in `private_key`, `private-key` or `privatekey` |
| `keep-actions-reference` | reference | Keeps one GitHub Actions context reference unmasked |
| `keep-terraform-reference` | reference | Keeps one Terraform reference unmasked in a `.tf` file |

An assignment key may be bare, quoted or an index (`password:`,
`"password":`, `'password' =>`, `$cfg['password'] =`); the operator one of
`=`, `:`, `:=`, `=>`, `==`, `||`, `&&`, `-`. A key matches the keyword it
ends in, case-insensitively: `generic-assignment-pass` masks `bypass = true`
and `PASS=…`, which is why it is the first rule to consider disabling in
source code.

## The dump, the MCP server and the scan

They read the same file the same way and give it the same meaning, with one
difference, about weakening:

- **The dump** (`ark <dir>`) and **`ark secrets-scan`** apply the whole
  configuration. They are run by a person, who could read the files anyway.
- **The MCP server** applies the entries that **disable** built-in rules only
  when its operator starts it with `--secret-rule-overrides on`. A file in the
  repository — which an agent may write — must not weaken the masking the
  operator runs. Without the flag the server applies the rest of the
  configuration (added rules, and the disabling of reference exceptions,
  which masks more), leaves the disabling out, and logs once on standard
  error which entries it left out:

  ```text
  ark: .ark-secrets-rules.toml disables built-in masking rules (generic-assignment-pass); not applied: the server was not started with --secret-rule-overrides on
  ```

The MCP server reads the configuration for every request that masks, so an
edit applies to the next request; it compiles it only when its bytes change,
and a request masks everything with the one configuration it read. A
configuration that cannot be read or compiled fails closed: every result with
repository text in it — file contents, snippets, search lines, resources — is
replaced by an error naming the file and the problem; results built only from
identifiers (`list_files`, `get_repository_map`, `search_code` in text) still
go out, and the message of a request that fails is withheld (it could hold a
value only the configuration's rules mask). The problem is logged once on
standard error. A failing request's message is otherwise masked by the
configuration that request read.

`--mask-secrets off` turns masking off altogether, configuration included;
the `maskSecrets` argument of a tool call still changes nothing.

The configuration changes only what is masked in output. It never changes
which files can be read (`.arkignore`, root and symlink checks), nor the
index, symbols, the repository map or search results.

## Scanning

```bash
ark secrets-scan                     # the current directory
ark secrets-scan path/to/repo --format json
ark secrets-scan --list-rules        # every rule: ID, source, kind, state, reason
ark secrets-scan --suggest-ignore    # suggestions; changes nothing
ark secrets-scan --include-ignored   # also the files .arkignore and .gitignore exclude
```

See [docs/cli.md](cli.md#secrets-scan) for the output and exit statuses. A
scan reports what masking masks, with the same rules and the same matching,
never the values. A finding-free scan is no proof that a repository holds no
secret: it finds only what the rules know.
