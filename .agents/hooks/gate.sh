#!/usr/bin/env bash
# Stop gate. Go: vet + test -race. UI: typecheck + lint + test.
# Claude Code: exit 2 + stderr. Cursor: {"followup_message": ...}.
set -u
UI_DIR=${UI_DIR:-web/control-ui}
in=$(cat)

client=claude
jq -e 'has("cursor_version")' >/dev/null 2>&1 <<<"$in" && client=cursor
pass() { [ "$client" = cursor ] && echo '{}'; exit 0; }

root=${CLAUDE_PROJECT_DIR:-$(jq -r '.workspace_roots[0] // .cwd // empty' <<<"$in")}
cd "${root:-.}" || pass

# Loop guards
if [ "$client" = claude ] && [ "$(jq -r '.stop_hook_active // false' <<<"$in")" = true ]; then pass; fi
if [ "$client" = cursor ] && [ "$(jq -r '.loop_count // 0' <<<"$in")" -ge 3 ]; then pass; fi

changed() {
  ! git diff --quiet HEAD -- "$@" 2>/dev/null \
    || [ -n "$(git ls-files --others --exclude-standard -- "$@")" ]
}

fail=""

# --- Go ---
if changed '*.go' 'go.mod' 'go.sum' 'internal/scenario/lab'; then
  gofiles=$( { git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go'; } 2>/dev/null | sort -u | while read -r f; do [ -f "$f" ] && echo "$f"; done )
  unfmt=$( [ -n "$gofiles" ] && gofmt -l $gofiles )
  [ -n "$unfmt" ] && fail+=$'\n### Go: not gofmt-formatted (run gofmt -w on these files)\n'"$unfmt"$'\n'
  out=$( set -o pipefail; { go vet ./... && go test -race -count=1 ./... ; } 2>&1 | { grep -vE '^(ok|\?) ' || true; } ) \
    || fail+=$'\n### Go: go vet / go test -race\n'"$(tail -n 60 <<<"$out")"$'\n'
fi

# --- UI ---
if changed "$UI_DIR"; then
  if [ ! -d "$UI_DIR/node_modules" ]; then
    fail+=$'\n### UI: node_modules missing — run `npm ci` in '"$UI_DIR"$'\n'
  else
    for s in typecheck lint test; do
      out=$(cd "$UI_DIR" && CI=1 npm run --silent "$s" 2>&1) \
        || fail+=$'\n### UI: npm run '"$s"$'\n'"$(tail -n 60 <<<"$out")"$'\n'
    done
  fi
fi

[ -z "$fail" ] && pass

msg=$(printf 'Checks failed. Fix the cause (do not skip, delete or weaken tests/lint rules), then finish:\n%s\n' "$fail")
if [ "$client" = cursor ]; then
  jq -n --arg m "$msg" '{followup_message: $m}'; exit 0
else
  printf '%s\n' "$msg" >&2; exit 2
fi
