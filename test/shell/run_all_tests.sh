#!/bin/bash

source ../utils/utils.sh

check_root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
cd "$SCRIPT_DIR"

TEST_SCRIPTS=$(find . -type f -name "test_*" -executable | sort)

if [ -z "$TEST_SCRIPTS" ]; then
    echo -e "${RED}No test scripts found!${NC}"
    echo "Make sure test scripts have executable permission"
    exit 1
fi

TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

for test in $TEST_SCRIPTS; do
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    TEST_NAME=$(basename "$test")

    echo -e "\n${YELLOW}Running test: ${TEST_NAME}${NC}"
    echo -e "${YELLOW}-----------------------------${NC}"

    $test
    TEST_RESULT=$?

    if [ $TEST_RESULT -eq 0 ]; then
        echo -e "${GREEN}TEST PASSED: ${TEST_NAME}${NC}"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}TEST FAILED: ${TEST_NAME}${NC}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
done

echo -e "n${BLUE}=======================================${NC}"
echo -e "Total tests:  $TOTAL_TESTS"
echo -e "Passed:       ${GREEN}$PASSED_TESTS${NC}"
echo -e "Failed:       ${RED}$FAILED_TESTS${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed successfully!${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed.${NC}"
    exit 1
fi

