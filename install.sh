#!/bin/sh
# Installs, upgrades or downgrades a command-line tool from its GitHub Releases.
#
#   sh install.sh                              # the latest stable release
#   sh install.sh --version v1.2.3             # a given release (older ones too)
#   sh install.sh --upgrade                    # same as the first: say it is an upgrade
#   sh install.sh --dir "$HOME/.local/bin"     # where to put it (this is the default)
#   sh install.sh --help
#
# The archive is checked against the release's SHA-256 checksum file, only the
# binary is taken out of it, and it replaces the installed one with a single
# rename in the same directory: a failed run leaves the installed binary as it
# was. It never uses sudo and never edits PATH or shell start-up files.
#
# Reuse this installer:
#   1. Change APP_NAME
#   2. Change GITHUB_URL
#   3. Ensure the release assets follow the supported distribution contract
#      (see docs/installer.md in https://github.com/magicdrive/enma): GoReleaser
#      archives <APP_NAME>_<version>_<os>_<arch>.tar.gz with the binary
#      <APP_NAME> at their root, <APP_NAME>_checksums.txt, and a binary whose
#      `--version` prints "<APP_NAME> version v<version>".
# Nothing below the project configuration needs to change.

# Project configuration
APP_NAME="ark"
GITHUB_URL="https://github.com/magicdrive/ark"

# ---------------------------------------------------------------------------

set -u
LC_ALL=C
export LC_ALL
unset CDPATH

PROG=install.sh
NL='
'
TMP_DIR=
STAGED=

say() { printf '%s\n' "$*"; }
warn() { printf '%s: warning: %s\n' "$PROG" "$*" >&2; }
die() {
	printf '%s: error: %s\n' "$PROG" "$*" >&2
	exit 1
}
usage_error() {
	printf '%s: %s\nRun "sh %s --help" for usage.\n' "$PROG" "$*" "$PROG" >&2
	exit 2
}

# printable TEXT: TEXT without control characters (it may come from a server)
printable() {
	printf '%s' "$1" | tr -d '\000-\037\177'
}

# lastline FILE: the last non-empty line of FILE, without control characters
lastline() {
	tr -d '\000-\011\013-\037\177' <"$1" | awk 'NF { l = $0 } END { print l }'
}

# errtext FILE: the non-empty lines of FILE (a tool's error output) on one line
errtext() {
	tr -d '\000-\011\013-\037\177' <"$1" | awk 'NF { printf "%s%s", s, $0; s = "; " } END { print "" }'
}

cleanup() {
	if [ -n "$STAGED" ]; then rm -f -- "$STAGED"; fi
	if [ -n "$TMP_DIR" ]; then rm -rf -- "$TMP_DIR"; fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

usage() {
	cat <<EOF
Install $APP_NAME from $GITHUB_URL/releases.

Usage: sh install.sh [--upgrade] [--version VERSION] [--dir DIR]

  --version VERSION  install this release (e.g. v1.2.3; older releases too,
                     which downgrades). Default: the latest stable release.
  --upgrade          replace an installed $APP_NAME (the same as running
                     without it; it only says what you mean).
  --dir DIR          install into DIR (created if missing).
                     Default: \$HOME/.local/bin
  -h, --help         show this help.

Running it again with the installed version changes nothing. The download is
verified with the release's SHA-256 checksums. sudo is never used, and PATH
is not changed: add DIR to PATH yourself.

Environment: INSTALLER_TIMEOUT  seconds to wait for a connection or for more
data before giving up (1-3600, default 60). The proxy variables of curl or
wget (https_proxy, ...) apply as usual.
EOF
}

# --- validation helpers -----------------------------------------------------

# valid_app_name NAME: a file name of letters, digits, '.', '_' and '-'
valid_app_name() {
	case $1 in
	'' | . | .. | -* | .*) return 1 ;;
	*[!A-Za-z0-9._-]*) return 1 ;;
	esac
	[ "${#1}" -le 64 ]
}

# valid_owner NAME: a GitHub user or organization name
valid_owner() {
	case $1 in
	'' | -* | *-) return 1 ;;
	*[!A-Za-z0-9-]*) return 1 ;;
	esac
	[ "${#1}" -le 39 ]
}

# valid_repo NAME: a GitHub repository name
valid_repo() {
	case $1 in
	'' | . | .. | *.git) return 1 ;;
	*[!A-Za-z0-9._-]*) return 1 ;;
	esac
	[ "${#1}" -le 100 ]
}

