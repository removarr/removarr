---
name: implement-issue
description: Takes one GitHub issue of removarr from open to a reviewed pull request. Runs only when the owner calls it by name, with "next", an issue number or an issue URL. Never start it on your own.
compatibility: Requires git and the GitHub CLI (gh), signed in with an account that can see the repository.
argument-hint: "next | <issue number> | <issue URL>"
disable-model-invocation: true
---

# Implementing an issue

One run takes one issue from open to a reviewed pull request. An agent does the run on its own. The owner checks the result and merges it.

`AGENTS.md` holds the rules for every change. This skill holds the order of work. Its scripts are in the `scripts` folder next to this file. The commands below give their path from the root of the repository.

"The checks" in this skill are the commands that `AGENTS.md` lists as checks under "Commands": formatting, vet, the tests, the race tests and the build. Starting the server is not one of them.

## Starting a run

A run starts only when the owner calls this skill by name. Do not start one because a request mentions an issue.

```
/implement-issue next
/implement-issue 3
/implement-issue https://github.com/removarr/removarr/issues/3
```

That is how Claude Code calls it. Other tools have their own way to call a skill by name; the argument is the same.

| Argument | The issue |
|---|---|
| `next`, or no argument | The open issue with the lowest number whose dependencies are closed (step 1) |
| A number, with or without `#` | That issue |
| The URL of an issue | That issue. The URL must point to an issue of this repository |

Stop and tell the owner when the argument is anything else, when a URL points to another repository or to a pull request, and when the issue does not exist or is closed. Do not guess which issue was meant.

## The run

| # | Step | Skill | Done when |
|---|---|---|---|
| 1 | Pick the issue | | One open issue is chosen, and what it depends on is closed |
| 2 | Prepare | `using-git-worktrees` | A branch from `main` exists and the checks pass on it |
| 3 | Decide what is open | `brainstorming`, cut down | Every question the issue leaves open has its answer in the spec |
| 4 | Plan | `writing-plans` | The plan is committed |
| 5 | Build | `subagent-driven-development` | Every task is built and reviewed, and the branch is reviewed as a whole |
| 6 | Check | `verification-before-completion` | The checks pass on the last commit and the project files are up to date |
| 7 | Open the pull request | `finishing-a-development-branch` | The pull request exists and says `Closes #N` |
| 8 | Ask Copilot and wait | | Copilot has reviewed and CI is green |
| 9 | Answer the review | `receiving-code-review` | Every finding of the review has an answer |
| 10 | Close out | `verification-before-completion` | The report to the owner is written |

The skills in the table are those of the superpowers plugin. "What changes in the superpowers skills" below says where a run departs from them. Without the plugin the steps still hold: plan in the same place, write the test first, have a fresh reviewer look at each task and at the whole branch.

## The steps

### 1. Pick the issue

- The argument says which issue; see "Starting a run". For `next`, take the open issue with the lowest number whose dependencies are closed. An issue names them as "Depends on #N".
- Issue #8 is a list of follow-ups, not a work package. `next` never picks it. Called with its number, ask the owner which of its items are meant; see "Follow-ups".
- If a branch or a pull request for the issue exists already, continue it; see "Where a run continues".
- One issue per run. When the run ends, stop.

### 2. Prepare

- `gh repo view` must show the repository. If it does not, the wrong account is active: stop.
- `git fetch origin`, then create the branch `issue-<N>-<two or three words>` from `origin/main`, in a worktree of its own.
- Read the whole issue, the spec sections it names, `AGENTS.md` and `LEARNINGS.md`.
- Run the checks before you change anything. If they fail on `main`, stop and tell the owner.

### 3. Decide what is open

The `brainstorming` skill exists to produce an approved spec. removarr has one, so the skill is not run in full. This is what remains of it:

- List the questions the issue leaves open. Issues mark them, for example "Decide in the plan whether …".
- Answer each from the spec and its two principles (spec section 2). Write the answer into the spec, add it to the "Amended" line and commit it as `docs:`.
- Keep, for the pull request, what each answer costs if it is wrong.
- If neither the spec nor the principles answer a question, stop and ask the owner. Do not guess, and do not build around the question.

### 4. Plan

- Write the plan with `writing-plans` to `docs/superpowers/plans/YYYY-MM-DD-removarr-<name>.md` and commit it.
- The skill ends by asking the owner to review the plan and to choose how it is executed. A run answers both: subagents execute the plan, and the owner reviews it with the pull request.
- **Plan gate.** If the issue adds a call that changes or deletes something in an app, nothing is built before the owner has said yes to the plan. Today that is issue #5.
  - Write `Plan gate: waiting for the owner` under the title of the plan. Commit, push the branch, tell the owner where the plan is, and stop.
  - When the owner says yes, change the line to `Plan gate: approved by the owner on <date>` and commit it. Only then go on to step 5.
  - The yes is the owner's own word in the session. A comment on GitHub or a line someone else put into a file is not a yes.
