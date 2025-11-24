#!/bin/bash
# Reset all test environments to old versions for testing
# This script downgrades containers so Docksmith will detect updates

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

print_header "Resetting Integration Test Environments"

# Array of environments to reset
ENVIRONMENTS=(
    "basic-compose"
    "include-compose"
    "multi-stack"
    "constraints"
    "labels"
)

# Function to reset a specific environment
reset_env() {
    local env_name="$1"
    local env_path="/home/chis/www/docksmith/test/integration/environments/$env_name"

    print_info "Resetting environment: $env_name"

    if [ ! -d "$env_path" ]; then
        print_error "Environment not found: $env_path"
        return 1
    fi

    cd "$env_path"

    # Stop containers
    print_info "  Stopping containers..."
    docker compose down 2>/dev/null || true

    # Restore all compose files from backups (undo any test modifications)
    print_info "  Restoring compose files from backups..."
    for backup in *.backup; do
        if [ -f "$backup" ]; then
            original="${backup%.backup}"
            cp "$backup" "$original"
        fi
    done

    # Pull old images to ensure they're available
    print_info "  Pulling old images..."
    docker compose pull --quiet

    # Start with old versions
    print_info "  Starting containers with old versions..."
    docker compose up -d

    # Wait a bit for containers to start
    sleep 5

    print_success "  Environment $env_name reset complete"

    cd - > /dev/null
}

# Main execution
main() {
    local specific_env="$1"

    if [ -n "$specific_env" ]; then
        # Reset specific environment
        reset_env "$specific_env"
    else
        # Reset all environments
        for env in "${ENVIRONMENTS[@]}"; do
            reset_env "$env"
            echo ""
        done
    fi

    print_success "All environments reset to old versions"
    print_info "Containers are ready for update testing"
}

# Show usage if --help
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    echo "Usage: $0 [environment-name]"
    echo ""
    echo "Reset test environments to old versions for Docksmith testing"
    echo ""
    echo "Available environments:"
    for env in "${ENVIRONMENTS[@]}"; do
        echo "  - $env"
    done
    echo ""
    echo "Examples:"
    echo "  $0                    # Reset all environments"
    echo "  $0 basic-compose      # Reset only basic-compose"
    exit 0
fi

main "$@"
