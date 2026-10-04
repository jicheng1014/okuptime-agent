#!/bin/sh
# Official first-install bootstrap. Existing installations are never overwritten.
set -eu
fail() { echo "$*" >&2; exit 1; }
case "$(uname -s)" in Darwin) platform=darwin;; Linux) platform=linux;; *) echo 'Unsupported system' >&2; exit 1;; esac
case "$(uname -m)" in x86_64|amd64) architecture=amd64;; arm64|aarch64) architecture=arm64;; *) echo 'Unsupported architecture' >&2; exit 1;; esac
target="$HOME/.local/bin/okuptime"
if command -v okuptime >/dev/null 2>&1; then exec okuptime version --json; fi
if [ -e "$target" ] || [ -L "$target" ]; then exec "$target" version --json; fi
command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
status=$(curl --fail --silent --show-error --write-out '%{http_code}' --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --max-filesize 65536 "https://www.okuptime.com/api/v1/cli/releases/latest?platform=$platform&architecture=$architecture" -o "$work/metadata")
[ "$status" = 200 ] || fail "Metadata HTTP $status; redirects are refused"
# Extract only the release API's flat scalar fields. Reject missing/duplicate fields.
field() {
  value=$(tr ',' '\n' < "$work/metadata" | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p")
  [ -n "$value" ] && [ "$(printf '%s\n' "$value" | wc -l | tr -d ' ')" = 1 ] || { echo "Missing or duplicate metadata field: $1" >&2; return 1; }
  printf '%s' "$value"
}
url=$(field url); checksum=$(field sha256); version=$(field version)
[ "$(field platform)" = "$platform" ] || fail 'Platform mismatch'
[ "$(field architecture)" = "$architecture" ] || fail 'Architecture mismatch'
size=$(tr ',' '\n' < "$work/metadata" | sed -n 's/.*"size"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\)[[:space:]}]*$/\1/p')
case "$size" in ''|*[!0-9]*) echo 'Invalid release size' >&2; exit 1;; esac
[ "$size" -gt 0 ] || fail 'Invalid release size'
[ "$size" -le 67108864 ] || fail 'Release exceeds 64 MiB'
printf '%s\n' "$checksum" | LC_ALL=C grep -Eq '^[0-9a-f]{64}$' || fail 'Invalid SHA256'
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail 'Invalid release version'
printf '%s\n' "$url" | LC_ALL=C grep -Eq '^https://www\.okuptime\.com/cli/releases/[A-Za-z0-9._/-]+$' || fail 'Invalid official download URL'
case "$url" in */../*|*/./*) echo 'Invalid download path' >&2; exit 1;; esac
status=$(curl --fail --silent --show-error --write-out '%{http_code}' --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 180 --max-filesize "$size" "$url" -o "$work/okuptime")
[ "$status" = 200 ] || fail "Download HTTP $status; redirects are refused"
[ "$(wc -c < "$work/okuptime" | tr -d ' ')" = "$size" ] || fail 'Size mismatch'
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$work/okuptime" | cut -d ' ' -f 1); else actual=$(shasum -a 256 "$work/okuptime" | cut -d ' ' -f 1); fi
[ "$actual" = "$checksum" ] || { echo 'Checksum mismatch; installation stopped' >&2; exit 1; }
chmod 755 "$work/okuptime"
# Check the downloaded program before installing it.
"$work/okuptime" version --json > "$work/version"
installed_version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^" ]*\)".*/\1/p' "$work/version")
[ "$installed_version" = "$version" ] || { echo 'Version mismatch' >&2; exit 1; }
cat "$work/version"
mkdir -p "$(dirname "$target")"
# Hard-link from a temporary file in the destination directory: atomic, no overwrite.
staged=$(mktemp "$(dirname "$target")/.okuptime-install.XXXXXX")
trap 'rm -rf "$work"; rm -f "$staged"' EXIT HUP INT TERM
cp "$work/okuptime" "$staged"
chmod 755 "$staged"
ln "$staged" "$target"
printf 'Installed %s at %s\n' "$version" "$target"
