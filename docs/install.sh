#!/bin/sh
set -eu

repository=https://github.com/zainfathoni/amux
install_dir=${HOME:+"$HOME/.local/bin"}
install_path=${install_dir:+"$install_dir/amux"}
work_dir=
install_tmp=

say() {
	printf '%s\n' "$*"
}

fail() {
	printf 'amux installer: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	status=$?
	trap - 0 HUP INT TERM
	if [ -n "$install_tmp" ]; then
		rm -f "$install_tmp" || true
	fi
	if [ -n "$work_dir" ]; then
		rm -rf "$work_dir" || true
	fi
	exit "$status"
}

trap cleanup 0
trap 'exit 1' HUP INT TERM

for command_name in uname curl tar awk mktemp mkdir cp chmod mv rm; do
	command -v "$command_name" >/dev/null 2>&1 || fail "required command not found: $command_name"
done

[ -n "${HOME:-}" ] || fail 'HOME is not set; cannot determine the canonical install path'
case "$HOME" in
	/*) ;;
	*) fail "HOME must be an absolute path (got $HOME)" ;;
esac

case "$(uname -s 2>/dev/null || true)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "unsupported operating system: $(uname -s 2>/dev/null || printf unknown); amux publishes binaries for Darwin and Linux" ;;
esac

case "$(uname -m 2>/dev/null || true)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "unsupported architecture: $(uname -m 2>/dev/null || printf unknown); amux publishes binaries for arm64 and amd64" ;;
esac

version=${AMUX_VERSION:-}
if [ -n "$version" ]; then
	case "$version" in
		v?*) ;;
		*) fail "AMUX_VERSION must be a release tag such as v0.2.1 (got $version)" ;;
	esac
	case "$version" in
		*[!A-Za-z0-9._-]*) fail "AMUX_VERSION contains unsafe characters: $version" ;;
	esac
	archive_name="amux-${version}-${os}-${arch}.tar.gz"
	download_base="$repository/releases/download/$version"
else
	archive_name="amux-${os}-${arch}.tar.gz"
	download_base="$repository/releases/latest/download"
fi
checksum_name="$archive_name.sha256"
archive_dir=${archive_name%.tar.gz}

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/amux-install.XXXXXX") || fail 'could not create a temporary directory'
archive="$work_dir/$archive_name"
checksum="$work_dir/$checksum_name"

download() {
	url=$1
	destination=$2
	curl -fL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 120 \
		--proto '=https' --tlsv1.2 -o "$destination" "$url" || fail "download failed: $url"
}

say "Downloading amux for $os/$arch..."
download "$download_base/$archive_name" "$archive"
download "$download_base/$checksum_name" "$checksum"

expected=$(awk -v asset="$archive_name" '
	NF != 2 { exit 2 }
	{
		count++
		name = $2
		sub(/^\*/, "", name)
		if (name != asset || length($1) != 64 || $1 !~ /^[0-9A-Fa-f]+$/) exit 2
		print tolower($1)
	}
	END { if (count != 1) exit 2 }
' "$checksum") || fail "published checksum is invalid or is not for $archive_name"
[ -n "$expected" ] || fail "published checksum is empty for $archive_name"

digest_output="$work_dir/archive.sha256"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum "$archive" >"$digest_output" || fail 'sha256sum failed'
elif command -v shasum >/dev/null 2>&1; then
	shasum -a 256 "$archive" >"$digest_output" || fail 'shasum failed'
else
	fail 'SHA-256 verification requires sha256sum or shasum'
fi
actual=$(awk 'NF >= 1 && length($1) == 64 && $1 ~ /^[0-9A-Fa-f]+$/ { print tolower($1); found = 1 } END { if (!found) exit 2 }' "$digest_output") || fail 'SHA-256 tool returned an invalid digest'
[ "$actual" = "$expected" ] || fail "checksum verification failed for $archive_name; the existing installation was not changed"

tar -xzf "$archive" -C "$work_dir" "$archive_dir/amux" || fail "could not extract amux from $archive_name"
candidate="$work_dir/$archive_dir/amux"
[ -f "$candidate" ] && [ -s "$candidate" ] || fail "release archive did not contain a non-empty $archive_dir/amux"

if [ -L "$HOME/.local" ] || [ -L "$install_dir" ]; then
	fail "$install_dir must not contain symlinked .local or bin directories; amux update requires the canonical path to be made of real directories"
fi
mkdir -p "$install_dir" || fail "could not create $install_dir"
if [ -e "$install_path" ] && [ ! -f "$install_path" ]; then
	fail "$install_path exists and is not a regular file"
fi
install_tmp=$(mktemp "$install_dir/.amux-install.XXXXXX") || fail "could not create a temporary file in $install_dir"
cp "$candidate" "$install_tmp" || fail "could not stage the new executable; the existing installation was not changed"
chmod 0755 "$install_tmp" || fail "could not make the staged executable runnable; the existing installation was not changed"
mv -f "$install_tmp" "$install_path" || fail "could not atomically replace $install_path; the existing installation was not changed"
install_tmp=

say "Installed amux at $install_path"

case ":${PATH:-}:" in
	*:"$install_dir":*) path_has_install_dir=true ;;
	*) path_has_install_dir=false ;;
esac

if [ "$path_has_install_dir" = false ]; then
	say ""
	say "Add $install_dir to PATH, then restart your shell:"
	say "  export PATH=\"$install_dir:\$PATH\""
else
	selected=$(command -v amux 2>/dev/null || true)
	if [ -n "$selected" ] && [ "$selected" != "$install_path" ] && ! [ "$selected" -ef "$install_path" ] 2>/dev/null; then
		say ""
		say "Warning: $selected currently shadows $install_path."
		say "Put $install_dir before its directory on PATH or remove the duplicate."
	fi
fi

say ""
say "Next, verify executable selection and installation health:"
say "  $install_path install doctor"
