#!/usr/bin/env bash
# Fails when committed generated code under api/proto/ahv1/ does not match
# what `buf generate` produces from api/proto/*.proto.
#
# buf generate writes into a fresh temporary directory, so the complete output
# set is compared, not only the files that happen to be overwritten in place:
#   - output written outside api/proto/ahv1/ (buf.gen.yaml mapping drift, #30)
#   - a committed generated file the generator no longer emits (obsolete)
#   - an emitted file that is not committed (new)
#   - any content change, comments included
#
# The only tolerated difference is the protoc version in the generated header
# ("// <TAB>protoc        vX.Y.Z" from protoc-gen-go, "// - protoc  vX.Y.Z" from
# protoc-gen-go-grpc): buf reports "(unknown)" while the committed files were
# generated with a local protoc. Only those exact header lines are normalized.
#
# Usage: proto-drift-check.sh [generated-root]
#   generated-root  existing buf output to compare against (used by the
#                   self-test). Default: run `${BUF:-buf} generate --output`.
set -euo pipefail

gen_dir="api/proto/ahv1"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if [ "$#" -ge 1 ]; then
  out="$(cd "$1" && pwd)"
else
  out="${work}/gen"
  mkdir -p "${out}"
  "${BUF:-buf}" generate --output "${out}"
fi

(cd "${out}" && find . -type f | sed 's|^\./||') | LC_ALL=C sort > "${work}/generated"

stray="$(grep -v "^${gen_dir}/" "${work}/generated" || true)"
if [ -n "${stray}" ]; then
  echo "::error::buf generate wrote output outside ${gen_dir}/; fix buf.gen.yaml output mapping:"
  echo "${stray}"
  exit 1
fi

git ls-files -- "${gen_dir}/*.pb.go" | LC_ALL=C sort > "${work}/committed"

obsolete="$(LC_ALL=C comm -13 "${work}/generated" "${work}/committed")"
if [ -n "${obsolete}" ]; then
  echo "::error::committed generated files are no longer produced by buf generate; delete them:"
  echo "${obsolete}"
  exit 1
fi

missing="$(LC_ALL=C comm -23 "${work}/generated" "${work}/committed")"
if [ -n "${missing}" ]; then
  echo "::error::buf generate produced files that are not committed; run 'make proto' and commit them:"
  echo "${missing}"
  exit 1
fi

tab="$(printf '\t')"
header_re="^(// ${tab}protoc +|// - protoc +)(v[0-9][0-9A-Za-z.+-]*|\\(unknown\\))\$"

# Header lines sit within the first few lines; normalizing only there keeps a
# look-alike line further down (e.g. in a doc comment) under comparison.
normalize() {
  sed -E "1,8s#${header_re}#\\1<protoc-version>#"
}

stale=0
while IFS= read -r f; do
  if ! diff -u --label "committed/${f}" --label "generated/${f}" \
    <(git show ":${f}" | normalize) <(normalize < "${out}/${f}"); then
    stale=1
  fi
done < "${work}/generated"

if [ "${stale}" -ne 0 ]; then
  echo "::error::generated code in ${gen_dir}/ is stale; run 'make proto' and commit the result"
  exit 1
fi

echo "proto drift check: ${gen_dir}/ matches buf generate output"
