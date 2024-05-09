#!/bin/sh
set -eu

usage() {
	cat <<EOF
Usage: make-img.sh [-h] [-v]

Create Linux rootfs images for send and recv VMs.
Fetches a Linux rootfs tarball (unless already present).
Creates a new raw image and unpacks tarballs into it.
Then creates two qcow2 images for send and recv VMs backed by the raw image.

ANY EXISTING ROOTFS IMAGES WILL BE DESTROYED.

Options:

  -h  Print this message and exit
  -v  set -x
EOF
}

while getopts hv opt; do
	case $opt in
	h) usage; exit 0;;
	v) set -x;;
	*) usage >&2; exit 1;;
	esac
done

shift $((OPTIND - 1))

[ $# -eq 0 ] || {
	usage
	exit 1
} >&2

tgz_url=https://repo-default.voidlinux.org/live/current/void-x86_64-ROOTFS-20240314.tar.xz
archive=rootfs.tar.xz
tgz_sha=FIXME

tmpdir=$(mktemp -d)

rm -f -- rootfs.img
fallocate -l 10G rootfs.img
loopdev=$(sudo losetup -f --show rootfs.img)

cleanup() {
	rm -f "$archive.tmp"
	sudo umount -- "$tmpdir"   ||:
	sudo rm -rf -- "$tmpdir"   ||:
	sudo losetup -d "$loopdev" ||:
}

trap cleanup EXIT INT QUIT TERM

touch "$archive.etag"
curl \
	-f \
	-# \
	-o "$archive" \
	--etag-save "$archive.etag.tmp" \
	--etag-compare "$archive.etag" \
	"$tgz_url"

mv "$archive.etag.tmp" "$archive.etag"

sudo mkfs.ext4 "$loopdev"
sudo mount "$loopdev" "$tmpdir"
sudo tar -xJvf "$archive" -C "$tmpdir" # TODO: Verify hash

for img in send recv; do
	qemu-img create -f qcow2 -b rootfs.img -F raw "$img.qcow2"
done