# valid_number N: a decimal number without leading zeros (at most 9 digits)
valid_number() {
	case $1 in
	'' | *[!0-9]*) return 1 ;;
	0) return 0 ;;
	0*) return 1 ;;
	esac
	[ "${#1}" -le 9 ]
}

# valid_tag TAG: v<major>.<minor>.<patch> with an optional -<prerelease>
# (semantic versioning, without build metadata)
valid_tag() {
	_t=$1
	[ "${#_t}" -le 64 ] || return 1
	case $_t in v[0-9]*) ;; *) return 1 ;; esac
	_t=${_t#v}
	case $_t in
	*-*) _core=${_t%%-*} _pre=${_t#*-} ;;
	*) _core=$_t _pre=none ;;
	esac
	case $_core in
	*.*.*.* | *..* | .* | *.) return 1 ;;
	*.*.*) ;;
	*) return 1 ;;
	esac
	valid_number "${_core%%.*}" || return 1
	_rest=${_core#*.}
	valid_number "${_rest%%.*}" || return 1
	valid_number "${_rest#*.}" || return 1
	case $_pre in
	none) return 0 ;;
	'' | .* | *. | *..* | *[!0-9A-Za-z.-]*) return 1 ;;
	esac
	return 0
}

# compare_tags A B: prints "lt", "eq", "gt" or "ne" (prereleases of the same
# version that differ are not ordered here)
compare_tags() {
	_a=${1#v} _b=${2#v}
	_ac=${_a%%-*} _bc=${_b%%-*}
	_ap=${_a#"$_ac"} _bp=${_b#"$_bc"}
	set -- "${_ac%%.*}" "${_bc%%.*}"
	_ar=${_ac#*.} _br=${_bc#*.}
	set -- "$1" "$2" "${_ar%%.*}" "${_br%%.*}" "${_ar#*.}" "${_br#*.}"
	while [ $# -gt 0 ]; do
		if [ "$1" -lt "$2" ]; then echo lt && return; fi
		if [ "$1" -gt "$2" ]; then echo gt && return; fi
		shift 2
	done
	if [ "$_ap" = "$_bp" ]; then echo eq
	elif [ -z "$_ap" ]; then echo gt
	elif [ -z "$_bp" ]; then echo lt
	else echo ne
	fi
}

# --- 1. project configuration ----------------------------------------------

check_config() {
	valid_app_name "$APP_NAME" ||
		die "APP_NAME '$APP_NAME' is not a valid name (letters, digits, '.', '_', '-')"
	_rest=${GITHUB_URL#https://github.com/}
	[ "$_rest" != "$GITHUB_URL" ] ||
		die "GITHUB_URL '$GITHUB_URL' must start with https://github.com/"
	case $_rest in
	*/*/* | */ | /*) die "GITHUB_URL '$GITHUB_URL' must be https://github.com/OWNER/REPOSITORY" ;;
	*/*) ;;
	*) die "GITHUB_URL '$GITHUB_URL' must be https://github.com/OWNER/REPOSITORY" ;;
	esac
	GH_OWNER=${_rest%%/*}
	GH_REPO=${_rest#*/}
	valid_owner "$GH_OWNER" || die "GITHUB_URL '$GITHUB_URL': '$GH_OWNER' is not a valid owner"
	valid_repo "$GH_REPO" || die "GITHUB_URL '$GITHUB_URL': '$GH_REPO' is not a valid repository"
	REPO_URL=https://github.com/$GH_OWNER/$GH_REPO

	TIMEOUT=${INSTALLER_TIMEOUT:-60}
	if ! { valid_number "$TIMEOUT" && [ "$TIMEOUT" -ge 1 ] && [ "$TIMEOUT" -le 3600 ]; }; then
		die "INSTALLER_TIMEOUT '$TIMEOUT' must be a number of seconds from 1 to 3600"
	fi
}

# --- 2. arguments ------------------------------------------------------------

REQ_TAG=
INSTALL_DIR=
HAVE_DIR=no
UPGRADE=no

set_version() {
	[ -z "$REQ_TAG" ] || usage_error "--version is given more than once"
	case $1 in
	'' | -*) usage_error "--version needs a value (e.g. --version v1.2.3)" ;;
	[0-9]*) _v=v$1 ;;
	*) _v=$1 ;;
	esac
	valid_tag "$_v" || usage_error "'$1' is not a version like v1.2.3 or v1.2.3-rc.1"
	REQ_TAG=$_v
}

