#!/bin/sh
set -eu

version=${1:?usage: scripts/update-homebrew-formula.sh <tag> [dist-dir] <formula-path>}
dist_dir=${2:-dist}
formula=${3:?usage: scripts/update-homebrew-formula.sh <tag> [dist-dir] <formula-path>}

case "$version" in
	v*) formula_version=${version#v} ;;
	*) formula_version=$version ;;
esac

sha_for() {
	asset=$1
	checksum_file="$dist_dir/$asset.sha256"
	if [ ! -f "$checksum_file" ]; then
		echo "missing checksum file: $checksum_file" >&2
		exit 1
	fi
	awk '{print $1}' "$checksum_file"
}

darwin_amd64=$(sha_for "amux-${version}-darwin-amd64.tar.gz")
darwin_arm64=$(sha_for "amux-${version}-darwin-arm64.tar.gz")
linux_amd64=$(sha_for "amux-${version}-linux-amd64.tar.gz")
linux_arm64=$(sha_for "amux-${version}-linux-arm64.tar.gz")

python3 - "$formula" "$version" "$formula_version" "$darwin_arm64" "$darwin_amd64" "$linux_arm64" "$linux_amd64" <<'PY'
import re
import sys
from pathlib import Path

formula_path = Path(sys.argv[1])
tag = sys.argv[2]
formula_version = sys.argv[3]
darwin_arm64 = sys.argv[4]
darwin_amd64 = sys.argv[5]
linux_arm64 = sys.argv[6]
linux_amd64 = sys.argv[7]

text = formula_path.read_text()

replacements = {
    "darwin-arm64": darwin_arm64,
    "darwin-amd64": darwin_amd64,
    "linux-arm64": linux_arm64,
    "linux-amd64": linux_amd64,
}

def replace_once(pattern, replacement, label):
    global text
    count = len(re.findall(pattern, text))
    if count != 1:
        raise SystemExit(f"expected exactly one replacement for {label}, got {count}")
    text = re.sub(pattern, replacement, text, count=1)

replace_once(r'version "[^"]+"', f'version "{formula_version}"', "formula version")

for platform, sha in replacements.items():
    replace_once(
        rf'url "https://github\.com/zainfathoni/amux/releases/download/v[^/]+/amux-v[^/]+-{platform}\.tar\.gz"\n\s+sha256 "[0-9a-f]+"',
        f'url "https://github.com/zainfathoni/amux/releases/download/{tag}/amux-{tag}-{platform}.tar.gz"\n      sha256 "{sha}"',
        platform,
    )

for platform in replacements:
    expected_url = f'https://github.com/zainfathoni/amux/releases/download/{tag}/amux-{tag}-{platform}.tar.gz'
    count = text.count(expected_url)
    if count != 1:
        raise SystemExit(f"updated formula must contain expected URL exactly once: {expected_url} (got {count})")

formula_path.write_text(text)
PY

echo "Updated $formula to $version"
