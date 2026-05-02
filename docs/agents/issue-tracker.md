# Issue tracker: GitHub

Issues and PRDs for this repo live as GitHub issues in `kannon-email/kannon`. Use the `gh` CLI for all operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v` — `gh` does this automatically when run inside a clone.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`.

## PRDs

PRDs are tracked as GitHub issues with a parent/child structure:

- **Parent PRD issue**: apply the `prd` label.
  ```
  gh issue create --title "PRD: ..." --body-file prd.md --label prd
  ```
- **Sub-issues** (the implementation slices a PRD breaks into): apply the `prd/subissue` label, and link them as native GitHub sub-issues of the parent PRD.
  ```
  gh issue create --title "..." --body "..." --label prd/subissue
  ```
  Then attach the new issue as a sub-issue of the parent. Sub-issues are not exposed via top-level `gh issue` commands yet — use `gh api` against the REST endpoint:
  ```
  # Resolve issue node ids
  PARENT_ID=$(gh api repos/:owner/:repo/issues/<parent-number> --jq .id)
  CHILD_ID=$(gh api repos/:owner/:repo/issues/<child-number> --jq .id)

  # Attach child as sub-issue of parent
  gh api -X POST repos/:owner/:repo/issues/<parent-number>/sub_issues \
    -f sub_issue_id=$CHILD_ID
  ```
  List a parent's sub-issues with:
  ```
  gh api repos/:owner/:repo/issues/<parent-number>/sub_issues
  ```

If the `prd` or `prd/subissue` labels don't exist yet in the repo, create them once:

```
gh label create prd --description "Product requirements document" --color 0E8A16
gh label create prd/subissue --description "Implementation slice of a PRD" --color C2E0C6
```
