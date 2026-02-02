#!/bin/bash
# Constitution Compliance Checker
# Verifies adherence to MoonTrack constitution principles

set -e

echo "🔍 MoonTrack Constitution Compliance Check"
echo "=========================================="
echo ""

BACKEND_DIR="apps/backend"
ERRORS=0
WARNINGS=0

# Change to backend directory
cd "$BACKEND_DIR"

echo "📋 Principle IV: Precision & Immutability"
echo "------------------------------------------"

# Check 1: No float64 for financial amounts
echo -n "✓ Checking for float64 in amount calculations... "
FLOAT_VIOLATIONS=$(grep -r "float64.*amount\|amount.*float64" internal/ --include="*.go" | grep -v "test.go" | grep -v "//.*float64" || true)
if [ -n "$FLOAT_VIOLATIONS" ]; then
    echo "❌ FAILED"
    echo "  Found float64 usage for amounts (should use *big.Int):"
    echo "$FLOAT_VIOLATIONS" | while read line; do echo "    $line"; done
    ERRORS=$((ERRORS + 1))
else
    echo "✅ PASSED"
fi

# Check 2: Verify NUMERIC(78,0) in migrations
echo -n "✓ Checking for NUMERIC(78,0) in migrations... "
NUMERIC_COUNT=$(grep -c "NUMERIC(78,0)" migrations/*.sql 2>/dev/null || echo "0")
if [ "$NUMERIC_COUNT" -lt 1 ]; then
    echo "⚠️  WARNING"
    echo "  No NUMERIC(78,0) columns found in migrations"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED ($NUMERIC_COUNT columns)"
fi

# Check 3: No UPDATE or DELETE on entries table
echo -n "✓ Checking entries table immutability... "
UPDATE_DELETE_VIOLATIONS=$(grep -r "UPDATE entries\|DELETE FROM entries" internal/ --include="*.go" | grep -v "test.go" | grep -v "//.*UPDATE\|//.*DELETE" || true)
if [ -n "$UPDATE_DELETE_VIOLATIONS" ]; then
    echo "❌ FAILED"
    echo "  Found UPDATE/DELETE on entries table (entries must be immutable):"
    echo "$UPDATE_DELETE_VIOLATIONS" | while read line; do echo "    $line"; done
    ERRORS=$((ERRORS + 1))
else
    echo "✅ PASSED"
fi

# Check 4: Verify big.Int usage for amounts
echo -n "✓ Checking for *big.Int usage in domain models... "
BIG_INT_COUNT=$(grep -r "\*big\.Int" internal/core/ledger/domain/ --include="*.go" | wc -l)
if [ "$BIG_INT_COUNT" -lt 1 ]; then
    echo "⚠️  WARNING"
    echo "  No *big.Int usage found in ledger domain"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED ($BIG_INT_COUNT occurrences)"
fi

echo ""
echo "📋 Principle V: Double-Entry Accounting"
echo "----------------------------------------"

# Check 5: Balance verification in ledger service
echo -n "✓ Checking for balance verification logic... "
BALANCE_CHECK=$(grep -r "SUM(debit)\|balance.*debit.*credit" internal/core/ledger/ --include="*.go" | wc -l)
if [ "$BALANCE_CHECK" -lt 1 ]; then
    echo "⚠️  WARNING"
    echo "  No explicit balance verification found"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

echo ""
echo "📋 Security by Design"
echo "---------------------"

# Check 6: No hardcoded secrets
echo -n "✓ Checking for hardcoded secrets... "
SECRET_VIOLATIONS=$(grep -r "password.*=.*\"\|secret.*=.*\"\|api.*key.*=.*\"" internal/ --include="*.go" | grep -v "test.go" | grep -v "example" | grep -v "//.*" || true)
if [ -n "$SECRET_VIOLATIONS" ]; then
    echo "❌ FAILED"
    echo "  Found potential hardcoded secrets:"
    echo "$SECRET_VIOLATIONS" | head -5 | while read line; do echo "    $line"; done
    ERRORS=$((ERRORS + 1))
else
    echo "✅ PASSED"
fi

# Check 7: SQL injection prevention (parameterized queries)
echo -n "✓ Checking for SQL injection risks... "
SQL_CONCAT=$(grep -r "SELECT.*+\|INSERT.*+\|UPDATE.*+\|DELETE.*+" internal/ --include="*.go" | grep -v "test.go" | grep -v "//.*" || true)
if [ -n "$SQL_CONCAT" ]; then
    echo "⚠️  WARNING"
    echo "  Found potential string concatenation in SQL (use parameterized queries):"
    echo "$SQL_CONCAT" | head -3 | while read line; do echo "    $line"; done
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

# Check 8: Environment variable usage for config
echo -n "✓ Checking for environment variable usage... "
ENV_VAR_COUNT=$(grep -r "os\.Getenv\|config\." internal/shared/config/ --include="*.go" | wc -l)
if [ "$ENV_VAR_COUNT" -lt 1 ]; then
    echo "⚠️  WARNING"
    echo "  No environment variable usage found in config"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

echo ""
echo "📋 Test Coverage"
echo "----------------"

# Check 9: Test files exist
echo -n "✓ Checking for test files... "
TEST_COUNT=$(find internal/ -name "*_test.go" | wc -l)
if [ "$TEST_COUNT" -lt 10 ]; then
    echo "⚠️  WARNING"
    echo "  Only $TEST_COUNT test files found"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED ($TEST_COUNT test files)"
fi

echo ""
echo "=========================================="
echo "📊 Summary"
echo "=========================================="
echo "Errors:   $ERRORS ❌"
echo "Warnings: $WARNINGS ⚠️"
echo ""

if [ "$ERRORS" -eq 0 ] && [ "$WARNINGS" -eq 0 ]; then
    echo "✅ All constitution compliance checks passed!"
    exit 0
elif [ "$ERRORS" -eq 0 ]; then
    echo "⚠️  Constitution compliance checks passed with warnings"
    exit 0
else
    echo "❌ Constitution compliance checks failed"
    exit 1
fi
