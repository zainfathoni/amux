#!/bin/sh
set -eu

repository=https://github.com/zainfathoni/amux
install_dir=${HOME:+"$HOME/.local/bin"}
install_path=${install_dir:+"$install_dir/amux"}
skills_source=${AMUX_SKILLS_SOURCE:-}
skills_root=${HOME:+"$HOME/.agents/skills"}
skills_backup_suffix=
work_dir=
install_tmp=

say() {
	printf '%s\n' "$*"
}

fail() {
	printf 'amux installer: %s\n' "$*" >&2
	exit 1
}

preflight_mutation_directory() {
	directory=$1
	if [ -L "$directory" ]; then
		fail "managed path must not contain symlinked directories: $directory"
	fi
	if [ -e "$directory" ] && [ ! -d "$directory" ]; then
		fail "managed directory path exists and is not a directory: $directory"
	fi
	if [ -d "$directory" ] && { [ ! -w "$directory" ] || [ ! -x "$directory" ]; }; then
		fail "managed directory is not writable and searchable: $directory"
	fi
}

paths_overlap() {
	left=$1
	right=$2
	if [ "$left" = / ] || [ "$right" = / ]; then
		return 0
	fi
	case "$left" in
		"$right" | "$right"/*) return 0 ;;
	esac
	case "$right" in
		"$left" | "$left"/*) return 0 ;;
	esac
	return 1
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
[ -d "$HOME" ] || fail "HOME is not a directory: $HOME"
canonical_home=$(CDPATH= cd -- "$HOME" 2>/dev/null && pwd -P) || fail "could not canonicalize HOME: $HOME"
for managed_directory in "$HOME" "$HOME/.local" "$install_dir"; do
	preflight_mutation_directory "$managed_directory"
done

if [ -n "$skills_source" ]; then
	for managed_directory in "$HOME/.agents" "$skills_root"; do
		preflight_mutation_directory "$managed_directory"
	done
	for command_name in date ln readlink; do
		command -v "$command_name" >/dev/null 2>&1 || fail "required command not found for AMUX_SKILLS_SOURCE: $command_name"
	done
	case "$skills_source" in
		/*) ;;
		*) fail "AMUX_SKILLS_SOURCE must be an absolute path (got $skills_source)" ;;
	esac
	[ -d "$skills_source" ] || fail "AMUX_SKILLS_SOURCE is not a directory: $skills_source"
	canonical_skills_source=$(CDPATH= cd -- "$skills_source" 2>/dev/null && pwd -P) || fail "could not canonicalize AMUX_SKILLS_SOURCE: $skills_source"
	[ "$skills_source" = "$canonical_skills_source" ] || fail "AMUX_SKILLS_SOURCE must be the canonical path $canonical_skills_source"
	for skill_name in amux amux-claude amux-pi; do
		skill_source="$skills_source/skills/$skill_name"
		[ -d "$skill_source" ] || fail "missing bundled skill directory: $skill_source"
		[ -f "$skill_source/SKILL.md" ] && [ -r "$skill_source/SKILL.md" ] || fail "missing readable bundled skill entrypoint: $skill_source/SKILL.md"
	done
	for managed_physical_root in "$canonical_home/.local/bin" "$canonical_home/.agents/skills"; do
		if paths_overlap "$canonical_skills_source" "$managed_physical_root"; then
			fail "AMUX_SKILLS_SOURCE must not overlap a managed install directory: $managed_physical_root"
		fi
	done
	skills_backup_suffix=$(date -u '+%Y%m%dT%H%M%SZ') || fail 'could not create a skill backup timestamp'
	for skill_name in amux amux-claude amux-pi; do
		skill_destination="$skills_root/$skill_name"
		if [ -e "$skill_destination" ] && [ ! -L "$skill_destination" ]; then
			skill_backup="$skill_destination.backup-$skills_backup_suffix"
			[ ! -e "$skill_backup" ] && [ ! -L "$skill_backup" ] || fail "skill backup already exists: $skill_backup"
		fi
		for legacy_root in "$HOME/.config/amp/skills" "$HOME/.config/agents/skills"; do
			legacy_destination="$legacy_root/$skill_name"
			if [ -e "$legacy_destination" ] || [ -L "$legacy_destination" ]; then
				preflight_mutation_directory "$legacy_root"
				legacy_physical_root=$(CDPATH= cd -- "$legacy_root" 2>/dev/null && pwd -P) || fail "could not canonicalize managed legacy skill directory: $legacy_root"
				if paths_overlap "$canonical_skills_source" "$legacy_physical_root"; then
					fail "AMUX_SKILLS_SOURCE must not overlap a managed legacy skill directory: $legacy_physical_root"
				fi
				legacy_backup="$legacy_destination.backup-$skills_backup_suffix"
				[ ! -e "$legacy_backup" ] && [ ! -L "$legacy_backup" ] || fail "skill backup already exists: $legacy_backup"
			fi
		done
	done
fi

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

if [ -n "$skills_source" ]; then
	for managed_directory in "$HOME" "$HOME/.agents" "$skills_root"; do
		preflight_mutation_directory "$managed_directory"
	done
	mkdir -p "$skills_root" || fail "could not create $skills_root"
	for skill_name in amux amux-claude amux-pi; do
		skill_source="$skills_source/skills/$skill_name"
		skill_destination="$skills_root/$skill_name"
		if [ -L "$skill_destination" ] && [ "$(readlink "$skill_destination" 2>/dev/null || true)" = "$skill_source" ]; then
			say "Skill $skill_name already links to $skill_source"
		else
			if [ -L "$skill_destination" ]; then
				rm -f "$skill_destination" || fail "could not replace skill symlink $skill_destination"
			elif [ -e "$skill_destination" ]; then
				skill_backup="$skill_destination.backup-$skills_backup_suffix"
				mv "$skill_destination" "$skill_backup" || fail "could not preserve existing skill at $skill_backup"
				say "Preserved existing skill at $skill_backup"
			fi
			ln -s "$skill_source" "$skill_destination" || fail "could not link $skill_destination to $skill_source"
			say "Linked skill $skill_name to $skill_source"
		fi
		for legacy_root in "$HOME/.config/amp/skills" "$HOME/.config/agents/skills"; do
			legacy_destination="$legacy_root/$skill_name"
			if [ -e "$legacy_destination" ] || [ -L "$legacy_destination" ]; then
				legacy_backup="$legacy_destination.backup-$skills_backup_suffix"
				mv "$legacy_destination" "$legacy_backup" || fail "could not preserve duplicate skill at $legacy_backup"
				say "Preserved duplicate skill at $legacy_backup"
			fi
		done
	done
	say "Linked bundled skills from $skills_source"
	say "Reload Amp or start a new thread after the source checkout changes."
fi

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
