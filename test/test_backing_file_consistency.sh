#!/bin/bash

TMP_DIR=$(mktemp -d)

TEST_NAME="\"Backing file consistency\""
START_SUFFIX_PATH="test"

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

cleanup() {
    log_info "Cleaning up resources..."

    cleanup_targets $TARGET_NAME

    rm -rf "$TMP_DIR"

    log_info "Resources cleaned up."
}
trap cleanup EXIT

# create one target
create_targets 1 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES

# create one loop device with number 2
create_loop_device 2 $TARGET_SIZE
