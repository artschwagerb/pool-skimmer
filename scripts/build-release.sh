#!/bin/sh
set -eu

version="${VERSION:-dev}"
go_version="${GO_VERSION:-1.27.1}"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    destination="dist/${target_os}-${target_arch}"
    mkdir -p "$destination"
    docker build \
        --target binary \
        --build-arg "GO_VERSION=${go_version}" \
        --build-arg "TARGETOS=${target_os}" \
        --build-arg "TARGETARCH=${target_arch}" \
        --build-arg "VERSION=${version}" \
        --output "type=local,dest=${destination}" \
        .
done

echo "Built release binaries in dist/"
