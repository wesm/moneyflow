#!/bin/bash
# Check for arrow characters (→) used as list bullets instead of proper markdown lists
# This catches formatting like:
#   **Heading**
#   → bullet point
#   → another point
# Which should be:
#   ### Heading
#   - bullet point
#   - another point

set -e

echo "Checking for arrow-based pseudo-lists in markdown files..."

# Search for lines that start with arrow (with optional whitespace)
issues_found=0

for file in README.md docs/**/*.md; do
    if [ ! -f "$file" ]; then
        continue
    fi

    # Find lines starting with whitespace + arrow
    matches=$(grep -n "^[[:space:]]*→" "$file" 2>/dev/null || true)

    if [ -n "$matches" ]; then
        echo "❌ Found arrow-based list in $file:"
        echo "$matches"
        echo ""
        issues_found=$((issues_found + 1))
    fi
done

if [ $issues_found -eq 0 ]; then
    echo "✅ No arrow-based pseudo-lists found"
    exit 0
else
    echo "❌ Found arrow-based lists in $issues_found file(s)"
    echo ""
    echo "Fix: Replace arrows (→) with proper markdown list syntax (-)"
    echo "Example:"
    echo "  **Heading**"
    echo "  → item          ❌ WRONG"
    echo ""
    echo "  ### Heading"
    echo "  - item          ✅ CORRECT"
    exit 1
fi
