#!/bin/bash
# List the review threads of a pull request, or resolve some of them.
#
# Usage:
#   scripts/review-threads.sh <pull request>
#       Prints every open thread: the ID of its first comment, who wrote it, the place in
#       the code, how many comments the thread has, and the text of the first comment.
#   scripts/review-threads.sh <pull request> resolve <comment ID> [<comment ID> ...]
#       Resolves the threads that start with these comments. Safe to run again.
#
# To answer inside a thread, before resolving it:
#   gh api "repos/{owner}/{repo}/pulls/<pull request>/comments/<comment ID>/replies" -F body=@reply.md
#
# Needs gh, signed in with an account that can see the repository.
set -eu

usage() {
  echo "usage: review-threads.sh <pull request> [resolve <comment ID> ...]" >&2
  exit 64
}

PR="${1:-}"
case "$PR" in '' | *[!0-9]*) usage ;; esac
shift

QUERY='
query($owner: String!, $name: String!, $pr: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $pr) {
      reviewThreads(first: 100) {
        pageInfo { hasNextPage }
        nodes {
          id isResolved isOutdated path line originalLine
          comments(first: 1) { totalCount nodes { databaseId author { login } body } }
        }
      }
    }
  }
}'

# A pull request with more threads than one page holds is an error, not a shorter list.
ALL='.data.repository.pullRequest.reviewThreads
  | if .pageInfo.hasNextPage
    then error("more than 100 review threads; this script reads only the first 100")
    else .nodes end'

# threads <jq filter over the list of threads>
threads() {
  gh api graphql -F owner='{owner}' -F name='{repo}' -F pr="$PR" -f query="$QUERY" --jq "$ALL | $1"
}

if [ $# -eq 0 ]; then
  threads '
    map(select(.isResolved | not)) as $open
    | ($open[]
       | "--- comment \(.comments.nodes[0].databaseId) by \(.comments.nodes[0].author.login // "a deleted account")"
         + " at \(.path):\(.line // .originalLine // "?")"
         + (if .isOutdated then " (the code has changed since)" else "" end)
         + ", comments in the thread: \(.comments.totalCount)\n"
         + .comments.nodes[0].body + "\n"),
      "open: \($open | length), resolved: \(length - ($open | length))"'
  exit 0
fi

[ "$1" = resolve ] || usage
shift
[ $# -gt 0 ] || usage

list=$(threads '.[] | [.id, (.isResolved | tostring), (.comments.nodes[0].databaseId | tostring)] | @tsv')

# Check every ID before resolving anything.
for want in "$@"; do
  if ! printf '%s\n' "$list" | awk -F'\t' -v id="$want" '$3 == id { found = 1 } END { exit !found }'; then
    echo "no review thread of pull request $PR starts with comment $want" >&2
    exit 1
  fi
done

for want in "$@"; do
  line=$(printf '%s\n' "$list" | awk -F'\t' -v id="$want" '$3 == id')
  thread=$(printf '%s' "$line" | cut -f1)
  if [ "$(printf '%s' "$line" | cut -f2)" = true ]; then
    echo "already resolved: comment $want"
    continue
  fi
  gh api graphql -f id="$thread" -f query='
    mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { isResolved } } }' \
    --jq '.data.resolveReviewThread.thread.isResolved' > /dev/null
  echo "resolved: comment $want"
done
