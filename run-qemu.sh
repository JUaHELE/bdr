#!/bin/sh
set -eu

dir=$(dirname "$(realpath "$0")")

usage() {
	cat <<EOF
Usage: run-qemu.sh [-h] [-v] NAME

Launch the VM called NAME.
NAME must be either "send" or "recv".

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

[ $# -eq 1 ] || {
	usage
	exit 1
} >&2

name=$1

case $name in
send) ;;
recv) ;;
*)
	printf >&2 "No VM called %s\n" "$name"
	exit 1
esac

kernel=$dir/linux/arch/x86/boot/bzImage
shared_dir=$dir/shared/$name
qcow2_img=$dir/$name.qcow2

mkdir -p -- "$shared_dir"

qemu-system-x86_64 \
	-m 2G \
	-smp cpus=4 \
	-enable-kvm \
	-kernel "$kernel" \
	-append "root=/dev/sda" \
	-drive "file=$qcow2_img,format=qcow2" \
	-fsdev local,id=fsdev0,path="$shared_dir",security_model=mapped \
	-device virtio-9p-pci,id=fs0,fsdev=fsdev0,mount_tag=hostshare