set_dir() {
	[ "$HAVE_DIR" = no ] || usage_error "--dir is given more than once"
	case $1 in
	'' | -*) usage_error "--dir needs a directory" ;;
	*"$NL"*) usage_error "--dir must not contain a newline" ;;
	esac
	INSTALL_DIR=$1
	HAVE_DIR=yes
}

parse_args() {
	while [ $# -gt 0 ]; do
		case $1 in
		-h | --help)
			usage
			exit 0
			;;
		--upgrade)
			[ "$UPGRADE" = no ] || usage_error "--upgrade is given more than once"
			UPGRADE=yes
			;;
		--version)
			[ $# -ge 2 ] || usage_error "--version needs a value (e.g. --version v1.2.3)"
			set_version "$2"
			shift
			;;
		--version=*) set_version "${1#--version=}" ;;
		--dir)
			[ $# -ge 2 ] || usage_error "--dir needs a directory"
			set_dir "$2"
			shift
			;;
		--dir=*) set_dir "${1#--dir=}" ;;
		-*) usage_error "unknown option '$1'" ;;
		*) usage_error "unexpected argument '$1'" ;;
		esac
		shift
	done
	if [ "$HAVE_DIR" = no ]; then
		[ -n "${HOME:-}" ] || die "HOME is not set; use --dir DIR"
		INSTALL_DIR=$HOME/.local/bin
	fi
}

# --- tools ---------------------------------------------------------------------

check_tools() {
	for _c in uname mktemp gzip tar awk tr ls find cat chmod mkdir mv rm; do
		command -v "$_c" >/dev/null 2>&1 || die "'$_c' is required but was not found"
	done
	if command -v curl >/dev/null 2>&1; then
		DOWNLOADER=curl
	elif command -v wget >/dev/null 2>&1; then
		_wv=$(wget --version 2>/dev/null)
		case $_wv in
		"GNU Wget 1."[0-9]*) ;;
		*) die "the wget found is not GNU Wget 1.x (BusyBox wget does not verify TLS certificates; wget2 is not supported); install curl" ;;
		esac
		# --https-only needs GNU Wget 1.17 or later
		_wv=${_wv#GNU Wget 1.}
		_wv=${_wv%%[!0-9]*}
		[ "$_wv" -ge 17 ] || die "GNU Wget 1.17 or later is needed (or curl)"
		DOWNLOADER=wget
	else
		die "curl (or GNU Wget) is required"
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		SHA_TOOL=sha256sum
	elif command -v shasum >/dev/null 2>&1; then
		SHA_TOOL=shasum
	elif command -v openssl >/dev/null 2>&1; then
		SHA_TOOL=openssl
	else
		die "a SHA-256 tool is required (sha256sum, shasum or openssl)"
	fi
}

# sha256_of FILE: prints the SHA-256 of FILE in lower-case hex
sha256_of() {
	case $SHA_TOOL in
	sha256sum) _h=$(sha256sum <"$1") || return 1 ;;
	shasum) _h=$(shasum -a 256 <"$1") || return 1 ;;
	openssl) _h=$(openssl dgst -sha256 <"$1") || return 1 ;;
	esac
	# "<hex>  -" (sha256sum, shasum) or "SHA2-256(stdin)= <hex>" (openssl)
	case $SHA_TOOL in
	openssl) _h=${_h##* } ;;
	*) _h=${_h%% *} ;;
	esac
	case $_h in *[!0-9a-f]*) return 1 ;; esac
	[ "${#_h}" -eq 64 ] || return 1
	printf '%s\n' "$_h"
}

# --- 4. platform ---------------------------------------------------------------

detect_platform() {
	_os=$(uname -s) || die "uname -s failed"
	_arch=$(uname -m) || die "uname -m failed"
	case $_os in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	MINGW* | MSYS* | CYGWIN* | Windows*)
		die "Windows is not supported by this installer (use WSL, or the .zip of the release)" ;;
	*) die "unsupported operating system '$_os' (supported: macOS and Linux)" ;;
	esac
	case $_arch in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "unsupported CPU '$_arch' (supported: amd64 (x86_64) and arm64 (aarch64))" ;;
	esac
	# A shell running under Rosetta 2 reports x86_64 on an Apple silicon Mac:
	# install the native arm64 binary.
	_sysctl=sysctl
	if ! command -v sysctl >/dev/null 2>&1 && [ -x /usr/sbin/sysctl ]; then _sysctl=/usr/sbin/sysctl; fi
	if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] &&
		[ "$("$_sysctl" -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
		ARCH=arm64
		say "Running under Rosetta 2: installing the native arm64 build."
	fi
}