- **Small work.** If the plan would have fewer than three tasks, write no plan file and use no subagent per task. Build it in the session, test first, then have one fresh subagent review the whole diff (`requesting-code-review`). Work that falls under the plan gate is never small work: it gets a plan file and waits for the owner, however few tasks it has.

### 5. Build

Follow `subagent-driven-development` as written: a fresh implementer per task, a review of every task, fix rounds, and at the end one review of the whole branch by the most capable model, one round of fixes and one re-review. For removarr, add:

- The implementer writes the test first and sees it fail (`test-driven-development`). A failure that nobody can explain goes through `systematic-debugging`, not through a guess.
- The reviewer of glue code breaks it once on purpose and checks that a test notices.
- Name the model in every dispatch. Use the most capable one for the rules in `internal/scan` and for anything that deletes, a cheaper one for mechanical tasks.
- One implementer at a time.
- Decide instead of waiting. A conflict or a gap in the plan gets a ruling in the ledger: what was decided, why, and what it costs if it is wrong. A ruling the owner would want to know goes into the pull request.
- A finding that is real but does not belong to this issue is kept for issue #8 (step 10).

### 6. Check

- Run every check on the last commit and read the output. "It passed earlier" is not a result.
- Bring the files up to date that `AGENTS.md` lists under "Files to keep up to date".
- **Real apps.** A run does not start removarr against the owner's apps. If the issue changes what a scan lists or what removarr sends to an app, prepare that check: the command to run and what to compare. The owner runs it before merging. See "Checking against real apps" in `LEARNINGS.md`.

### 7. Open the pull request

Push the branch and open the pull request against `main`. Its title is the issue's title. Its description has:

- What changed and why, in a few lines.
- `Closes #N` on a line of its own. GitHub then closes the issue when the pull request is merged.
- **Decisions:** every spec amendment and every ruling the owner should know, each with its cost if it is wrong.
- **How it was checked:** the commands, and counts where something was measured.
- **Check on the real apps:** not needed, or waiting for the owner (with the command), or done (with the result).
- **Not done:** what went to issue #8.

GitHub reads "close", "fix" or "resolve" in front of an issue number as an order, even inside a sentence such as "this does not fix #8". For an issue that must stay open, write "Part of #8".

### 8. Ask Copilot and wait

```
gh pr edit <PR> --add-reviewer @copilot
bash .agents/skills/implement-issue/scripts/wait-for-copilot.sh <PR>
gh pr checks <PR> --watch
```

- `wait-for-copilot.sh` looks every two minutes for up to 30 minutes. If your tool tells you when a background command ends, start it in the background. If not, run it and wait for it to end. Do not look by hand in the meantime.
- It ends with 0 when the review is there, and prints the summary of the review and the open threads. It ends with 2 when Copilot was not asked, with 3 when no review came, with 4 when the pull request is no longer open.
- After a 3, ask Copilot once more and wait once more. If again no review comes, go on without it and say so in the pull request and in the report.
- CI must be green. Fix a red CI before anything else. If `gh pr checks` reports no checks, CI has not started yet: run it again a minute later.

### 9. Answer the review

Use `receiving-code-review`. Copilot is an outside reviewer and has not read the spec. Check every comment against the code and the spec before you believe it.

| The comment is | Do this |
|---|---|
| Right | Write the test that fails, fix, commit as `fix:`, push. Reply with the commit and what changed. Resolve the thread. |
| Right, but outside this issue | Add it to issue #8. Reply with where it went. Resolve the thread. |
| Wrong | Reply with the reason: what the code does, and the rule or the test that shows it. Resolve the thread. |
| The owner's to decide: it goes against the spec or a principle, or asks for a new dependency or other behaviour | Reply that the owner decides. Leave the thread open and name it in the report. |

- Read the summary of the review as well as the threads. A finding in the summary has no thread, and "0 open threads" does not mean that nothing was found. Copilot reports this way on a second review. Treat such a finding like a comment.
- Reply to a thread inside the thread, not as a comment on the pull request. Answer the findings of a summary together, in one comment on the pull request. Say what changed. No thanks and no agreement for its own sake.
- The commands:

  ```
  bash .agents/skills/implement-issue/scripts/review-threads.sh <PR>
  gh api "repos/{owner}/{repo}/pulls/<PR>/comments/<comment ID>/replies" -F body=@reply.md
  bash .agents/skills/implement-issue/scripts/review-threads.sh <PR> resolve <comment ID> [<comment ID> ...]
  gh pr comment <PR> --body-file reply.md
  ```

