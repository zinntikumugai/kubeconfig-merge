#!/usr/bin/env bash
# e2e test for the built kubeconfig-merge binary.
#
# Runs the real binary against the real fixtures in testdata/scenarios, each in
# a throwaway copy so the repository is never modified.
#
# Usage (from the repository root, after `make build`):
#   ./scripts/e2e.sh
#   BIN=/path/to/kubeconfig-merge ./scripts/e2e.sh

set -euo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "$0")/.." && pwd)
BIN=${BIN:-$REPO_ROOT/kubeconfig-merge}
SCENARIOS=$REPO_ROOT/testdata/scenarios

if [ ! -x "$BIN" ]; then
	echo "e2e: binary not found at $BIN (run 'make build' first)" >&2
	exit 1
fi
if [ ! -d "$SCENARIOS" ]; then
	echo "e2e: scenarios not found at $SCENARIOS" >&2
	exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

failures=0
checks=0

pass() {
	checks=$((checks + 1))
	printf 'PASS %s\n' "$1"
}

fail() {
	checks=$((checks + 1))
	failures=$((failures + 1))
	printf 'FAIL %s\n' "$1"
	if [ $# -gt 1 ]; then
		printf '     %s\n' "$2"
	fi
}

# workdir <scenario-dir> <name> -- copies a scenario (without the golden files)
# into a fresh directory and prints its path.
workdir() {
	local src=$1 name=$2 dst
	dst=$TMP/$name
	rm -rf "$dst"
	mkdir -p "$dst"
	cp -R "$src"/. "$dst"/
	rm -f "$dst"/want-*
	printf '%s\n' "$dst"
}

# runbin <workdir> <logprefix> [args...] -- runs the binary in workdir, capturing
# stdout/stderr outside of it. Prints the exit code.
runbin() {
	local dir=$1 log=$2
	shift 2
	local rc=0
	(cd "$dir" && "$BIN" "$@") >"$TMP/$log.out" 2>"$TMP/$log.err" || rc=$?
	printf '%s\n' "$rc"
}

# file_mode <path> -- prints the permission bits as octal (e.g. 600).
file_mode() {
	stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"
}

echo "== ok-* scenarios =="
for dir in "$SCENARIOS"/ok-*/; do
	name=$(basename "$dir")
	w=$(workdir "$dir" "$name")
	rc=$(runbin "$w" "$name")

	if [ "$rc" = "0" ]; then
		pass "$name: exit 0"
	else
		fail "$name: exit 0" "got exit $rc: $(cat "$TMP/$name.err")"
	fi

	if [ -f "$w/config" ]; then
		pass "$name: config was created"
		mode=$(file_mode "$w/config")
		if [ "$mode" = "600" ]; then
			pass "$name: config is 0600"
		else
			fail "$name: config is 0600" "got $mode"
		fi
		if [ -f "$dir/want-config.yaml" ]; then
			if cmp -s "$dir/want-config.yaml" "$w/config"; then
				pass "$name: config matches want-config.yaml"
			else
				fail "$name: config matches want-config.yaml" "$(diff -u "$dir/want-config.yaml" "$w/config" | head -20)"
			fi
		else
			echo "NOTE $name: no want-config.yaml golden, skipping content comparison"
		fi
	else
		fail "$name: config was created"
	fi
done

echo "== ng-* scenarios =="
for dir in "$SCENARIOS"/ng-*/; do
	name=$(basename "$dir")
	w=$(workdir "$dir" "$name")
	rc=$(runbin "$w" "$name")

	if [ "$rc" = "1" ]; then
		pass "$name: exit 1"
	else
		fail "$name: exit 1" "got exit $rc"
	fi

	if [ -f "$dir/want-error.txt" ]; then
		want=$(cat "$dir/want-error.txt")
		# trim leading/trailing whitespace
		want=$(printf '%s' "$want" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
		if grep -qF -- "$want" "$TMP/$name.err"; then
			pass "$name: stderr contains the expected message"
		else
			fail "$name: stderr contains the expected message" "want: $want / got: $(cat "$TMP/$name.err")"
		fi
	else
		echo "NOTE $name: no want-error.txt, skipping message comparison"
	fi

	if [ -e "$w/config" ]; then
		fail "$name: no config was created"
	else
		pass "$name: no config was created"
	fi
done

echo "== kubectl compatibility =="
w=$(workdir "$SCENARIOS/ok-basic" kubectl-basic)
rc=$(runbin "$w" kubectl-basic)
if [ "$rc" != "0" ]; then
	fail "kubectl: ok-basic run succeeded" "got exit $rc"
elif ! command -v kubectl >/dev/null 2>&1; then
	echo "NOTE kubectl is not installed, skipping the kubectl compatibility check"
else
	if (cd "$w" && kubectl config get-contexts --kubeconfig ./config) >"$TMP/kubectl.out" 2>"$TMP/kubectl.err"; then
		pass "kubectl config get-contexts reads the generated config"
	else
		fail "kubectl config get-contexts reads the generated config" "$(cat "$TMP/kubectl.err")"
	fi
	if grep -qF -- "cluster-merino-admin" "$TMP/kubectl.out"; then
		pass "kubectl lists cluster-merino-admin"
	else
		fail "kubectl lists cluster-merino-admin" "$(cat "$TMP/kubectl.out")"
	fi
fi

echo "== --dry-run =="
w=$(workdir "$SCENARIOS/ok-basic" dry-run)
rc=$(runbin "$w" dry-run --dry-run)
if [ "$rc" = "0" ]; then
	pass "--dry-run: exit 0"
else
	fail "--dry-run: exit 0" "got exit $rc: $(cat "$TMP/dry-run.err")"
fi
if [ -e "$w/config" ]; then
	fail "--dry-run: no config was created"
else
	pass "--dry-run: no config was created"
fi

echo "== backup =="
w=$(workdir "$SCENARIOS/ok-basic" backup)
rc1=$(runbin "$w" backup-1)
rc2=$(runbin "$w" backup-2)
if [ "$rc1" = "0" ] && [ "$rc2" = "0" ]; then
	pass "backup: two consecutive runs exit 0"
else
	fail "backup: two consecutive runs exit 0" "got exit $rc1 and $rc2"
fi
count=$(find "$w/backup" -type f 2>/dev/null | wc -l | tr -d ' ')
if [ "$count" = "1" ]; then
	pass "backup: exactly one backup after the second run"
else
	fail "backup: exactly one backup after the second run" "found $count file(s)"
fi

echo "== flags =="
w=$(workdir "$SCENARIOS/ok-basic" flags)
rc=$(runbin "$w" version --version)
if [ "$rc" = "0" ]; then
	pass "--version: exit 0"
else
	fail "--version: exit 0" "got exit $rc"
fi
rc=$(runbin "$w" help --help)
if [ "$rc" = "0" ]; then
	pass "--help: exit 0"
else
	fail "--help: exit 0" "got exit $rc"
fi
rc=$(runbin "$w" unknown --no-such-flag)
if [ "$rc" = "2" ]; then
	pass "unknown flag: exit 2"
else
	fail "unknown flag: exit 2" "got exit $rc"
fi

echo
if [ "$failures" -eq 0 ]; then
	echo "e2e: all $checks checks passed"
	exit 0
fi
echo "e2e: $failures of $checks checks FAILED"
exit 1
