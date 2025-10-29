#!/bin/bash
# Check for problematic arrow character usage in markdown
#
# Catches two patterns:
# 1. Arrows used as list bullets (starts line with →)
# 2. Multiple arrows in list items (confusing flow notation)

set -e

echo "Checking for arrow-based formatting issues in markdown files..."

issues_found=0

for file in README.md docs/**/*.md; do
    if [ ! -f "$file" ]; then
        continue
    fi

    # Pattern 1: Lines starting with whitespace + arrow (pseudo-list bullets)
    arrow_bullets=$(grep -n "^[[:space:]]*→" "$file" 2>/dev/null || true)

    if [ -n "$arrow_bullets" ]; then
        echo "❌ Found arrow-based list bullets in $file:"
        echo "$arrow_bullets"
        echo ""
        issues_found=$((issues_found + 1))
    fi

    # Pattern 2: List items with multiple arrows (confusing flow notation)
    # This catches: "- From X → press Y → returns to Z"
    multi_arrow_lists=$(grep -n "^[[:space:]]*-.*→.*→" "$file" 2>/dev/null || true)

    if [ -n "$multi_arrow_lists" ]; then
        echo "❌ Found list items with multiple arrows in $file:"
        echo "$multi_arrow_lists"
        echo ""
        echo "Hint: Use 'press X to Y' instead of 'X → Y → Z'"
        issues_found=$((issues_found + 1))
    fi
done

if [ $issues_found -eq 0 ]; then
    echo "✅ No arrow formatting issues found"
    exit 0
else
    echo ""
    echo "❌ Found arrow formatting issues in $issues_found file(s)"
    echo ""
    echo "Common fixes:"
    echo "1. For pseudo-bullets:"
    echo "   **Heading**"
    echo "   → item          ❌ WRONG"
    echo "   Should be:"
    echo "   ### Heading"
    echo "   - item          ✅ CORRECT"
    echo ""
    echo "2. For flow notation with multiple arrows:"
    echo "   - From X → press Y → returns to Z    ❌ CONFUSING"
    echo "   Should be:"
    echo "   - From X, press Y to return to Z     ✅ CLEAR"
    exit 1
fi
