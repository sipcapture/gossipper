#!/usr/bin/env bash
# After-edit formatter. Claude Code (PostToolUse) + Cursor (afterFileEdit).
set -u
UI_DIR=${UI_DIR:-web/control-ui}
in=$(cat)
f=$(jq -r '.tool_input.file_path // .file_path // empty' <<<"$in")
[ -n "$f" ] || exit 0

root=${CLAUDE_PROJECT_DIR:-$(jq -r '.workspace_roots[0] // empty' <<<"$in")}
root=${root:-$(pwd)}
case "$f" in /*) abs=$f ;; *) abs=$root/$f ;; esac
[ -f "$abs" ] || exit 0

case "$abs" in
  *.go)
    gofmt -w "$abs" >/dev/null 2>&1 ;;
  "$root/$UI_DIR/node_modules/"*)
    ;;
  "$root/$UI_DIR/"*.ts|"$root/$UI_DIR/"*.tsx|"$root/$UI_DIR/"*.js|"$root/$UI_DIR/"*.jsx)
    bin="$root/$UI_DIR/node_modules/.bin"
    [ -x "$bin/eslint" ] && ( cd "$root/$UI_DIR" && "$bin/eslint" --fix --quiet "$abs" >/dev/null 2>&1 ) ;;
esac
exit 0
