# Pass `go test -v` output through as it arrives and end with a count of the
# tests that ran, the names of those that failed, and the packages that
# failed, which covers a build error, a panic and the timeout, where no test
# reports a failure of its own. A test is a top-level one; subtests are
# counted beside them.
/^--- PASS: / { passed++ }
/^--- FAIL: / { failed++; failures = failures "\n   ✗ " $3 }
/^--- SKIP: / { skipped++ }
/^ +--- (PASS|FAIL|SKIP): / { subtests++ }
/^FAIL[ \t]+[^ \t]/ { failures = failures "\n   ✗ package " $2 }
{
	print
	fflush()
}
END {
	printf "\n%s %d tests run: %d passed, %d failed, %d skipped (%d subtests)%s\n", \
		failures == "" ? "✅" : "❌", passed + failed + skipped, passed, failed, skipped, subtests, failures
}
