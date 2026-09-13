#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'action-pin: %s\n' "$*" >&2
  exit 1
}

version="${INPUT_VERSION:-source}"
checksum="${INPUT_CHECKSUM:-}"
for value in "${INPUT_CHECK:-true}" "${INPUT_FIX:-false}" "${INPUT_DIFF:-false}" "${INPUT_RESOLVE:-false}"; do
  case "$value" in true|false) ;; *) fail 'check, fix, diff, and resolve must be true or false' ;; esac
done
if [[ "${INPUT_FIX:-false}" == true && "${INPUT_DIFF:-false}" == true ]]; then
  fail 'fix and diff cannot both be true'
fi

args=()
if [[ "${INPUT_DIFF:-false}" == true ]]; then
  # The Action defaults check to true, but a diff is its own CLI mode.
  args+=(--diff)
elif [[ "${INPUT_FIX:-false}" == true ]]; then
  args+=(--fix)
else
  args+=(--check)
fi
if [[ "${INPUT_RESOLVE:-false}" == true ]]; then
  args+=(--resolve)
fi
if [[ -n "${INPUT_DIR:-}" ]]; then
  args+=(--dir "$INPUT_DIR")
fi

if [[ "$version" == source ]]; then
  [[ -z "$checksum" ]] || fail 'checksum is only valid with an exact release version'
  command -v go >/dev/null 2>&1 || fail 'source mode requires Go 1.22 or newer; install Go or set an exact version and checksum'
  [[ -f "${ACTION_PATH:?ACTION_PATH is required}/go.mod" ]] || fail 'action source is missing go.mod'
else
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || fail 'version must be source or an exact release tag (for example v1.0.0); latest and branches are not supported'
  [[ "$checksum" =~ ^[[:xdigit:]]{64}$ ]] || fail 'release mode requires the 64-character SHA-256 checksum of the runner archive'
fi

work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT

if [[ "$version" == source ]]; then
  printf 'Building action-pin from the selected action source\n' >&2
  # Use host build defaults even in a caller's cross-compilation job. Ignore
  # persisted Go settings, go.work and build flags. Run in the caller's directory.
  (
    cd "$ACTION_PATH"
    GOENV=off GO111MODULE=on GOOS= GOARCH= GOAMD64= GOARM= GOARM64= GOEXPERIMENT= \
      GOWORK=off GOFLAGS= GOTOOLCHAIN=local CGO_ENABLED=0 \
      go build -mod=readonly -trimpath -o "$work_dir/action-pin" ./cmd/action-pin
  )
  "$work_dir/action-pin" "${args[@]}"
  exit
fi

case "$(uname -s)" in
  Linux*) os=linux ;;
  Darwin*) os=darwin ;;
  MSYS*|MINGW*|CYGWIN*) os=windows ;;
  *) fail 'unsupported runner operating system' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) fail 'unsupported runner architecture' ;;
esac

extension=tar.gz
binary=action-pin
if [[ "$os" == windows ]]; then
  extension=zip
  binary=action-pin.exe
fi
asset="action-pin_${version#v}_${os}_${arch}.${extension}"
url="https://github.com/emirhan-karaca/action-pin/releases/download/${version}/${asset}"
archive="$work_dir/$asset"
printf 'Downloading action-pin %s for %s/%s\n' "$version" "$os" "$arch" >&2
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  --retry 2 --connect-timeout 10 --max-time 120 --output "$archive" "$url"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive")
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$archive")
else
  fail 'sha256sum or shasum is required to verify the release archive'
fi
expected=$(printf '%s' "$checksum" | tr '[:upper:]' '[:lower:]')
[[ "${actual%% *}" == "$expected" ]] || fail 'release archive SHA-256 mismatch; refusing to extract or execute it'

if [[ "$os" == windows ]]; then
  unzip -q "$archive" "$binary" -d "$work_dir"
else
  tar -xzf "$archive" -C "$work_dir" "$binary"
fi
chmod +x "$work_dir/$binary"
"$work_dir/$binary" "${args[@]}"
