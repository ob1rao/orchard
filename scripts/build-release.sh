#!/bin/sh
set -eu
version=${1:-dev}
mkdir -p dist
for target in linux/amd64 linux/arm64 linux/armv6 linux/armv7 darwin/amd64 darwin/arm64; do
 os=${target%/*}; arch=${target#*/}; goarch=$arch; goarm=''
 case "$arch" in armv6) goarch=arm; goarm=6 ;; armv7) goarch=arm; goarm=7 ;; esac
 stage=$(mktemp -d)
 CGO_ENABLED=0 GOOS=$os GOARCH=$goarch GOARM=$goarm go build -trimpath -ldflags="-s -w -X main.version=$version" -o "$stage/orchard" ./cmd/orchard
 cp README.md THIRD_PARTY_NOTICES.txt "$stage/"
 cp -R docs "$stage/docs"
 tar -czf "dist/orchard_${os}_${arch}.tar.gz" -C "$stage" orchard README.md THIRD_PARTY_NOTICES.txt docs
 rm -rf "$stage"
done
(cd dist && sha256sum orchard_*.tar.gz > checksums.txt)
