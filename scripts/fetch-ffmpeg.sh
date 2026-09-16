#!/usr/bin/env bash
# Download the pinned, statically linked ffmpeg and ffprobe builds from packaging/ffmpeg.lock.
#
# Usage: scripts/fetch-ffmpeg.sh <out-dir> <os/arch>...
#
# Writes ffmpeg/ffprobe (linux) and ffmpeg.exe/ffprobe.exe (windows) into <out-dir>, next to the
# arxgo binaries, where tool discovery looks first. Every download and every extracted binary is
# checked against its pinned SHA-256; a mismatch fails without touching existing files. Binaries
# that already match their pins are not downloaded again. Needs curl, tar, gzip and unzip.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
lockfile=$root/packaging/ffmpeg.lock

die() {
	echo "fetch-ffmpeg: $*" >&2
	exit 1
}

lock() {
	local v
	v=$(sed -n "s/^$1=//p" "$lockfile")
	[ -n "$v" ] || die "key $1 missing in $lockfile"
	printf '%s' "$v"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

matches() { # file lock-key
	[ -f "$1" ] && [ "$(sha256 "$1")" = "$(lock "$2")" ]
}

verify() { # file lock-key
	matches "$1" "$2" || die "checksum mismatch for $(basename "$1"): got $(sha256 "$1"), want $(lock "$2")"
}

fetch() { # url output [curl args...]
	local url=$1 out=$2
	shift 2
	echo "download $url"
	curl -fsSL --retry 3 "$@" -o "$out" "$url"
}

fetch_linux_amd64() { # work-dir
	local repo layer token
	repo=$(lock linux_amd64_registry_repo)
	layer=$(lock linux_amd64_layer_sha256)
	token=$(curl -fsSL --retry 3 \
		"https://auth.docker.io/token?service=registry.docker.io&scope=repository:$repo:pull" |
		sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
	[ -n "$token" ] || die "no Docker Hub pull token for $repo"
	fetch "https://registry-1.docker.io/v2/$repo/blobs/sha256:$layer" "$1/layer.tar.gz" \
		-H "Authorization: Bearer $token"
	verify "$1/layer.tar.gz" linux_amd64_layer_sha256
	tar -xzf "$1/layer.tar.gz" -C "$1/out" ffmpeg ffprobe
}

fetch_windows_amd64() { # work-dir
	local dir
	dir=$(lock windows_amd64_archive_dir)
	fetch "$(lock windows_amd64_url)" "$1/archive.zip"
	verify "$1/archive.zip" windows_amd64_archive_sha256
	unzip -q -o -j "$1/archive.zip" "$dir/ffmpeg.exe" "$dir/ffprobe.exe" -d "$1/out"
}

[ $# -ge 2 ] || die "usage: $0 <out-dir> <os/arch>..."
out=$1
shift
for tool in curl tar gzip unzip; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done
mkdir -p "$out"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "ffmpeg $(lock version) ($(lock license))"
for platform in "$@"; do
	key=${platform/\//_}
	ext=
	[ "${platform%/*}" = windows ] && ext=.exe
	declare -F "fetch_$key" >/dev/null || die "no pinned ffmpeg build for $platform"

	if matches "$out/ffmpeg$ext" "${key}_ffmpeg_sha256" &&
		matches "$out/ffprobe$ext" "${key}_ffprobe_sha256"; then
		echo "up to date $out/ffmpeg$ext $out/ffprobe$ext ($platform)"
		continue
	fi

	rm -rf "${work:?}/$key"
	mkdir -p "$work/$key/out"
	"fetch_$key" "$work/$key"
	for bin in ffmpeg ffprobe; do
		verify "$work/$key/out/$bin$ext" "${key}_${bin}_sha256"
		chmod 0755 "$work/$key/out/$bin$ext"
	done
	for bin in ffmpeg ffprobe; do
		mv -f "$work/$key/out/$bin$ext" "$out/$bin$ext"
		echo "wrote $out/$bin$ext ($platform)"
	done
	rm -rf "${work:?}/$key"
done
