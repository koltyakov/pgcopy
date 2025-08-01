#!/bin/bash

# Integration test script for pgcopy
# This script tests basic functionality without requiring actual databases

set -e

echo "Starting pgcopy integration tests..."

# Test 1: Version command
echo "Test 1: Version command"
./bin/pgcopy version
echo "✓ Version command works"

# Test 2: Help command
echo "Test 2: Help command"
./bin/pgcopy --help > /dev/null
echo "✓ Help command works"

# Test 3: Copy command help
echo "Test 3: Copy command help"
./bin/pgcopy copy --help > /dev/null
echo "✓ Copy command help works"

# Test 4: List command help
echo "Test 4: List command help"
./bin/pgcopy list --help > /dev/null
echo "✓ List command help works"

# Test 5: Test invalid configuration (should fail gracefully)
echo "Test 5: Invalid configuration handling"
if ./bin/pgcopy copy 2>/dev/null; then
    echo "✗ Expected error for missing configuration"
    exit 1
else
    echo "✓ Correctly handles missing configuration"
fi

# Test 6: Test configuration validation
echo "Test 6: Configuration validation"
if ./bin/pgcopy copy --source "invalid" --dest "invalid" --parallel 0 2>/dev/null; then
    echo "✗ Expected error for invalid configuration"
    exit 1
else
    echo "✓ Correctly validates configuration"
fi

echo ""
echo "All integration tests passed! 🎉"
echo ""
echo "To test with actual databases, you can use:"
echo "  pgcopy copy --source 'postgres://...' --dest 'postgres://...' --dry-run"
