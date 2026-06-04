# KiteString

[![CI](https://github.com/RowanDark/kitestring/actions/workflows/ci.yml/badge.svg)](https://github.com/RowanDark/kitestring/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/RowanDark/kitestring?sort=semver)](https://github.com/RowanDark/kitestring/releases/latest)
[![License](https://img.shields.io/github/license/RowanDark/kitestring)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/RowanDark/kitestring)](go.mod)

**Context-aware API endpoint discovery and path fuzzing for modern web applications.**

KiteString sends routes with the correct HTTP method, headers, parameters, and body content derived from a structured wordlist schema — not blind path guessing. Feed it an OpenAPI spec, a SecLists alias, or a curated `.ks` wordlist and it surgically probes your target the way an API consumer would, surfacing endpoints that traditional directory fuzzers miss entirely.

---

## Installation

### Pre-built binary (recommended)

Download the latest release for your platform from the [releases page](https://github.com/RowanDark/kitestring/releases/latest).

```sh
# Linux / macOS (example)
curl -fsSL https://github.com/RowanDark/kitestring/releases/latest/download/kitestring_linux_amd64.tar.gz \
  | tar -xz -C /usr/local/bin ks

# Verify
ks version
```

Checksums are published in `checksums.txt` alongside each release archive.

### Build from source

```sh
git clone https://github.com/RowanDark/kitestring.git
cd kitestring
make build
# binary written to bin/ks
```

Requires Go 1.25+.

### Wordlist setup

KiteString's curated wordlists are distributed as release assets. Pull them with:

```sh
ks wordlist update
```

This downloads all manifest entries into `~/.cache/kitestring/wordlists/`. Pass an alias to fetch a specific list:

```sh
ks wordlist update apiroutes
```

---

## Quick start

```sh
# 1. Context-aware scan against a live API using the apiroutes CDN wordlist
ks scan https://api.example.com/v1 -A apiroutes

# 2. Traditional path fuzzing with extensions
ks brute https://example.com -w ~/wordlists/common.txt -e php,json

# 3. Replay captured requests against a different host
ks replay results.jsonl --target https://staging.example.com

# 4. Update all CDN wordlists
ks wordlist update

# 5. Run a scan using a saved profile
ks scan https://api.example.com -p myprofile
```

---

## Flag reference

### Global flags (all commands)

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `pretty` | Output format: `pretty`, `text`, `jsonl` |
| `--quiet` | `-q` | `false` | Suppress decorative output |
| `--verbose` | `-v` | `info` | Verbosity level: `error`, `info`, `debug`, `trace` |
| `--config` | | | Config file path (default: `~/.kitestring.yaml`) |

### `ks scan`

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--wordlist` | `-w` | | Wordlist file(s) (`.ks`, `.txt`, `.json`); repeatable |
| `--wordlist-alias` | `-A` | | CDN wordlist alias, e.g. `apiroutes` or `apiroutes:20000`; repeatable |
| `--seclists` | `-S` | | SecLists alias to fetch on demand |
| `--openapi-url` | `-O` | | Fetch OpenAPI/Swagger spec from URL at scan time |
| `--openapi-file` | | | Load local OpenAPI/Swagger spec at scan time |
| `--head` | | `0` | Use only the first N routes from each wordlist (0 = all) |
| `--threads` | `-t` | `10` | Concurrent connections per host |
| `--parallel-hosts` | `-j` | `10` | Maximum hosts to scan concurrently |
| `--timeout` | | `10` | Request timeout in seconds |
| `--delay` | | `0` | Fixed inter-request delay per host (e.g. `200ms`, `1s`) |
| `--max-retries` | | `3` | Maximum retries on 429 or connection failure |
| `--backoff-base` | | `5s` | Base duration for exponential backoff on 429 |
| `--backoff-max` | | `60s` | Maximum backoff ceiling |
| `--proxy` | `-P` | | HTTP proxy URL |
| `--header` | `-H` | | Extra request header `Key: Value`; repeatable |
| `--user-agent` | | `KiteString/1.0` | Custom User-Agent |
| `--follow-redirects` | | `true` | Follow HTTP redirects |
| `--max-redirects` | | `3` | Maximum redirects to follow |
| `--blacklist-domain` | | | Do not follow redirects to these domains; repeatable |
| `--force-method` | | | Override HTTP method for all routes |
| `--fail-status-codes` | | | Status codes to suppress, comma-separated |
| `--success-status-codes` | | | Only report these status codes |
| `--ignore-length` | | | Suppress responses at this content length or range; repeatable |
| `--disable-precheck` | | `false` | Skip preflight host check and wildcard baseline |
| `--preflight-depth` | | `1` | Path depth for wildcard baseline probing |
| `--wildcard-detection` | | `true` | Detect and quarantine wildcard routing hosts |
| `--quarantine-threshold` | | `10` | Consecutive wildcard responses before quarantine |
| `--kitebuilder-full-scan` | | `false` | Send all routes regardless of wildcard baseline |
| `--disable-similarity` | | `false` | Skip body similarity scoring |
| `--js-extract` | `-J` | `false` | Parse `<script>` tags and add extracted routes |
| `--js-depth` | | `1` | Pages deep to crawl for script tags |
| `--scope-file` | `-s` | | Path to scope file |
| `--scope` | | | Inline include pattern; repeatable |
| `--exclude` | | | Inline exclude pattern; repeatable |
| `--skip-out-of-scope` | | `false` | Silently skip out-of-scope targets |
| `--warn-out-of-scope` | | `false` | Log warnings for out-of-scope targets |
| `--depth` | `-d` | `2` | Crawl depth for context discovery |
| `--filter-api` | | | Only scan routes matching this KSUID |
| `--report` | `-R` | | Auto-generate report on completion: `md`, `html` |
| `--checkpoint` | `-c` | | Checkpoint file path (creates or resumes a scan) |
| `--resume` | `-r` | | Alias for `--checkpoint` with explicit resume intent |
| `--checkpoint-interval` | | `500` | Save checkpoint every N completed requests |
| `--profile` | `-p` | | Load settings from a named profile |

### `ks brute`

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--wordlist` | `-w` | | Wordlist file(s); repeatable |
| `--wordlist-alias` | `-A` | | CDN wordlist alias |
| `--seclists` | `-S` | | SecLists alias to fetch on demand |
| `--head` | | `0` | Use only the first N paths from each wordlist |
| `--extensions` | `-e` | | Extensions to append to each path, comma-separated |
| `--dirsearch-compat` | `-D` | `false` | Substitute `%EXT%` placeholder instead of appending |
| `--threads` | `-t` | `40` | Concurrent connections per host |
| `--parallel-hosts` | `-j` | `10` | Maximum hosts to scan concurrently |
| `--timeout` | | `10` | Request timeout in seconds |
| `--delay` | | `0` | Fixed inter-request delay per host |
| `--max-retries` | | `3` | Maximum retries on 429 or connection failure |
| `--backoff-base` | | `5s` | Base for exponential backoff on 429 |
| `--backoff-max` | | `60s` | Maximum backoff ceiling |
| `--proxy` | `-P` | | HTTP proxy URL |
| `--header` | `-H` | | Extra request header; repeatable |
| `--user-agent` | | `KiteString/1.0` | Custom User-Agent |
| `--force-method` | | `GET` | Override HTTP method |
| `--follow-redirects` | `-r` | `false` | Follow HTTP redirects |
| `--max-redirects` | | `3` | Maximum redirects to follow |
| `--blacklist-domain` | | | Do not follow redirects to these domains; repeatable |
| `--fail-status-codes` | | | Status codes to suppress |
| `--success-status-codes` | | | Only report these status codes |
| `--ignore-length` | | | Suppress responses at this content length; repeatable |
| `--disable-precheck` | | `false` | Skip preflight host check |
| `--preflight-depth` | `-d` | `0` | Directory depth for wildcard baseline probing |
| `--wildcard-detection` | | `true` | Detect and quarantine wildcard routing hosts |
| `--quarantine-threshold` | | `10` | Consecutive wildcard responses before quarantine |
| `--filter-api` | | | Only report routes matching this KSUID |
| `--report` | `-R` | | Auto-generate report on completion: `md`, `html` |
| `--profile` | `-p` | | Load settings from a named profile |

---

## Pipeline integration

KiteString speaks JSONL on stdout, which composes naturally with other security tools.

### subfinder → httpx → ks

Enumerate subdomains, probe for live hosts, then scan all of them:

```sh
subfinder -d example.com -silent \
  | httpx -silent \
  | ks scan - -A apiroutes -o jsonl \
  | tee results.jsonl
```

### ks → jq

Extract only 200 OK endpoints from a scan:

```sh
ks scan https://api.example.com -A apiroutes -o jsonl \
  | jq 'select(.status_code == 200) | {method: .method, path: .path, status: .status_code}'
```

### ks → nuclei

Pipe discovered endpoints into Nuclei for vulnerability scanning:

```sh
ks scan https://api.example.com -A apiroutes -o jsonl \
  | jq -r '.url' \
  | nuclei -list - -t cves/ -o nuclei-results.txt
```

### Bulk targets from a file

```sh
# targets.txt: one URL per line
ks scan - -A apiroutes < targets.txt
```

---

## Wordlists

### CDN wordlists

Maintained wordlists are hosted as GitHub Release assets and pulled via `ks wordlist update`:

| Alias | Routes | Description |
|-------|--------|-------------|
| `apiroutes` | 215,000 | Common API routes from OpenAPI/Swagger corpus |
| `graphql` | 50,000 | GraphQL introspection and query paths |
| `admin` | 25,000 | Administrative and API management routes |
| `cloud` | 35,000 | Cloud provider metadata and management endpoints |
| `springboot` | 18,000 | Spring Boot Actuator and Java framework routes |

```sh
ks wordlist list            # show available + cached status
ks wordlist update          # pull all
ks wordlist update apiroutes graphql  # pull specific aliases
ks wordlist update --force  # re-download even if cached
```

### SecLists integration

Any SecLists wordlist can be fetched on demand and compiled to `.ks` format:

```sh
ks wordlist seclists list                        # show known aliases
ks wordlist seclists fetch api-endpoints         # fetch one alias
ks wordlist seclists fetch --all                 # fetch all known aliases
ks scan https://api.example.com -S api-endpoints # fetch + scan inline
```

### OpenAPI ingestion

Compile any OpenAPI/Swagger spec into a `.ks` wordlist:

```sh
# From a URL
ks wordlist openapi fetch --url https://petstore.swagger.io/v2/swagger.json

# From a local file
ks wordlist openapi fetch --file ./my-api.yaml --output my-api.ks

# Search APIs.guru catalogue
ks wordlist openapi search stripe

# Use spec at scan time (no pre-compilation needed)
ks scan https://api.stripe.com --openapi-url https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
```

### Custom wordlists

Compile plain text or JSON wordlists to the optimised `.ks` format:

```sh
ks wordlist compile my-routes.txt            # outputs my-routes.ks
ks wordlist compile my-routes.json -o out.ks
```

---

## Profiles and config

KiteString reads `~/.kitestring.yaml` on startup. Profiles let you save named sets of flags for different engagements:

```yaml
# ~/.kitestring.yaml
profiles:
  api-recon:
    threads: 20
    timeout: 15
    wordlists:
      - apiroutes
    headers:
      - "Authorization: Bearer ${API_TOKEN}"
    scope_file: ~/engagements/example/scope.txt
    output: jsonl

  aggressive:
    threads: 100
    parallel_hosts: 25
    delay: 0
    max_retries: 1
```

Use a profile:

```sh
ks scan https://api.example.com -p api-recon
ks brute https://example.com -p aggressive -e php,aspx
```

---

## Contributing

### Reporting issues

Use the issue tracker. Please include:

- `ks version --verbose` output
- The exact command you ran
- Expected vs. actual behaviour
- A minimal reproduction if possible

### Pull request process

1. Fork the repository and create a feature branch.
2. Run `make test` and `make lint` — both must pass.
3. Follow [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `docs:`, etc.).
4. Open a PR against `main` with a clear description of what changed and why.

### Wordlist contributions

To contribute a new wordlist or update an existing one:

1. Open an issue describing the source, route count, and use case.
2. Attach a sample or link to the source data.
3. The maintainers will review and integrate it into the next CDN wordlist update cycle.

---

## Credits

KiteString is a spiritual successor to [Kiterunner](https://github.com/assetnote/kiterunner) by Assetnote, which pioneered context-aware API route discovery by using route schemas rather than plain path lists. KiteString extends that foundation with OpenAPI ingestion, a structured binary wordlist format, profiles, scope filtering, and a modular Go architecture.
