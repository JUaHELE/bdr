#!/bin/sh
set -eu

dir=$(dirname "$(realpath "$0")")

usage() {
	cat <<EOF
Usage: run-qemu.sh [-h] NAME

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

qemu-system-x86_64 \
	-enable-kvm \
	-m 2G \
	-kernel "$dir/FIXME" \
	-drive "file=$name.qcow2"
	-smp cpus=4