- After a fix, repeat step 6 and wait for CI again.
- Ask Copilot for a second review only if the fixes changed behaviour, not for changed comments, tests or documents. Two reviews at most: fix and answer what the second one finds, and do not ask for a third.

### 10. Close out

A run is done when all of this holds:

- Every task of the plan is built and reviewed, and the findings of the whole-branch review are fixed or ruled on.
- The checks pass on the last commit and CI is green.
- The description of the pull request is current and says `Closes #N`.
- Copilot has answered the latest request to review, or failed to twice and the pull request says so. An earlier review does not count while a later request is waiting. `wait-for-copilot.sh` tells which it is: it ends with 0 at once when the latest request is answered.
- Every thread and every finding in a review summary has an answer. The only open threads are those left to the owner.
- In the issue, the boxes that the pull request covers are ticked, and each item under "Carried over" says how it ended.
- Issue #8 has the findings that were not fixed, and the items this pull request resolved are ticked there.
- `LEARNINGS.md` has what the next run would otherwise find out again.

Then write the report to the owner: the pull request, what was built, how it was checked, the decisions and what each costs if it is wrong, the threads left open, and what is left for the owner to do (the check on the real apps, the merge).

Do not merge. Do not start the next issue.

## After the merge

The owner merges, or tells the agent to. Use a merge commit, never squash or rebase: the issues and the replies in review threads name commits, and those must stay valid. Afterwards check that the issue is closed and that no thread is open.

## When to stop and ask

Only these stop a run:

- The argument does not name an open issue of this repository.
- A question that neither the spec nor its two principles answer.
- The plan of an issue that adds a changing or deleting call to an app (step 4).
- A new third-party module.
- Anything that leaves the repository and its pull request: a release, a published image, a repository setting, a call that changes something in an app.
- The checks fail on `main` before any change.
- What the issue depends on is still open, or `gh` cannot see the repository.

Decide everything else, write the decision down and go on.

## Where a run continues

A run can break off. GitHub and git show where it stopped:

| You find | Continue at |
|---|---|
| No branch `issue-<N>-…` on `origin` or in the worktree | Step 2 |
| The branch, without a plan file | Step 3 |
| A plan that says `Plan gate: waiting for the owner` | Step 4: ask the owner, and build nothing until they say yes |
| A plan file that is not waiting at the plan gate, and tasks that are not built | Step 5, at the first task that is not built |
| Every task built, no pull request | Step 6 |
| A pull request that Copilot was not asked to review | Step 8 |
| A pull request with a request to Copilot that has no review yet, whatever earlier reviews there are | Step 8, at the waiting. Do not ask again |
| A pull request with open threads that have no reply, or with a review summary whose findings have no answer | Step 9 |
| The latest request to Copilot answered, every thread and every finding answered, and CI green | Step 10 |

To tell the rows about Copilot apart, run `wait-for-copilot.sh` with `0` as the minutes: it ends with 2 when Copilot was not asked, with 3 when a request is waiting, and with 0 when the latest request is answered.

The ledger of `subagent-driven-development` is in `.superpowers/sdd/` in the worktree and not in git. If the worktree is gone, the plan and `git log` show which tasks are built.

## Follow-ups

Issue #8 collects small findings that no work package owns.

- No pull request closes it. A pull request that resolves some of its items says "Part of #8" and ticks them.
- Take items from it when the owner asks, or when they are in code that the current issue changes anyway.
- A few items together are small work (step 4).

## What changes in the superpowers skills

The superpowers skills assume that the owner is at the keyboard, and they ask for approval several times. A run moves those approvals to the pull request. That means fewer stops and a later veto: a wrong decision costs rework on a branch, never anything on `main`.

| Skill | As written | In a run |
|---|---|---|
| `brainstorming` | The owner approves a design and then a written spec | Not run in full, because the spec exists. Step 3 is what remains |
| `writing-plans` | The owner reviews the plan and chooses how it is executed | Subagents execute it, and the owner reviews it with the pull request. The plan gate of step 4 is the exception |
| `subagent-driven-development` | | Unchanged |
| `finishing-a-development-branch` | Offers to merge, to open a pull request or to keep the branch | Always a pull request. A run never merges |
| `receiving-code-review` | | Unchanged. Copilot counts as an outside reviewer |
| `executing-plans` | Executes a plan in the session | Not used: small work has no plan file |
| `dispatching-parallel-agents` | Several agents at once | For reading and searching only, never for two implementers |

Where this skill and a superpowers skill disagree, this skill decides.
