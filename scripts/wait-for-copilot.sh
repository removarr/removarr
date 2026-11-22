#!/bin/bash
# Wait until Copilot has answered the latest request to review a pull request.
#
# Usage: scripts/wait-for-copilot.sh <pull request> [minutes to wait, default 30]
#
# Looks every two minutes (INTERVAL=<seconds> changes that) and ends with status
#   0  Copilot reviewed after it was last asked; the summary of the review and the open
#      review threads are printed
#   2  Copilot was not asked, or the request was withdrawn
#   3  no review arrived in the time given
#   4  the pull request is merged or closed
#
# Start it in the background, so that the session is told when it ends, and do not look
# by hand in the meantime. A review took between three and seven minutes when this was
# written.
#
# Needs gh, signed in with an account that can see the repository.
set -eu

PR="${1:-}"
MINUTES="${2:-30}"
INTERVAL="${INTERVAL:-120}"
case "$PR" in '' | *[!0-9]*) echo "usage: wait-for-copilot.sh <pull request> [minutes]" >&2; exit 64 ;; esac
case "$MINUTES" in '' | *[!0-9]*) echo "minutes must be a whole number" >&2; exit 64 ;; esac
case "$INTERVAL" in '' | *[!0-9]*) echo "INTERVAL must be a whole number of seconds" >&2; exit 64 ;; esac
# 10# reads "08" as eight; without it the shell takes a leading zero for octal.
MINUTES=$((10#$MINUTES))
INTERVAL=$((10#$INTERVAL))
# An interval of zero would ask GitHub without a pause.
if [ "$INTERVAL" -lt 1 ]; then echo "INTERVAL must be at least one second" >&2; exit 64; fi

HERE="$(cd "$(dirname "$0")" && pwd)"
DEADLINE=$(($(date +%s) + MINUTES * 60))

# One line per event that matters, as "<time> <event>". The timeline names the reviewer
# "Copilot" both when it is asked and when it answers.
events() {
  gh api "repos/{owner}/{repo}/issues/$PR/timeline" --paginate --jq '
    .[]
    | if (.event == "review_requested" or .event == "review_request_removed")
         and .requested_reviewer.login == "Copilot"
      then "\(.created_at) \(.event)"
      elif .event == "reviewed" and .user.login == "Copilot"
      then "\(.submitted_at) reviewed"
      else empty end'
}

while :; do
  # A failed read is not an answer: try again until the time is up.
  if state=$(gh api "repos/{owner}/{repo}/issues/$PR" --jq .state) && log=$(events); then
    if [ "$state" != open ]; then
      echo "pull request $PR is no longer open"
      exit 4
    fi
    # Sort by time only and keep the timeline's order within one second: a request and
    # its withdrawal can share a time, and the later one has to stay the later one.
    log=$(printf '%s\n' "$log" | sort -s -k1,1)
    asked=$(printf '%s\n' "$log" | awk '$2 == "review_requested" || $2 == "review_request_removed" { last = $0 } END { print last }')
    reviewed=$(printf '%s\n' "$log" | awk '$2 == "reviewed" { last = $1 } END { print last }')
    case "$asked" in
      '')
        echo "Copilot was not asked to review pull request $PR"
        exit 2
        ;;
      *review_request_removed)
        echo "the request for Copilot to review pull request $PR was withdrawn"
        exit 2
        ;;
    esac
    # The times are UTC in one format, so comparing them as text compares them as times.
    if [ -n "$reviewed" ] && [[ "$reviewed" > "${asked%% *}" ]]; then
      echo "Copilot reviewed pull request $PR at $reviewed"
      exec "$HERE/review-threads.sh" "$PR"
    fi
    echo "$(date +%H:%M) no review from Copilot on pull request $PR yet"
  else
    echo "$(date +%H:%M) could not read pull request $PR" >&2
  fi

  left=$((DEADLINE - $(date +%s)))
  if [ "$left" -le 0 ]; then
    echo "no review from Copilot on pull request $PR within $MINUTES minutes"
    exit 3
  fi
  # Never sleep past the time given: the last look is at the end of it.
  if [ "$left" -lt "$INTERVAL" ]; then sleep "$left"; else sleep "$INTERVAL"; fi
done
