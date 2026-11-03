#!/bin/sh
set -eu
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "Usage: $0 archive.zip [http://127.0.0.1:port]" >&2; exit 2
fi
archive=$1
[ -f "$archive" ] || { echo "ZIP file not found: $archive" >&2; exit 1; }
url=${2:-}
if [ -z "$url" ]; then
  case "$(uname -s)" in
    Darwin) config="$HOME/Library/Application Support" ;;
    *) config=${XDG_CONFIG_HOME:-$HOME/.config} ;;
  esac
  endpoint="$config/Learn-With-AI/transfer-endpoint.json"
  url=$(sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$endpoint")
fi
if ! printf '%s\n' "$url" | LC_ALL=C grep -Eq '^http://127\.0\.0\.1:[0-9]{1,5}/?$'; then
  echo 'Expected a local Learn-With-AI server URL.' >&2; exit 1
fi
reply=$(curl -q --noproxy '*' --silent --show-error --write-out '\n%{http_code}' \
  -H 'X-LLL-Workspace: 1' -H 'Content-Type: application/zip' \
  --data-binary @- "${url%/}/api/workspace/import" < "$archive")
code=$(printf '%s\n' "$reply" | tail -n 1)
case "$code" in
  2??) printf '%s\n' "$reply" | sed '$d' ;;
  *) printf '%s\n' "$reply" >&2; exit 1 ;;
esac
