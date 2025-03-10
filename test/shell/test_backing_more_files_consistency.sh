#!/bin/bash

set -eou pipefail

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

LOOPDEV_ID_ONE=44
LOOPDEV_ID_TWO=66

cleanup() {
    log_info "Cleaning up resources..."

    cleanup_targets $TARGET_NAME

    log_info "Removing device-mapper target..."
    sudo rmmod bdr 2>&1 > /dev/null || true

    rm -rf "$TMP_DIR"

    log_info "Resources cleaned up."
}
trap cleanup EXIT

make_driver
load_driver
# create one target
create_targets 2 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES
# this mapper should be created
MAPPER_PATH_ONE="/dev/mapper/bdr-1"
MAPPER_PATH_TWO="/dev/mapper/bdr-2"

# create one loop device with number 32
create_loop_device $LOOPDEV_ID_ONE $TARGET_SIZE
create_loop_device $LOOPDEV_ID_TWO $TARGET_SIZE

if [[ ${#LOOP_DEVICES[@]} -ne 4 ]]; then
    error_exit "There should be exactly 4 loop devices, found ${#LOOP_DEVICES[@]}."
fi

dd if=/dev/urandom of="${TMP_DIR}/sample_data.bin" bs=1M count=10 status=none

# Copy the same data to both loop device files
cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH_ONE"
cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID_ONE]}"

cp "${TMP_DIR}/sample_data.bin" "$MAPPER_PATH_TWO"
cp "${TMP_DIR}/sample_data.bin" "${LOOP_DEVICES[$LOOPDEV_ID_TWO]}"

log_info "Wrote gibberish on devices. Comparing..."

if cmp -s "$MAPPER_PATH_ONE" "${LOOP_DEVICES[$LOOPDEV_ID_ONE]}" && \
   cmp -s "$MAPPER_PATH_TWO" "${LOOP_DEVICES[$LOOPDEV_ID_TWO]}"; then
    log_info "TEST PASSED"
else
    error_exit "TEST FAILED"
fi