# --- install directory -------------------------------------------------------

# check_dir: INSTALL_DIR exists; sets PHYS_DIR (symbolic links resolved) and TARGET
check_dir() {
	[ -d "$INSTALL_DIR" ] || die "'$INSTALL_DIR' is not a directory"
	PHYS_DIR=$(cd -P -- "$INSTALL_DIR" && pwd -P) || die "cannot enter '$INSTALL_DIR'"
	case $PHYS_DIR in
	/*) ;;
	*) die "cannot resolve '$INSTALL_DIR'" ;;
	esac
	case $PHYS_DIR in *"$NL"*) die "'$INSTALL_DIR' resolves to a path with a newline" ;; esac
	_ww=$(find "$PHYS_DIR" -prune -perm -0002) || die "cannot check the permissions of '$PHYS_DIR'"
	[ -z "$_ww" ] || die "'$PHYS_DIR' is writable by every user; choose another --dir"
	# others who can write the parent could rename this directory away
	_ww=$(find "$PHYS_DIR/.." -prune -perm -0002 ! -perm -1000) ||
		die "cannot check the permissions of the directory above '$PHYS_DIR'"
	[ -z "$_ww" ] || die "the directory above '$PHYS_DIR' is writable by every user; choose another --dir"
	check_not_homebrew
	[ -w "$PHYS_DIR" ] ||
		die "'$PHYS_DIR' is not writable (this installer does not use sudo); choose another --dir"
	if [ "$PHYS_DIR" = / ]; then TARGET=/$APP_NAME; else TARGET=$PHYS_DIR/$APP_NAME; fi
}

# check_ancestor: INSTALL_DIR does not exist yet: the nearest directory above
# it that does must not be in Homebrew, nor writable by others without the
# sticky bit (they could swap the directories created below it)
check_ancestor() {
	case /$INSTALL_DIR/ in
	*/../*) die "a --dir that does not exist yet must not contain '..': '$INSTALL_DIR'" ;;
	*/Cellar/*) die "'$INSTALL_DIR' is inside a Homebrew Cellar; use 'brew upgrade' for a Homebrew $APP_NAME, or choose another --dir" ;;
	esac
	_a=$INSTALL_DIR
	case $_a in /*) ;; *) _a=./$_a ;; esac
	while [ ! -d "$_a" ]; do
		case $_a in */*) _a=${_a%/*} ;; *) break ;; esac
		[ -n "$_a" ] || _a=/
	done
	PHYS_DIR=$(cd -P -- "$_a" && pwd -P) || die "cannot enter '$_a'"
	_ww=$(find "$PHYS_DIR" -prune -perm -0002 ! -perm -1000) || die "cannot check the permissions of '$PHYS_DIR'"
	[ -z "$_ww" ] || die "'$PHYS_DIR' is writable by every user; choose another --dir"
	check_not_homebrew
}

# check_not_homebrew: refuse a directory inside a Homebrew prefix, where the
# binary belongs to brew (upgrade it with `brew upgrade` instead)
BREW_PREFIXES=
check_not_homebrew() {
	if [ -z "$BREW_PREFIXES" ]; then
		BREW_PREFIXES=/opt/homebrew$NL/home/linuxbrew/.linuxbrew
		# Homebrew on an Intel Mac lives in /usr/local
		if [ -d /usr/local/Homebrew ] || [ -d /usr/local/Cellar ]; then
			BREW_PREFIXES=$BREW_PREFIXES$NL/usr/local
		fi
		if [ -n "${HOMEBREW_PREFIX:-}" ]; then BREW_PREFIXES=$BREW_PREFIXES$NL$HOMEBREW_PREFIX; fi
		if command -v brew >/dev/null 2>&1; then
			_bp=$(brew --prefix 2>/dev/null) && BREW_PREFIXES=$BREW_PREFIXES$NL$_bp
		fi
	fi
	_old_ifs=$IFS
	IFS=$NL
	set -f
	for _p in $BREW_PREFIXES; do
		IFS=$_old_ifs
		case $_p in /*) ;; *) continue ;; esac
		[ -d "$_p" ] || continue
		_pp=$(cd -P -- "$_p" && pwd -P) || continue
		[ "$_pp" != / ] || continue
		case $PHYS_DIR/ in
		"$_pp"/*)
			die "'$PHYS_DIR' is inside the Homebrew prefix '$_pp'; use 'brew upgrade' for a Homebrew $APP_NAME, or choose another --dir" ;;
		esac
	done
	set +f
	IFS=$_old_ifs
	case $PHYS_DIR/ in
	*/Cellar/*) die "'$PHYS_DIR' is inside a Homebrew Cellar; use 'brew upgrade' for a Homebrew $APP_NAME, or choose another --dir" ;;
	esac
}

# installed_tag FILE: prints the version FILE reports, or nothing
installed_tag() {
	# only a regular executable file of this user's is run
	[ -f "$1" ] && [ -x "$1" ] || return 0
	_uid=$(id -u 2>/dev/null) || return 0
	[ -n "$(find "$1" -prune -user "$_uid" 2>/dev/null)" ] || return 0
	_out=$("$1" --version </dev/null 2>/dev/null) || return 0
	_line=${_out%%"$NL"*}
	_v=${_line#"$APP_NAME version "}
	[ "$_v" != "$_line" ] || return 0
	case $_v in [0-9]*) _v=v$_v ;; esac
	valid_tag "$_v" && printf '%s\n' "$_v"
	return 0
}

# check_target: the file to replace is absent or a regular file (never a link)
check_target() {
	if [ -L "$TARGET" ]; then
		die "'$TARGET' is a symbolic link (for example one made by Homebrew); it is not replaced. Remove it, or choose another --dir"
	fi
	if [ -e "$TARGET" ] && [ ! -f "$TARGET" ]; then
		die "'$TARGET' exists and is not a regular file"
	fi
}

# --- downloads -----------------------------------------------------------------

# fetch URL FILE: 0 when saved, 3 when the server answered 404, 1 otherwise
# (with the reason in FETCH_ERROR). Redirects are followed only to https URLs.
fetch() {
	rm -f -- "$2"
	if [ "$DOWNLOADER" = curl ]; then
		_code=$(curl -q -sS -L --proto =https --proto-redir =https --max-redirs 5 \
			--connect-timeout "$TIMEOUT" --speed-limit 1 --speed-time "$TIMEOUT" \
			-o "$2" -w '%{http_code}' "$1" 2>"$TMP_DIR/fetch.err")
		_rc=$?
	else
		# wget follows a redirect from https to http even with --https-only, so
		# each redirect is followed here, after checking it.
		_u=$1
		_n=0
		while :; do
			wget -S --https-only --check-certificate --tries=1 --timeout="$TIMEOUT" \
				--max-redirect=0 -O "$2" -o "$TMP_DIR/fetch.err" "$_u"
			_rc=$?
			_code=$(awk '$1 ~ /^HTTP\// { c = $2 } END { print c }' "$TMP_DIR/fetch.err")
			case $_code in 301 | 302 | 303 | 307 | 308) ;; *) break ;; esac
			_u=$(awk 'tolower($1) == "location:" { sub(/\r$/, "", $2); print $2; exit }' "$TMP_DIR/fetch.err")
			_n=$((_n + 1))
			case $_u in
			https://*) [ "$_n" -le 5 ] || { _rc=1 _code=; echo "too many redirects" >>"$TMP_DIR/fetch.err"; break; } ;;
			'') _rc=1 _code=; echo "a redirect without a Location" >>"$TMP_DIR/fetch.err"; break ;;
			*) _rc=1 _code=; echo "redirected to '$(printable "$_u")', which is not https" >>"$TMP_DIR/fetch.err"; break ;;
			esac
		done
	fi
	if [ "$_rc" -eq 0 ] && [ "$_code" = 200 ]; then return 0; fi
	rm -f -- "$2"
	if [ "$_code" = 404 ]; then return 3; fi
	FETCH_ERROR="HTTP status ${_code:-none}, $DOWNLOADER exit status $_rc"
	_detail=$(lastline "$TMP_DIR/fetch.err")
	if [ -n "$_detail" ]; then FETCH_ERROR="$FETCH_ERROR: $_detail"; fi
	return 1
}

# resolve_latest: sets TAG from the redirect of $REPO_URL/releases/latest
resolve_latest() {
	_url=$REPO_URL/releases/latest
	if [ "$DOWNLOADER" = curl ]; then
		_out=$(curl -q -sS --proto =https --connect-timeout "$TIMEOUT" --max-time "$TIMEOUT" \
			-o /dev/null -w '%{http_code} %{redirect_url}' "$_url" 2>"$TMP_DIR/fetch.err") ||
			die "cannot reach $_url: $(errtext "$TMP_DIR/fetch.err")"
		_code=${_out%% *}
		_loc=${_out#* }
	else
		wget -S --https-only --check-certificate --tries=1 --timeout="$TIMEOUT" \
			--max-redirect=0 -O /dev/null -o "$TMP_DIR/fetch.err" "$_url"
		_code=$(awk '$1 ~ /^HTTP\// { print $2; exit }' "$TMP_DIR/fetch.err")
		[ -n "$_code" ] ||
			die "cannot reach $_url: $(lastline "$TMP_DIR/fetch.err")"
		_loc=$(awk 'tolower($1) == "location:" { sub(/\r$/, "", $2); print $2; exit }' "$TMP_DIR/fetch.err")
	fi
	case $_code in
	301 | 302 | 303 | 307 | 308) ;;
	*) die "cannot find the latest release: $_url answered HTTP status ${_code:-none}" ;;
	esac
	_want=$(printf '%s' "$REPO_URL/releases/tag/" | tr '[:upper:]' '[:lower:]')
	_tag=${_loc##*/}
	_lower=$(printf '%s' "$_loc" | tr '[:upper:]' '[:lower:]')
	_lower_tag=$(printf '%s' "$_tag" | tr '[:upper:]' '[:lower:]')
	if [ "$_lower" != "$_want$_lower_tag" ] || [ -z "$_tag" ]; then
		die "cannot find the latest release: $_url redirects to '$(printable "$_loc")' (no stable release yet?)"
	fi
	valid_tag "$_tag" || die "the latest release has the tag '$(printable "$_tag")', which is not a version like v1.2.3"
	TAG=$_tag
}

