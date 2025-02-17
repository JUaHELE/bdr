#!/usr/bin/env bats

# Load test utilities
load '../utils/utils.sh'

# Global variables
TARGETS_CREATED=5
TARGET_SIZE=10 # MB
BUFFER_SIZE_IN_WRITES=1024
TARGET_NAME="bdr"
START_SUFFIX_PATH="bats"

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
	if [[ "${NO_CLEANUP:-false}" == true ]]; then
		echo "Cleanup disabled, skipping resource cleanup."
		return
	fi
	
	echo "Cleaning up resources..."
	sleep "${WAIT_TIME:-0}"
	
	# Explicitly clean up any remaining targets and loop devices
	for i in $(seq 1 $TARGETS_CREATED); do
		if ls /dev/mapper/$TARGET_NAME-$i &>/dev/null; then
			sudo dmsetup remove $TARGET_NAME-$i
		fi
	done
	
	# Clean up all loop devices
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
}

# Helper function to check path
check_test_path() {
	if ! check_correct_path "$START_SUFFIX_PATH"; then
		echo "Executing tests in wrong directory($PWD), please go to test/bats" >&2
		return 1
	fi
	return 0
}

@test "Many targets should be loadable" {
	# Check if we're in the correct directory
	run check_test_path
	[ "$status" -eq 0 ]

	# Create targets
	run create_targets $TARGETS_CREATED $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES
	[ "$status" -eq 0 ]
	
	# Verify all targets were created
	for i in $(seq 1 $TARGETS_CREATED); do
		[ -e "/dev/mapper/$TARGET_NAME-$i" ]
	done
}

# Help function (can be called via bats-file --help)
help() {
	cat << EOF
Usage: bats $BATS_TEST_FILENAME [OPTIONS]
A test script for creating and managing multiple BDR targets.

Options:
  NO_CLEANUP=true	Skip cleanup of resources after test completion
  WAIT_TIME=N		Optional delay (in seconds) before cleanup (default: 0)

The script must be run from a directory ending in '$START_SUFFIX_PATH'
EOF
}
