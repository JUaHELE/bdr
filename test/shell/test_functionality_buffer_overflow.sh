#!/bin/bash

set -eou pipefail

source ../utils/utils.sh

check_root

TMP_DIR=$(mktemp -d)

TEST_NAME="\"Complete functionality basic\""
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
LOOPDEV_JOURNAL_ID=66

CLIENT_PID=""
SERVER_PID=""

CLIENT_BIN="client/client"
SERVER_BIN="server/server"

cleanup() {
    log_info "Cleaning up resources..."

    if [[ -n "${CLIENT_PID:-}" && -e /proc/$CLIENT_PID ]]; then
            log_info "Killing client daemon..."
            kill "$CLIENT_PID" || echo "unable to kill client daemon"

            log_info "Waiting to ensure kill..."
            sleep 5
    fi

    if [[ -n "${SERVER_PID:-}" && -e /proc/$SERVER_PID ]]; then
            log_info "Killing server daemon..."
            kill "$SERVER_PID" || echo "unable to kill server daemon"

            log_info "Waiting to ensure kill..."
            sleep 5
    fi

    log_info "Removing daemon binaries..."
    pushd ../../user/ > /dev/null
    rm $CLIENT_BIN $SERVER_BIN
    popd > /dev/null

    cleanup_targets $TARGET_NAME

    remove_driver

    rm -rf "$TMP_DIR"

    log_info "Resources cleaned up."
}
trap cleanup EXIT

make_driver
load_driver

create_targets 1 $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES

MAPPER_PATH="/dev/mapper/bdr-1"
create_loop_device $LOOPDEV_ID $TARGET_SIZE

create_loop_device $LOOPDEV_JOURNAL_ID $TARGET_SIZE
if [[ ${#LOOP_DEVICES[@]} -ne 3 ]]; then
    error_exit "There should be exactly 3 loop devices, found ${#LOOP_DEVICES[@]}."
fi

CLIENT_ARGS="--control-device /dev/bdr-1 --source-device /dev/mapper/bdr-1 --address 127.0.0.1 --port 9832 --full-scan"
SERVER_ARGS="--target-device ${LOOP_DEVICES[$LOOPDEV_ID]} --port 9832 --address 127.0.0.1 --no-print --journal ${LOOP_DEVICES[$LOOPDEV_JOURNAL_ID]}"

compile_and_start_daemons() {
    log_info "Compiling and starting daemons..."

    pushd ../../user/ > /dev/null

    go build -C client || error_exit "Failed to build client daemon."
    go build -C server || error_exit "Failed to build server daemon."

    log_info "Starting server..."
    ./"$SERVER_BIN" $SERVER_ARGS &
    SERVER_PID=$!
    sleep 2

    log_info "Starting client..."
    ./"$CLIENT_BIN" $CLIENT_ARGS &
    CLIENT_PID=$!
    sleep 2

    popd > /dev/null
}

log_info "Writing test data to $MAPPER_PATH (source device)..."
dd if=/dev/urandom of=$MAPPER_PATH bs=4096 count=2000 conv=fsync &> /dev/null

compile_and_start_daemons

log_info "Testing replication..."

if cmp "$MAPPER_PATH" "${LOOP_DEVICES[$LOOPDEV_ID]}"; then
    log_info "TEST PASSED"
else
    error_exit "TEST FAILED"
fi
