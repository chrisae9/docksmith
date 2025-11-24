#!/bin/bash
# Main test runner for Docksmith integration tests
# Each test script is self-contained and manages its own setup/cleanup

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

# Show help
show_help() {
    echo "Usage: $0 [TEST...]"
    echo ""
    echo "Run Docksmith integration tests"
    echo ""
    echo "Arguments:"
    echo "  api          Run API tests only"
    echo "  labels       Run label tests only"
    echo "  constraints  Run constraint tests only"
    echo "  all          Run all tests (default)"
    echo ""
    echo "Examples:"
    echo "  $0                    # Run all tests"
    echo "  $0 all                # Run all tests"
    echo "  $0 api                # Run only API tests"
    echo "  $0 labels constraints # Run label and constraint tests"
    exit 0
}

# Parse arguments
RUN_API=false
RUN_LABELS=false
RUN_CONSTRAINTS=false

if [ $# -eq 0 ]; then
    # No args = run all
    RUN_API=true
    RUN_LABELS=true
    RUN_CONSTRAINTS=true
else
    for arg in "$@"; do
        case $arg in
            api)
                RUN_API=true
                ;;
            labels)
                RUN_LABELS=true
                ;;
            constraints)
                RUN_CONSTRAINTS=true
                ;;
            all)
                RUN_API=true
                RUN_LABELS=true
                RUN_CONSTRAINTS=true
                ;;
            --help|-h|help)
                show_help
                ;;
            *)
                print_error "Unknown test: $arg"
                echo "Use --help for usage information"
                exit 1
                ;;
        esac
    done
fi

# Main execution
main() {
    print_header "Docksmith Integration Test Suite"

    # Check Docksmith is running
    if ! check_docksmith; then
        print_error "Docksmith is not running. Please start Docksmith first."
        exit 1
    fi

    # Track results
    local total_suites=0
    local passed_suites=0
    local failed_suites=0

    # Run API tests
    if [ "$RUN_API" = true ]; then
        total_suites=$((total_suites + 1))
        echo ""
        if "$SCRIPT_DIR/test-api.sh"; then
            passed_suites=$((passed_suites + 1))
        else
            failed_suites=$((failed_suites + 1))
        fi
    fi

    # Run label tests
    if [ "$RUN_LABELS" = true ]; then
        total_suites=$((total_suites + 1))
        echo ""
        if "$SCRIPT_DIR/test-labels.sh"; then
            passed_suites=$((passed_suites + 1))
        else
            failed_suites=$((failed_suites + 1))
        fi
    fi

    # Run constraint tests
    if [ "$RUN_CONSTRAINTS" = true ]; then
        total_suites=$((total_suites + 1))
        echo ""
        if "$SCRIPT_DIR/test-constraints.sh"; then
            passed_suites=$((passed_suites + 1))
        else
            failed_suites=$((failed_suites + 1))
        fi
    fi

    # Print overall summary
    echo ""
    print_header "OVERALL TEST SUMMARY"
    echo "Test Suites Run: $total_suites"
    echo -e "${GREEN}Passed: $passed_suites${NC}"
    echo -e "${RED}Failed: $failed_suites${NC}"
    echo ""

    if [ $failed_suites -eq 0 ]; then
        print_success "ALL TEST SUITES PASSED!"
        return 0
    else
        print_error "SOME TEST SUITES FAILED"
        return 1
    fi
}

main "$@"
