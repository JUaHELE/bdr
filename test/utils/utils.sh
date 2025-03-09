#!/bin/bash

declare -ga LOOP_DEVICES
TMP_DIR=$(mktemp -d)

SRC_DIR="../../kern/"

# FUNCTIONS ------

log_info() {
	echo "[INFO]: $1"
}

error_exit() {
	echo "[ERROR]: $1" >&2
	exit 1
}

make_driver() {
	log_info "Compiling the kernel driver..."
	make -C "$SRC_DIR" > /dev/null 2>&1 || error_exit "Failed to compile the source directory"
}

load_driver() {
	log_info "Loading the driver into the kernel..."
	if ! lsmod | grep -q bdr; then
		sudo insmod "$SRC_DIR/bdr.ko" || error_exit "Failed to load the kernel driver"
	fi
}

create_loop_device() {
	# $1 should hold a number to index in LOOP_DEVICES
	
	local number="$1"
	local dev_backing_file="$TMP_DIR/loop$number.img"

	# $2 is size of the loop device created
	local dev_size_mb="$2"

	dd if=/dev/zero of="$dev_backing_file" bs=1M count="$dev_size_mb" status=none

	local loop_device=$(sudo losetup --find --show "$dev_backing_file") || error_exit "Can't create loop device on $dev_backing_file."
	LOOP_DEVICES[$number]="$loop_device"

	log_info "Loop device $number created with backing on $dev_backing_file and size $dev_size_mb MB"
}

# $1 is how many loop devices is created, $2 is what size of loop devices, $3 is name of the target $4 is size of the buffer in writes
create_targets() {
	local count="$1"
	local size_mb="$2"
	local target_name="$3"
	local buffer_size_in_writes="$4"

	log_info "Creating $count $target_name targets..."

	for i in $(seq 1 "$count"); do
		create_loop_device $i $size_mb
		local loop_device=${LOOP_DEVICES[$i]}

		local sector_count=$((size_mb * 2048))
		echo "0 $sector_count $target_name $loop_device $target_name-$i $buffer_size_in_writes" | sudo dmsetup create $target_name-$i || error_exit "Can't create target $target_name-$i. Probably name collision."

		local backing_file="$TMP_DIR/loop$i.img"
		log_info "$target_name target created on ${LOOP_DEVICES[$i]}"
	done

	log_info "Targets created."
}

cleanup_loop_device() {
	local loop_dev_name="$1"

	if losetup -l | grep -q "$loop_dev_name"; then
	    sudo losetup -d "$loop_dev_name"
	fi
}

# first argument is name of the targets that are to be removed
cleanup_targets() {
	local target_name="$1"

	log_info "Cleaning up $target_name targets..."

	for i in "${!LOOP_DEVICES[@]}"; do
		local loop_device=${LOOP_DEVICES[$i]}
		
		if ls /dev/mapper/$target_name-$i &>/dev/null; then
		    sudo dmsetup remove $target_name-$i
		fi

		cleanup_loop_device $loop_device
	done

	log_info "Targets cleaned up."
}

check_correct_path() {
    local working_dir="$PWD"
    if [[ "$working_dir" == *"$1" ]]; then
        return 0
    else
        return 1
    fi
}
