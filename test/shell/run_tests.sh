#!/bin/bash

set -eou pipefail

source ../utils/utils.sh

check_root

run_test() {
    local test=$1
    log_info "Running $test..."
    sh $test &> /dev/null
    if [ $? -ne 0 ]; then
        log_info "$test FAILED!"
        return 1
    fi
    log_info "$test PASSED!"

    return 0
}

for test in test_*.sh; do
    run_test $test
done

echo "All scripts completed successfully!"
exit 0
