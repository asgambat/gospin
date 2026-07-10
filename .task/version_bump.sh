#!/usr/bin/env bash
set -euo pipefail

NEW="$1"
OLD=$(grep 'var Version = ' internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')

if [ -z "$NEW" ]; then
  echo "ERROR: specify the new version, e.g. task version:bump -- 0.0.11"
  echo "Current version: $OLD"
  exit 1
fi

if [ "$NEW" = "$OLD" ]; then
  echo "ERROR: new version ($NEW) must differ from current version ($OLD)"
  exit 1
fi

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "ERROR: not inside a git repository"
  exit 1
}

VERSION_FILES=".version internal/version/version.go Makefile Dockerfile README.md docs/progetto.txt .github/workflows/docker-release.yml .github/workflows/docker_build_push.yml Taskfile.yml"

echo "Bumping $OLD → $NEW across all files..."

# Escape dots so they are literal in the sed pattern (0.0.10 → 0\.0\.10)
OLD_ESC=$(echo "$OLD" | sed 's/\./\\./g')

# Portable sed -i (BSD/macOS requires an explicit empty backup extension)
case "$(uname -s)" in
  Darwin) SED_INPLACE="sed -i ''" ;;
  *)      SED_INPLACE="sed -i" ;;
esac

for f in $VERSION_FILES; do
  $SED_INPLACE "s/$OLD_ESC/$NEW/g" "$f"
done

echo ""
echo "Version bumped to $NEW in:"
for f in $VERSION_FILES; do
  echo "   • $f"
done

# Stage only the version-controlled files, then commit and tag
git add $VERSION_FILES
if git commit -m "chore: bump to $NEW"; then
  git tag -f "v$NEW"
  echo ""
  echo "Committed and tagged v$NEW"
  echo "Run 'git push --follow-tags' to publish."
else
  echo ""
  echo "Nothing to commit (version may already be $NEW)"
  exit 1
fi
