#!/bin/sh
set -eu

version="${VERSION:?set VERSION to the release version without the leading v}"
input_dir="${INPUT_DIR:-dist}"
output_dir="${OUTPUT_ROOT:-release}/${version}"

mkdir -p "$output_dir"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    binary="$input_dir/${target_os}-${target_arch}/pool-skimmer"
    archive="pool-skimmer_${version}_${target_os}_${target_arch}.tar.gz"

    if [ ! -x "$binary" ]; then
        echo "missing executable release binary: $binary" >&2
        exit 1
    fi

    tar -C "${binary%/*}" -czf "$output_dir/$archive" pool-skimmer
done

if command -v sha256sum >/dev/null 2>&1; then
    (
        cd "$output_dir"
        sha256sum pool-skimmer_*.tar.gz > SHA256SUMS
    )
else
    (
        cd "$output_dir"
        shasum -a 256 pool-skimmer_*.tar.gz > SHA256SUMS
    )
fi

echo "Packaged release artifacts in $output_dir/"