# --- 7-9. checksums ------------------------------------------------------------

# expected_sha256 FILE NAME: prints the checksum of NAME listed in FILE
expected_sha256() {
	awk -v name="$2" '
		{
			if (length($0) < 67 || substr($0, 1, 64) ~ /[^0-9a-f]/ ||
			    substr($0, 65, 2) != "  " || substr($0, 67) ~ /[ \t\r]/) {
				bad = 1
				exit
			}
			if (substr($0, 67) == name) { n++; sum = substr($0, 1, 64) }
		}
		END {
			if (bad) exit 3
			if (n == 0) exit 4
			if (n > 1) exit 5
			print sum
		}' "$1"
}

# --- 10-12. archive -----------------------------------------------------------

# check_entries LIST VERBOSE_LIST: every entry is safe to have in the archive
check_entries() {
	awk -v app="$APP_NAME" '
		BEGIN { for (i = 1; i < 32; i++) ctl = ctl sprintf("%c", i); ctl = ctl sprintf("%c", 127) }
		{
			name = $0
			for (i = 1; i <= length(ctl); i++)
				if (index(name, substr(ctl, i, 1))) reject("a name with a control character")
			if (index(name, "\\")) reject("a name with a backslash: " name)
			if (substr(name, 1, 1) == "/") reject("an absolute path: " name)
			key = name
			sub(/\/+$/, "", key)
			# "./x", "a//b", "a/./b" name the same file as "x", "a/b": refused,
			# so that a duplicate cannot hide behind another spelling
			k = split(key, part, "/")
			for (i = 1; i <= k; i++) {
				if (part[i] == "..") reject("a path with \"..\": " name)
				if (part[i] == "" || part[i] == ".") reject("a name with an empty or \".\" part: " name)
			}
			if (key in seen) reject("a duplicate entry: " name)
			seen[key] = 1
			if (key == app) n++
			else if (substr(key, 1, length(app) + 1) == app "/") reject("an entry below " app "/: " name)
		}
		function reject(why) { print why; failed = 1; exit 1 }
		END {
			if (failed) exit 1
			if (n != 1) { print "no " app " at the top of the archive"; exit 1 }
		}' "$1" || return 1
	# " -> " is how every tar lists a symbolic link, and BusyBox a hard link
	awk '{
			t = substr($0, 1, 1)
			if ((t != "-" && t != "d") || index($0, " -> ")) {
				print "an entry that is not a file or directory: " $0
				exit 1
			}
		}' "$2" || return 1
}

