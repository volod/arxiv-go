#!/usr/bin/env bash
# Assemble the two allowlisted release bundles from already built, pinned tools.
# Usage: scripts/package-dist.sh <version> <bin-dir> <dist-dir> [ffmpeg-lock]
# The optional lock is for network-free fixture tests; make dist uses the pinned lock.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)

die() {
	echo "package-dist: $*" >&2
	exit 1
}

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
	die "usage: $0 <version> <bin-dir> <dist-dir> [ffmpeg-lock]"
fi
version=$1
bin=$2
dist=$3
lockfile=${4:-$root/packaging/ffmpeg.lock}
[[ $version =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || die "invalid version: $version"

for tool in tar zip sha256sum; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

lock() {
	local value
	value=$(sed -n "s/^$1=//p" "$lockfile")
	[ -n "$value" ] || die "missing $1 in $lockfile"
	printf '%s' "$value"
}

require_regular() {
	if [ ! -f "$1" ] || [ -L "$1" ]; then
		die "missing regular file: $1"
	fi
}

verify_pin() {
	local actual expected
	require_regular "$1"
	actual=$(sha256sum "$1")
	actual=${actual%% *}
	expected=$(lock "$2")
	[ "$actual" = "$expected" ] || die "checksum mismatch for $1: got $actual, want $expected"
}

for platform in linux windows; do
	ext=
	[ "$platform" = windows ] && ext=.exe
	require_regular "$bin/arxgo$ext"
	[ -x "$bin/arxgo$ext" ] || die "not executable: $bin/arxgo$ext"
	for tool in ffmpeg ffprobe; do
		verify_pin "$bin/$tool$ext" "${platform}_amd64_${tool}_sha256"
		[ -x "$bin/$tool$ext" ] || die "not executable: $bin/$tool$ext"
	done
	require_regular "$root/docs/guide/manual-$platform.md"
	require_regular "$root/packaging/LICENSES/FFmpeg-SOURCE-$platform.txt"
done
require_regular "$root/.env.example"
require_regular "$root/LICENSE"
require_regular "$root/packaging/LICENSES/GPL-3.0.txt"

mkdir -p "$dist"
dist=$(cd "$dist" && pwd)
work=$(mktemp -d "$dist/.package-dist.XXXXXXXX")
trap 'rm -rf "$work"' EXIT

for platform in linux windows; do
	ext=
	[ "$platform" = windows ] && ext=.exe
	stage=$work/$platform
	mkdir -p "$stage/LICENSES"
	cp "$bin/arxgo$ext" "$bin/ffmpeg$ext" "$bin/ffprobe$ext" "$stage/"
	cp "$root/.env.example" "$stage/.env.example"
	cp "$root/docs/guide/manual-$platform.md" "$stage/"
	cp "$root/LICENSE" "$stage/LICENSES/arxgo-MIT.txt"
	cp "$root/packaging/LICENSES/GPL-3.0.txt" "$stage/LICENSES/GPL-3.0.txt"
	cp "$root/packaging/LICENSES/FFmpeg-SOURCE-$platform.txt" "$stage/LICENSES/FFmpeg-SOURCE.txt"
	(
		cd "$stage"
		sha256sum "arxgo$ext" "ffmpeg$ext" "ffprobe$ext" .env.example \
			"manual-$platform.md" LICENSES/arxgo-MIT.txt \
			LICENSES/GPL-3.0.txt LICENSES/FFmpeg-SOURCE.txt > SHA256SUMS
		sha256sum -c SHA256SUMS >/dev/null
	)
	[ ! -e "$stage/.env" ] || die "private .env reached $stage"
	archive="arxgo-$version-$platform-amd64"
	if [ "$platform" = linux ]; then
		tar -czf "$work/$archive.tar.gz" -C "$stage" .
	else
		(cd "$stage" && zip -q -r "$work/$archive.zip" .)
	fi
done

for extension in linux-amd64.tar.gz windows-amd64.zip; do
	mv -f "$work/arxgo-$version-$extension" "$dist/"
done
(
	cd "$dist"
	sha256sum "arxgo-$version-linux-amd64.tar.gz" \
		"arxgo-$version-windows-amd64.zip" > "$work/SHA256SUMS"
)
mv -f "$work/SHA256SUMS" "$dist/SHA256SUMS"
echo "release bundles and SHA256SUMS written to $dist"
