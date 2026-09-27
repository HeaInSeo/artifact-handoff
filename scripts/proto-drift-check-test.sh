#!/usr/bin/env bash
# Self-test for proto-drift-check.sh. Each case runs the check in a throwaway
# git repo and asserts PASS or FAIL, so a regression in the guard's scope
# (too broad or too narrow) fails CI.
set -euo pipefail

check="$(cd "$(dirname "$0")" && pwd)/proto-drift-check.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

gen="api/proto/ahv1/artifact.pb.go"
failures=0

new_repo() {
  local dir="${work}/$1"
  mkdir -p "${dir}/api/proto/ahv1"
  git -C "${dir}" init -q
  printf '// \tprotoc        v7.34.1\npackage ahv1\n' > "${dir}/${gen}"
  git -C "${dir}" add -A
  git -C "${dir}" -c user.name=t -c user.email=t@t commit -q -m init
  echo "${dir}"
}

expect() {
  local want="$1" name="$2" dir="$3" got
  if (cd "${dir}" && "${check}" >/dev/null 2>&1); then got=PASS; else got=FAIL; fi
  if [ "${got}" = "${want}" ]; then
    echo "ok   ${name}: ${got}"
  else
    echo "FAIL ${name}: want ${want}, got ${got}"
    failures=$((failures + 1))
  fi
}

d="$(new_repo clean)"
expect PASS "clean tree" "${d}"

d="$(new_repo unrelated-untracked)"
echo scratch > "${d}/notes.txt"
mkdir -p "${d}/tmp" && echo x > "${d}/tmp/local.go"
expect PASS "unrelated untracked files are ignored" "${d}"

d="$(new_repo header-only)"
printf '// \tprotoc        (unknown)\npackage ahv1\n' > "${d}/${gen}"
expect PASS "protoc version header difference is tolerated" "${d}"

d="$(new_repo stray-mismapped)"
mkdir -p "${d}/github.com/HeaInSeo/artifact-handoff/api/proto/ahv1"
echo 'package ahv1' > "${d}/github.com/HeaInSeo/artifact-handoff/api/proto/ahv1/artifact.pb.go"
expect FAIL "generated output outside api/proto/ahv1 (issue #30)" "${d}"

d="$(new_repo stray-grpc)"
echo 'package ahv1' > "${d}/api/proto/ahv1/new_grpc.pb.go"
expect FAIL "uncommitted new generated file" "${d}"

d="$(new_repo stale)"
printf '// \tprotoc        v7.34.1\npackage ahv1\nvar X = 1\n' > "${d}/${gen}"
expect FAIL "stale committed generated code" "${d}"

if [ "${failures}" -ne 0 ]; then
  echo "::error::proto-drift-check self-test: ${failures} case(s) failed"
  exit 1
fi
echo "proto-drift-check self-test: all cases passed"