# extract_binary: puts the binary from $TMP_DIR/archive.tar.gz at $NEW_BIN
extract_binary() {
	gzip -dc <"$TMP_DIR/$ASSET" >"$TMP_DIR/archive.tar" 2>"$TMP_DIR/tar.err" ||
		die "$ASSET is not a valid gzip file: $(errtext "$TMP_DIR/tar.err")"
	tar -tf "$TMP_DIR/archive.tar" >"$TMP_DIR/list" 2>"$TMP_DIR/tar.err" ||
		die "$ASSET is not a valid tar archive: $(errtext "$TMP_DIR/tar.err")"
	tar -tvf "$TMP_DIR/archive.tar" >"$TMP_DIR/list.v" 2>>"$TMP_DIR/tar.err" ||
		die "$ASSET is not a valid tar archive: $(errtext "$TMP_DIR/tar.err")"
	_why=$(check_entries "$TMP_DIR/list" "$TMP_DIR/list.v") ||
		die "$ASSET is rejected: it has $_why"
	# A tar that rewrites unsafe names while listing them (BusyBox drops a
	# leading "/" or "../") says so on stderr: such an archive is refused too.
	[ ! -s "$TMP_DIR/tar.err" ] ||
		die "$ASSET is rejected: tar reported: $(errtext "$TMP_DIR/tar.err")"
	mkdir "$TMP_DIR/x" || die "cannot create a directory in $TMP_DIR"
	(cd "$TMP_DIR/x" && tar -xf "$TMP_DIR/archive.tar" "$APP_NAME") 2>"$TMP_DIR/tar.err" ||
		die "cannot extract $APP_NAME from $ASSET: $(errtext "$TMP_DIR/tar.err")"
	NEW_BIN=$TMP_DIR/x/$APP_NAME
	if ! { [ "$(ls -A "$TMP_DIR/x")" = "$APP_NAME" ] &&
		[ -f "$NEW_BIN" ] && [ ! -L "$NEW_BIN" ] && [ -s "$NEW_BIN" ]; }; then
		die "$ASSET is rejected: $APP_NAME in it is not a regular file"
	fi
}

