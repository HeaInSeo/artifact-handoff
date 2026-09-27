#!/usr/bin/env bash
# Fails when committed generated code under api/proto/ahv1/ does not match
# what `buf generate` produces from api/proto/*.proto. Run after `buf generate`.
#
# The only tolerated difference is the protoc version line in the generated
# header: buf reports "protoc (unknown)" while the committed files were
# generated with a local protoc. Every other changed line fails the check.
set -euo pipefail

gen_dir="api/proto/ahv1/"

# Any output written outside the tracked directory means buf.gen.yaml no longer
# maps go_package onto api/proto/ahv1/ (issue #30), so the diff below would
# compare nothing. Only generated artifacts count: buf.gen.yaml's plugins emit
# *.pb.go / *_grpc.pb.go, and the pathspec matches them at any depth. Unrelated
# untracked files (local scratch, notes) are not buf output and are ignored.
stray="$(git ls-files --others --exclude-standard -- '*.pb.go')"
if [ -n "${stray}" ]; then
  echo "::error::buf generate produced untracked files; commit them or fix buf.gen.yaml output mapping:"
  echo "${stray}"
  exit 1
fi

if ! git diff --exit-code -I '^//.*[[:space:]]protoc[[:space:]]' -- "${gen_dir}"; then
  echo "::error::generated code in ${gen_dir} is stale; run 'make proto' and commit the result"
  exit 1
fi

echo "proto drift check: ${gen_dir} matches buf generate output"
