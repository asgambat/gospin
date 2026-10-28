#!/usr/bin/env bash
set -euo pipefail

# The bump reads the current version from .version (the documented source of
# truth) and rewrites every file that carries it. Published registry tags are
# immutable, so the script refuses anything that would move the version
# backwards: a lower or equal version, or a tag that already exists.
VERSION_FILE=".version"
NEW="${1:-}"
OLD="$(tr -d '[:space:]' < "$VERSION_FILE" 2>/dev/null || true)"

if [ -z "$NEW" ]; then
  echo "ERROR: specify the new version, e.g. task version:bump -- 0.1.0"
  echo "Current version: ${OLD:-unknown}"
  exit 1
fi

if [ -z "$OLD" ]; then
  echo "ERROR: cannot read the current version from $VERSION_FILE"
  exit 1
fi

if ! printf '%s' "$NEW" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "ERROR: version '$NEW' is not in MAJOR.MINOR.PATCH form (e.g. 0.1.0)"
  echo "Current version: $OLD"
  exit 1
fi

if [ "$NEW" = "$OLD" ]; then
  echo "ERROR: new version ($NEW) must differ from current version ($OLD)"
  exit 1
fi

# sort -V orders the two versions; if the newest is not NEW, NEW is a downgrade.
HIGHEST=$(printf '%s\n%s\n' "$OLD" "$NEW" | sort -V | tail -n 1)
if [ "$HIGHEST" != "$NEW" ]; then
  echo "ERROR: new version ($NEW) is lower than current version ($OLD)"
  exit 1
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "ERROR: not inside a git repository"
  exit 1
}

if git rev-parse -q --verify "refs/tags/v$NEW" >/dev/null; then
  echo "ERROR: tag v$NEW already exists; published tags are never moved"
  exit 1
fi

# The compiled default must match .version, otherwise the sed below rewrites
# every file except internal/version/version.go and the binary keeps reporting
# the old string.
GO_VERSION=$(grep 'var Version = ' internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')
if [ "$GO_VERSION" != "$OLD" ]; then
  echo "ERROR: $VERSION_FILE ($OLD) and internal/version/version.go ($GO_VERSION) disagree"
  echo "Align them before bumping so every file is rewritten consistently."
  exit 1
fi

VERSION_FILES=(
  "$VERSION_FILE"
  internal/version/version.go
  Dockerfile
  README.md
  .github/workflows/docker-release.yml
  .github/workflows/docker_build_push.yml
  Taskfile.yml
  .task/version_bump.sh
)

echo "Bumping $OLD → $NEW across all files..."

# Escape the dots so sed treats them as literal characters rather than
# "any character" (0.1.0 → 0\.0\.12). Bash parameter expansion keeps the
# number of backslashes exact; piping through sed's own replacement syntax
# doubles them and the pattern stops matching anything.
OLD_ESC="${OLD//./\\.}"

# Portable sed -i (BSD/macOS requires an explicit empty backup extension)
case "$(uname -s)" in
  Darwin) SED_INPLACE="sed -i ''" ;;
  *)      SED_INPLACE="sed -i" ;;
esac

for f in "${VERSION_FILES[@]}"; do
  $SED_INPLACE "s/$OLD_ESC/$NEW/g" "$f"
done

# Fail before committing if any file still carries the old version.
for f in "${VERSION_FILES[@]}"; do
  if grep -Fq "$OLD" "$f"; then
    echo "ERROR: $f still contains $OLD after the rewrite"
    exit 1
  fi
done

echo ""
echo "Version bumped to $NEW in:"
for f in "${VERSION_FILES[@]}"; do
  echo "   • $f"
done

# Stage only the version-controlled files, then commit and tag
git add "${VERSION_FILES[@]}"
if git diff --cached --quiet; then
  echo "ERROR: no staged changes to commit after the rewrite"
  exit 1
fi

git commit -m "chore: bump to $NEW"
git tag "v$NEW"
echo ""
echo "Committed and tagged v$NEW"
echo "Run 'git push origin --tags' to publish."
