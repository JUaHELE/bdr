#!/usr/bin/env bats

# Load test utilities
load '../utils/utils.sh'

# Global variables
TARGET_SIZE=10 # MB
BUFFER_SIZE_IN_WRITES=1024
TARGET_NAME="bdr"
LOOPDEV_ID=44
START_SUFFIX_PATH="bats"
MAPPER_PATH="/dev/mapper/bdr-1"

# Setup runs before each test
setup() {
	# Save original working directory
	export ORIG_DIR="$PWD"
	
	# Create temp directory for test artifacts
	export TMP_DIR="$(mktemp -d)"
	
	# Initialize LOOP_DEVICES array
	declare -ga LOOP_DEVICES=()
}

# Teardown runs after each test
teardown() {
	echo "Cleaning up resources..."
	
	# Clean up targets
	cleanup_targets $TARGET_NAME
	
	# Ensure loop devices are cleaned up
	for loop_device in "${LOOP_DEVICES[@]}"; do
		if [[ -n "$loop_device" ]] && losetup -l | grep -q "$loop_device"; then
			sudo losetup -d "$loop_device"
		fi
	done
	
	# Clean up temporary directory
	if [[ -d "$TMP_DIR" ]]; then
		rm -rf "$TMP_DIR"
	fi
	
	cd "$ORIG_DIR"
	echo "Resources cleaned up."
}

# Helper function to check path
check_test_path() {
	if ! check_correct_path "$START_SUFFIX_PATH"; then
		echo "Executing tests in wrong directory($PWD), please go to test/bats" >&2
		return 1
	fi
	return 0
}

# Helper function to write test data
write_test_data() {
	# Create random test data
	dd if=/dev/urandom of="${TMP_DIR}/sample_data.bin" bs=1M count=10 status=none
	
	# Copy data to both devices
	cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH"
	cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID]}"
}

@test "Backing file should maintain consistency" {
	# Check if we're in the correct directory
	check_test_path
	
	# Create one target - don't use 'run' here to preserve array
	create_targets 1 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES
	
	# Verify target was created
	[ -e "$MAPPER_PATH" ]

	# Create additional loop device - don't use 'run' here to preserve array
	create_loop_device $LOOPDEV_ID $TARGET_SIZE
	
	# Debug output
	echo "Current LOOP_DEVICES array content:"
	declare -p LOOP_DEVICES
	
	# Verify loop device count
	local device_count="${#LOOP_DEVICES[@]}"
	echo "Number of loop devices: $device_count"
	[ "$device_count" -eq 2 ] || {
		echo "Expected 2 loop devices, but found $device_count"
		ls -l /dev/loop*
		sudo losetup -l
		false
	}
	
	# Write and verify data
	write_test_data
	
	echo "Comparing device contents..."
	cmp -s "$MAPPER_PATH" "${LOOP_DEVICES[$LOOPDEV_ID]}" || {
		echo "Device contents do not match"
		false
	}
}

# Help function
help() {
	cat << EOF
Usage: bats $BATS_TEST_FILENAME

A test script for verifying backing file consistency between devices.
The script must be run from a directory ending in '$START_SUFFIX_PATH'
EOF
}
