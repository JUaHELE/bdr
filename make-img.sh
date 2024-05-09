#!/bin/sh
set -eu

tgz_url=https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/x86_64/alpine-minirootfs-3.19.1-x86_64.tar.gz
tgz_sha="185123ceb6e7d08f2449fff5543db206ffb79decd814608d399ad447e08fa29e"

tmpdir=$(mktemp -d)

rm -f -- rootfs.img
fallocate -l 10G rootfs.img
loopdev=$(sudo losetup -f --show rootfs.img)

cleanup() {
	sudo umount -- "$tmpdir"   ||:
	sudo rm -rf -- "$tmpdir"   ||:
	sudo losetup -d "$loopdev" ||:
}

trap cleanup EXIT INT QUIT TERM

curl \
	-sSfq \
	-o alpine.tgz \
	--etag-save alpine.tgz.etag \
	--etag-compare alpine.tgz.etag \
	"$tgz_url"

sudo mkfs.ext4 "$loopdev"
sudo mount "$loopdev" "$tmpdir"
sudo tar -xzvf alpine.tgz -C "$tmpdir" # TODO: Verify hash

for img in send recv; do
	qemu-img create -f qcow2 -b rootfs.img -F raw "$img.qcow2"
done
