#!/usr/bin/env bats

# Load test utilities
load '../utils/utils.sh'

# Global variables
TARGET_SIZE=10 # MB
BUFFER_SIZE_IN_WRITES=1024
TARGET_NAME="bdr"
LOOPDEV_ID_ONE=44
LOOPDEV_ID_TWO=66
START_SUFFIX_PATH="bats"
MAPPER_PATH_ONE="/dev/mapper/bdr-1"
MAPPER_PATH_TWO="/dev/mapper/bdr-2"

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
    
    # Copy data to all devices
    cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH_ONE"
    cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID_ONE]}"
    cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH_TWO"
    cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID_TWO]}"
}

@test "Backing file should maintain consistency across multiple devices" {
    # Check if we're in the correct directory
    check_test_path
    
    # Create two targets
    create_targets 2 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES
    
    # Verify targets were created
    [ -e "$MAPPER_PATH_ONE" ]
    [ -e "$MAPPER_PATH_TWO" ]

    # Create additional loop devices
    create_loop_device $LOOPDEV_ID_ONE $TARGET_SIZE
    create_loop_device $LOOPDEV_ID_TWO $TARGET_SIZE
    
    # Debug output
    echo "Current LOOP_DEVICES array content:"
    declare -p LOOP_DEVICES
    
    # Verify loop device count
    local device_count="${#LOOP_DEVICES[@]}"
    echo "Number of loop devices: $device_count"
    [ "$device_count" -eq 4 ] || {
        echo "Expected 4 loop devices, but found $device_count"
        ls -l /dev/loop*
        sudo losetup -l
        false
    }
    
    # Write and verify data
    write_test_data
    
    echo "Comparing device contents..."
    # Check first device pair
    cmp -s "$MAPPER_PATH_ONE" "${LOOP_DEVICES[$LOOPDEV_ID_ONE]}" || {
        echo "Device contents do not match for first pair"
        false
    }
    
    # Check second device pair
    cmp -s "$MAPPER_PATH_TWO" "${LOOP_DEVICES[$LOOPDEV_ID_TWO]}" || {
        echo "Device contents do not match for second pair"
        false
    }
}

# Help function
help() {
    cat << EOF
Usage: bats $BATS_TEST_FILENAME

A test script for verifying backing file consistency between multiple devices.
The script must be run from a directory ending in '$START_SUFFIX_PATH'
EOF
}
