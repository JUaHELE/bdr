#!/bin/bash


source ../utils/utils.sh

TMP_DIR=$(mktemp -d)

TEST_NAME="\"Backing file consistency\""
START_SUFFIX_PATH="shell"

test_init() {
    if check_correct_path "$START_SUFFIX_PATH"; then
        log_info "Starting test $TEST_NAME"
    else
        error_exit "Executing tests in wrong directory($working_dir), please go to bdr/test"
    fi
}

# MAIN ------

TARGET_SIZE=10 # MB
BUFFER_SIZE_IN_WRITES=1024
TARGET_NAME="bdr"

LOOPDEV_ID=44

cleanup() {
    log_info "Cleaning up resources..."

    cleanup_targets $TARGET_NAME

    rm -rf "$TMP_DIR"

    log_info "Removing device-mapper target..."
    sudo rmmod bdr  &> /dev/null || true

    log_info "Resources cleaned up."
}
trap cleanup EXIT

make_driver
load_driver

# create one target
create_targets 1 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES
# this mapper should be created
MAPPER_PATH="/dev/mapper/bdr-1"

# create one loop device with number 32
create_loop_device $LOOPDEV_ID $TARGET_SIZE

if [[ ${#LOOP_DEVICES[@]} -ne 2 ]]; then
    error_exit "There should be only 2 loop devices, found ${#LOOP_DEVICES[@]}."
fi

dd if=/dev/urandom of="${TMP_DIR}/sample_data.bin" bs=1M count=10 status=none

# Copy the same data to both loop device files
cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH"
cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID]}"

log_info "Wrote gibberish on devices. Comparing..."

if cmp -s "$MAPPER_PATH" "${LOOP_DEVICES[$LOOPDEV_ID]}"; then
    log_info "TEST PASSED"
else
    error_exit "TEST FAILED"
fi
