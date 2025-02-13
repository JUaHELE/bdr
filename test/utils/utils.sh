#!/bin/bash

# CONSTANTS ------

declare -a LOOP_DEVICES
TMP_DIR=$(mktemp -d)


# FUNCTIONS ------

log_info() {
	echo "[INFO]: $1"
}

error_exit() {
	echo "[ERROR]: $1" >&2
	exit 1
}

# $1 is how many loop devices is created, $2 is what size of loop devices, $3 is name of the target $4 is size of the buffer in wtites
create_targets() {
	local count="$1"
	local size_mb="$2"
	local target_name="$3"
	local buffer_size_in_writes="$4"

	log_info "Creating $count $target_name targets..."

	for i in $(seq 1 "$count"); do
		local backing_file="$TMP_DIR/loop$i.img"
		dd if=/dev/zero of="$backing_file" bs=1M count="$size_mb" status=none

		local loop_device=$(sudo losetup --find --show "$backing_file") || error_exit "Can't create loop device on $backing_file."
		LOOP_DEVICES[$i]="$loop_device"

		local sector_count=$((size_mb * 2048))
		echo "0 $sector_count $target_name $loop_device $target_name-$i $buffer_size_in_writes" | sudo dmsetup create $target_name-$i || error_exit "Can't create target $target_name-$i. Probably name collision."

		log_info "$target_name target created with size $size_mb MB on $loop_device with backing $backing_file."
	done

	log_info "Targets created."
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

		# Detach loop device
		if losetup -l | grep -q "$loop_device"; then
		    sudo losetup -d "$loop_device"
		fi
	done

	log_info "Targets cleaned up."
}
