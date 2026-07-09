#!/bin/sh
set -eu

version=${1:?usage: scripts/update-homebrew-formula.sh <tag> [dist-dir] [formula-path]}
dist_dir=${2:-dist}
formula=${3:-Formula/amux.rb}

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
text = re.sub(r'version "[^"]+"', f'version "{formula_version}"', text, count=1)

replacements = {
    "darwin-arm64": darwin_arm64,
    "darwin-amd64": darwin_amd64,
    "linux-arm64": linux_arm64,
    "linux-amd64": linux_amd64,
}

for platform, sha in replacements.items():
    text = re.sub(
        rf'url "https://github\.com/zainfathoni/amux/releases/download/v[^/]+/amux-v[^/]+-{platform}\.tar\.gz"\n\s+sha256 "[0-9a-f]+"',
        f'url "https://github.com/zainfathoni/amux/releases/download/{tag}/amux-{tag}-{platform}.tar.gz"\n      sha256 "{sha}"',
        text,
        count=1,
    )

formula_path.write_text(text)
PY

echo "Updated $formula to $version"
