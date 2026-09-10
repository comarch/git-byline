#!/bin/sh

set -eu

repository="https://github.com/comarch/git-byline"
version="${GIT_BYLINE_VERSION:-latest}"
bin_dir="${GIT_BYLINE_BIN_DIR:-$HOME/.local/bin}"
install_git_hook="${GIT_BYLINE_INSTALL_GIT_HOOK:-1}"

usage() {
	cat <<'EOF'
Usage: install.sh [--version VERSION] [--bin-dir DIR] [--no-git-hook]

Install a checksum-verified git-byline release for Linux or macOS.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version)
		[ "$#" -ge 2 ] || {
			printf '%s\n' "install.sh: --version requires a value" >&2
			exit 2
		}
		version="$2"
		shift 2
		;;
	--bin-dir)
		[ "$#" -ge 2 ] || {
			printf '%s\n' "install.sh: --bin-dir requires a value" >&2
			exit 2
		}
		bin_dir="$2"
		shift 2
		;;
	--no-git-hook)
		install_git_hook=0
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'install.sh: unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

for command in curl tar awk grep mktemp sed sort; do
	command -v "$command" >/dev/null 2>&1 || {
		printf 'install.sh: required command not found: %s\n' "$command" >&2
		exit 1
	}
done

if [ "$version" = "latest" ]; then
	version="$(
		curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
			-o /dev/null -w '%{url_effective}' "$repository/releases/latest"
	)"
	version="${version##*/}"
fi
printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
	printf 'install.sh: unsupported release version: %s\n' "$version" >&2
	exit 1
}

case "$(uname -s)" in
Darwin) os="macOS" ;;
Linux) os="linux" ;;
*)
	printf 'install.sh: unsupported operating system: %s\n' "$(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*)
	printf 'install.sh: unsupported architecture: %s\n' "$(uname -m)" >&2
	exit 1
	;;
esac

release_version="${version#v}"
archive="git-byline_${release_version}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/git-byline.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
	-o "$tmp/$archive" "$repository/releases/download/$version/$archive"
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
	-o "$tmp/checksums.txt" "$repository/releases/download/$version/checksums.txt"

expected="$(awk -v name="$archive" '$2 == name || $2 == "*" name { print $1 }' "$tmp/checksums.txt")"
printf '%s' "$expected" | grep -Eq '^[0-9a-fA-F]{64}$' || {
	printf 'install.sh: missing or invalid checksum for %s\n' "$archive" >&2
	exit 1
}
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
else
	printf '%s\n' "install.sh: sha256sum or shasum is required" >&2
	exit 1
fi
[ "$actual" = "$expected" ] || {
	printf 'install.sh: checksum mismatch for %s\n' "$archive" >&2
	exit 1
}

archive_files="$(
	tar -tzf "$tmp/$archive" |
		sed 's#^\./##' |
		# AppleDouble metadata entries (._*) are tar-side effects of building
		# on macOS; they carry no release content and are never extracted.
		grep -v -E '^\._' |
		grep -v '/$' |
		sort
)"
expected_files="$(printf '%s\n' LICENSE README.md SECURITY.md git-byline | sort)"
[ "$archive_files" = "$expected_files" ] || {
	printf 'install.sh: unexpected files in %s\n' "$archive" >&2
	exit 1
}
case "$(tar -tvzf "$tmp/$archive" git-byline)" in
-*) ;;
*)
	printf 'install.sh: git-byline is not a regular archive file\n' >&2
	exit 1
	;;
esac

tar -xzf "$tmp/$archive" -C "$tmp" git-byline
chmod 0755 "$tmp/git-byline"
[ "$("$tmp/git-byline" version)" = "git-byline $version" ] || {
	printf '%s\n' "install.sh: binary version does not match release" >&2
	exit 1
}

mkdir -p "$bin_dir"
target="$bin_dir/git-byline"
[ ! -d "$target" ] || {
	printf 'install.sh: destination is a directory: %s\n' "$target" >&2
	exit 1
}
staged="$(mktemp "$bin_dir/.git-byline.XXXXXX")"
trap 'rm -rf "$tmp"; rm -f "$staged"' EXIT HUP INT TERM
cp "$tmp/git-byline" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$target"

if [ "$install_git_hook" != "0" ] &&
	command -v git >/dev/null 2>&1 &&
	git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	"$target" install-hooks --agent none --git --project
fi

printf 'Installed git-byline %s to %s\n' "$version" "$target"
case ":$PATH:" in
*":$bin_dir:"*) ;;
*) printf 'Add %s to PATH before starting an AI agent.\n' "$bin_dir" ;;
esac
