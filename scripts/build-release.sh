#!/usr/bin/env bash
set -euo pipefail

version="${1:?usage: scripts/build-release.sh vX.Y.Z}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
	echo "invalid release version: $version (expected vX.Y.Z)" >&2
	exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="$repo_root/dist/$version"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$out_dir"
cd "$repo_root"

assets=()
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	IFS=/ read -r goos goarch <<< "$target"
	name="sessionflow_${version#v}_${goos}_${goarch}"
	stage_dir="$tmp_dir/$name"
	mkdir -p "$stage_dir"
	cp README.md LICENSE "$stage_dir/"

	binary=sessionflow
	if [[ "$goos" == windows ]]; then
		binary=sessionflow.exe
	fi
	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
		go build -trimpath -ldflags='-s -w' -o "$stage_dir/$binary" ./cmd/sessionflow

	if [[ "$goos" == windows ]]; then
		archive="$name.zip"
		(cd "$tmp_dir" && zip -qr "$archive" "$name")
	else
		archive="$name.tar.gz"
		LC_ALL=C tar -C "$tmp_dir" -czf "$tmp_dir/$archive" "$name"
	fi
	mv "$tmp_dir/$archive" "$out_dir/$archive"
	assets+=("$archive")
done

(
	cd "$out_dir"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "${assets[@]}" > SHA256SUMS
	else
		shasum -a 256 "${assets[@]}" > SHA256SUMS
	fi
)

echo "Release assets written to $out_dir"
