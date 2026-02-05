#!/bin/bash
# Security Hardening Verification Script
# Checks for common security issues

set -e

echo "🔒 MoonTrack Security Check"
echo "============================"
echo ""

BACKEND_DIR="apps/backend"
ERRORS=0
WARNINGS=0

cd "$BACKEND_DIR"

echo "📋 T173: JWT Secret Verification"
echo "---------------------------------"

# Check 1: JWT secrets not in code
echo -n "✓ Checking for JWT secrets in code... "
JWT_IN_CODE=$(grep -r "jwt.*secret.*=.*\"" internal/ --include="*.go" | grep -v "test" | grep -v "example" | grep -v "//.*" | grep -v "os.Getenv" || true)
if [ -n "$JWT_IN_CODE" ]; then
    echo "❌ FAILED"
    echo "  Found JWT secret in code (should use environment variable):"
    echo "$JWT_IN_CODE" | while read line; do echo "    $line"; done
    ERRORS=$((ERRORS + 1))
else
    echo "✅ PASSED"
fi

# Check 2: .env files not in git
echo -n "✓ Checking .env files in .gitignore... "
if ! grep -q "\.env" ../../.gitignore 2>/dev/null; then
    echo "⚠️  WARNING"
    echo "  .env not in .gitignore"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

echo ""
echo "📋 T174: Input Sanitization"
echo "---------------------------"

# Check 3: SQL injection prevention
echo -n "✓ Checking for parameterized queries... "
QUERY_COUNT=$(grep -r "db.Exec\|db.Query" internal/ --include="*.go" | grep "\$[0-9]" | wc -l)
if [ "$QUERY_COUNT" -lt 1 ]; then
    echo "⚠️  WARNING"
    echo "  No parameterized queries found (may use ORM)"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED ($QUERY_COUNT parameterized queries)"
fi

# Check 4: Input validation in handlers
echo -n "✓ Checking for input validation... "
VALIDATION_COUNT=$(grep -r "Validate\|validation\|if.*err" internal/api/handlers/ --include="*.go" | wc -l)
if [ "$VALIDATION_COUNT" -lt 10 ]; then
    echo "⚠️  WARNING"
    echo "  Limited input validation found in handlers"
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

echo ""
echo "📋 T175: SQL Injection Prevention"
echo "----------------------------------"

# Check 5: No raw SQL string concatenation
echo -n "✓ Checking for SQL string concatenation... "
SQL_CONCAT=$(grep -r "\"SELECT.*\"\s*+\|\"INSERT.*\"\s*+\|\"UPDATE.*\"\s*+\|\"DELETE.*\"\s*+" internal/ --include="*.go" | grep -v "test.go" | grep -v "//.*" || true)
if [ -n "$SQL_CONCAT" ]; then
    echo "❌ FAILED"
    echo "  Found SQL string concatenation (use parameterized queries):"
    echo "$SQL_CONCAT" | head -3 | while read line; do echo "    $line"; done
    ERRORS=$((ERRORS + 1))
else
    echo "✅ PASSED"
fi

# Check 6: Use of prepared statements
echo -n "✓ Checking for safe database operations... "
if grep -r "db.Query\|db.Exec\|pgx" internal/ --include="*.go" | grep -q "\$"; then
    echo "✅ PASSED"
else
    echo "⚠️  WARNING"
    echo "  No parameterized query placeholders (\$1, \$2) found"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "📋 T176: Production Security"
echo "----------------------------"

# Check 7: HTTPS configuration hints
echo -n "✓ Checking for security headers... "
if grep -r "Secure\|HttpOnly\|SameSite" internal/ --include="*.go" | grep -q "cookie\|Cookie"; then
    echo "✅ PASSED"
else
    echo "⚠️  WARNING"
    echo "  No secure cookie flags found (add for production)"
    WARNINGS=$((WARNINGS + 1))
fi

# Check 8: Password hashing
echo -n "✓ Checking for password hashing... "
if grep -r "bcrypt" internal/ --include="*.go" | grep -q "GenerateFromPassword\|CompareHashAndPassword"; then
    echo "✅ PASSED"
else
    echo "⚠️  WARNING"
    echo "  No bcrypt password hashing found"
    WARNINGS=$((WARNINGS + 1))
fi

# Check 9: CORS configuration
echo -n "✓ Checking for CORS middleware... "
if grep -r "CORS\|AllowedOrigins" internal/ --include="*.go" >/dev/null 2>&1; then
    echo "✅ PASSED"
else
    echo "⚠️  WARNING"
    echo "  No CORS configuration found"
    WARNINGS=$((WARNINGS + 1))
fi

# Check 10: Rate limiting
echo -n "✓ Checking for rate limiting... "
if grep -r "RateLimit\|rate.*limit" internal/ --include="*.go" >/dev/null 2>&1; then
    echo "✅ PASSED"
else
    echo "⚠️  WARNING"
    echo "  No rate limiting found"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "📋 Additional Security Checks"
echo "------------------------------"

# Check 11: Sensitive data logging
echo -n "✓ Checking for password/token logging... "
SENSITIVE_LOG=$(grep -r "log.*password\|log.*token\|log.*secret" internal/ --include="*.go" | grep -v "test.go" | grep -v "PasswordHash" | grep -v "//.*" || true)
if [ -n "$SENSITIVE_LOG" ]; then
    echo "⚠️  WARNING"
    echo "  Found potential sensitive data in logs:"
    echo "$SENSITIVE_LOG" | head -3 | while read line; do echo "    $line"; done
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

# Check 12: Error message information disclosure
echo -n "✓ Checking for detailed error messages... "
DETAILED_ERRORS=$(grep -r "respondWithError.*err\.Error()" internal/api/handlers/ --include="*.go" || true)
if [ -n "$DETAILED_ERRORS" ]; then
    echo "⚠️  WARNING"
    echo "  Found detailed error messages in responses (may leak info):"
    echo "$DETAILED_ERRORS" | head -3 | while read line; do echo "    $line"; done
    WARNINGS=$((WARNINGS + 1))
else
    echo "✅ PASSED"
fi

echo ""
echo "============================"
echo "📊 Security Summary"
echo "============================"
echo "Errors:   $ERRORS ❌"
echo "Warnings: $WARNINGS ⚠️"
echo ""

if [ "$ERRORS" -eq 0 ] && [ "$WARNINGS" -eq 0 ]; then
    echo "✅ All security checks passed!"
    exit 0
elif [ "$ERRORS" -eq 0 ]; then
    echo "⚠️  Security checks passed with warnings"
    echo "Review warnings for production deployment"
    exit 0
else
    echo "❌ Security checks failed"
    echo "Fix errors before deployment"
    exit 1
fi
