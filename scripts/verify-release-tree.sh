#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"

if ! git diff --quiet --ignore-submodules -- || ! git diff --cached --quiet --ignore-submodules --; then
    echo "release tree has tracked or staged changes" >&2
    exit 1
fi

allowed_untracked=${RELEASE_ALLOWED_UNTRACKED:-}
unexpected=""
while IFS= read -r path; do
    [ -n "$path" ] || continue
    allowed=false
    while IFS= read -r allowed_path; do
        if [ -n "$allowed_path" ] && [ "$path" = "$allowed_path" ]; then
            allowed=true
            break
        fi
    done <<EOF
$allowed_untracked
EOF
    if [ "$allowed" = false ]; then
        unexpected="${unexpected}${unexpected:+
}${path}"
    fi
done <<EOF
$(git ls-files --others --exclude-standard)
EOF

if [ -n "$unexpected" ]; then
    echo "release tree has unexpected untracked files:" >&2
    printf '%s\n' "$unexpected" >&2
    exit 1
fi

echo "Release tree is clean at $(git rev-parse HEAD)."
