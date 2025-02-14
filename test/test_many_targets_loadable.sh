#!/bin/bash
# INIT ------
set -eou pipefail

source ./utils/utils.sh

TEST_NAME="\"Many targets loadable\""
START_SUFFIX_PATH="test"
HELP_MESSAGE="Usage: $(basename "$0") [OPTIONS] [WAIT_TIME]

A test script for creating and managing multiple BDR targets.

Options:
  --no-cleanup    Skip cleanup of resources after test completion
  -h, --help      Display this help message and exit

Arguments:
  WAIT_TIME       Optional delay (in seconds) before cleanup (default: 0)

The script must be run from a directory ending in '$START_SUFFIX_PATH'"

WAIT_TIME=0
NO_CLEANUP=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --no-cleanup)
            NO_CLEANUP=true
            shift
            ;;
        -h|--help)
            echo "$HELP_MESSAGE"
            exit 0
            ;;
        *)
            # Assume it's the wait time if it's a number
            if [[ $1 =~ ^[0-9]+$ ]]; then
                WAIT_TIME=$1
            else
                echo "[ERROR]: Unknown option '$1'"
                echo "$HELP_MESSAGE"
                exit 1
            fi
            shift
            ;;
    esac
done


test_init() {
    if check_correct_path "$START_SUFFIX_PATH"; then
        log_info "Starting test $TEST_NAME"
    else
        error_exit "Executing tests in wrong directory($working_dir), please go to bdr/test"
    fi
}

test_init

# MAIN ------

TARGETS_CREATED=5
TARGET_SIZE=10 # MB
BUFFER_SIZE_IN_WRITES=1024
TARGET_NAME="bdr"

cleanup() {
	if [[ "$NO_CLEANUP" == true ]]; then
		log_info "Cleanup disabled, skipping resource cleanup."
		return
	fi

	log_info "Cleaning up resources..."
	sleep "$WAIT_TIME"

	cleanup_targets $TARGET_NAME

	rm -rf "$TMP_DIR"

	log_info "Resources cleaned up."
}
trap cleanup EXIT

create_targets $TARGETS_CREATED $TARGET_SIZE $TARGET_NAME $BUFFER_SIZE_IN_WRITES

log_info "PASSED"