# reports_tag FILE: FILE runs and its --version names $TAG
reports_tag() {
	_out=$("$1" --version </dev/null 2>/dev/null) || return 1
	_line=${_out%%"$NL"*}
	[ "$_line" = "$APP_NAME version $TAG" ] || [ "$_line" = "$APP_NAME version ${TAG#v}" ]
}

# --- main ---------------------------------------------------------------------

main() {
	check_config
	parse_args "$@"
	check_tools
	detect_platform

	# Check what can be checked before downloading anything.
	if [ -e "$INSTALL_DIR" ] || [ -L "$INSTALL_DIR" ]; then
		check_dir
		check_target
		OLD_TAG=$(installed_tag "$TARGET")
	else
		check_ancestor
		OLD_TAG=
	fi

	TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/$APP_NAME-install.XXXXXX") ||
		die "cannot create a temporary directory (set TMPDIR to a writable directory)"
	# an absolute path: the archive is extracted from another directory
	_abs=$(cd -P -- "$TMP_DIR" && pwd -P) || die "cannot enter '$TMP_DIR'"
	TMP_DIR=$_abs

	if [ -n "$REQ_TAG" ]; then
		TAG=$REQ_TAG
	else
		resolve_latest
	fi
	VERSION=${TAG#v}
	ASSET=${APP_NAME}_${VERSION}_${OS}_${ARCH}.tar.gz
	BASE_URL=$REPO_URL/releases/download/$TAG
	say "$APP_NAME $TAG for $OS/$ARCH"

	# The checksum file first: it is small, and it lists the archives the
	# release has. Releases made before <APP>_checksums.txt existed have
	# <APP>_<version>_checksums.txt; that name is tried only on a 404.
	_sums=$TMP_DIR/checksums.txt
	fetch "$BASE_URL/${APP_NAME}_checksums.txt" "$_sums"
	_rc=$?
	if [ "$_rc" -eq 3 ]; then
		fetch "$BASE_URL/${APP_NAME}_${VERSION}_checksums.txt" "$_sums"
		_rc=$?
		[ "$_rc" -ne 3 ] || die "release $TAG not found at $REPO_URL/releases (or it has no checksum file)"
	fi
	[ "$_rc" -eq 0 ] || die "cannot download the checksum file of $TAG: $FETCH_ERROR"
	_want=$(expected_sha256 "$_sums" "$ASSET")
	case $? in
	0) ;;
	3) die "the checksum file of $TAG is malformed" ;;
	4) die "release $TAG has no $ASSET ($OS/$ARCH is not built for it)" ;;
	5) die "the checksum file of $TAG lists $ASSET more than once" ;;
	*) die "cannot read the checksum file of $TAG" ;;
	esac

	fetch "$BASE_URL/$ASSET" "$TMP_DIR/$ASSET"
	case $? in
	0) ;;
	3) die "release $TAG has no $ASSET" ;;
	*) die "cannot download $ASSET: $FETCH_ERROR" ;;
	esac
	_got=$(sha256_of "$TMP_DIR/$ASSET") || die "cannot compute the SHA-256 of $ASSET"
	[ "$_got" = "$_want" ] ||
		die "SHA-256 mismatch for $ASSET (expected $_want, got $_got); nothing was installed"
	say "Verified the SHA-256 of $ASSET."

	extract_binary
	_new_sum=$(sha256_of "$NEW_BIN") || die "cannot compute the SHA-256 of the new binary"

	# Prepare the directory, then check it (again: it may have changed).
	if [ ! -e "$INSTALL_DIR" ] && [ ! -L "$INSTALL_DIR" ]; then
		mkdir -p -- "$INSTALL_DIR" || die "cannot create '$INSTALL_DIR'"
	fi
	check_dir
	check_target

	# The same file is already there (and executable): leave it alone.
	if [ -f "$TARGET" ] && [ -x "$TARGET" ] && _old_sum=$(sha256_of "$TARGET") &&
		[ "$_old_sum" = "$_new_sum" ]; then
		say "$APP_NAME $TAG is already installed at $TARGET; nothing changed."
		report_path
		return 0
	fi

	if [ -z "$OLD_TAG" ] && [ -f "$TARGET" ]; then
		warn "'$TARGET' did not report an $APP_NAME version (or was not run: not executable, or not yours); it is replaced"
		_action="Replaced $TARGET (unknown version) with $APP_NAME $TAG"
	elif [ -z "$OLD_TAG" ]; then
		if [ "$UPGRADE" = yes ]; then say "No $APP_NAME at $TARGET to upgrade: installing it."; fi
		_action="Installed $APP_NAME $TAG to $TARGET"
	else
		case $(compare_tags "$OLD_TAG" "$TAG") in
		lt) _action="Upgraded $APP_NAME $OLD_TAG to $TAG at $TARGET" ;;
		gt) _action="Downgraded $APP_NAME $OLD_TAG to $TAG at $TARGET" ;;
		eq) _action="Reinstalled $APP_NAME $TAG at $TARGET (the file differed)" ;;
		*) _action="Switched $APP_NAME $OLD_TAG to $TAG at $TARGET" ;;
		esac
	fi

	# Copy into the install directory, check the copy, then rename it over the
	# old binary: rename(2) in one directory replaces it in one step.
	STAGED=$(mktemp "$PHYS_DIR/.$APP_NAME.install.XXXXXX") ||
		die "cannot create a temporary file in '$PHYS_DIR'"
	cat "$NEW_BIN" >"$STAGED" || die "cannot write to '$PHYS_DIR' (disk full?)"
	chmod 755 "$STAGED" || die "cannot make '$STAGED' executable"
	[ "$(sha256_of "$STAGED")" = "$_new_sum" ] || die "the copy in '$PHYS_DIR' is incomplete"
	reports_tag "$STAGED" ||
		die "the new $APP_NAME does not run in '$PHYS_DIR' or does not report $TAG (a noexec file system?); nothing was replaced"
	check_target
	mv -f -- "$STAGED" "$TARGET" || die "cannot move the new binary to '$TARGET'; nothing was replaced"
	STAGED=
	if [ ! -f "$TARGET" ] || [ -L "$TARGET" ]; then
		die "'$TARGET' changed while installing"
	fi

	say "$_action."
	report_path
}

# report_path: tell how to run the installed binary
report_path() {
	case :${PATH:-}: in
	*:"$PHYS_DIR":* | *:"$INSTALL_DIR":*)
		_first=$(command -v "$APP_NAME" 2>/dev/null) || _first=
		case $_first in /*) ;; *) _first= ;; esac # a function or builtin of that name
		if [ -n "$_first" ] && [ "$_first" != "$TARGET" ] && [ "$_first" != "$INSTALL_DIR/$APP_NAME" ]; then
			warn "'$_first' comes first in PATH, so '$APP_NAME' runs that one; run '$TARGET' or change PATH"
		fi
		;;
	*)
		say "'$PHYS_DIR' is not in PATH. Add it, e.g. in ~/.profile:"
		say "  export PATH=\"$PHYS_DIR:\$PATH\""
		;;
	esac
}

main "$@"
