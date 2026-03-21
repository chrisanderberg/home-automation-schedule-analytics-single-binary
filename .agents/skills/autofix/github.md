# Git Platform Commands

GitHub CLI commands for the CodeRabbit Autofix skill.

## Prerequisites

**GitHub CLI (`gh`):**
- Install: `brew install gh` or [cli.github.com](https://cli.github.com/)
- Authenticate: `gh auth login`
- Verify: `gh auth status`

## Core Operations

### 1. Find Pull Request

```bash
gh pr list --head $(git branch --show-current) --state open --json number,title
```

Gets the PR number for the current branch.

### 2. Fetch Unresolved Threads

Use GitHub GraphQL `reviewThreads` (there is no REST `pulls/<pr-number>/threads` endpoint):

```bash
all_threads='[]'
after=null

while :; do
  page_json="$(gh api graphql \
    -F owner='{owner}' \
    -F repo='{repo}' \
    -F pr=<pr-number> \
    -F after="$after" \
    -f query='query($owner:String!, $repo:String!, $pr:Int!, $after:String) {
      repository(owner:$owner, name:$repo) {
        pullRequest(number:$pr) {
          reviewThreads(first:100, after:$after) {
            pageInfo {
              endCursor
              hasNextPage
            }
            nodes {
              isResolved
              comments(first:1) {
                nodes {
                  databaseId
                  body
                  author { login }
                }
              }
            }
          }
        }
      }
    }')"

  all_threads="$(jq -c \
    '. + $page' \
    <<<"$all_threads" \
    --argjson page "$(jq '.data.repository.pullRequest.reviewThreads.nodes // []' <<<"$page_json")")"

  has_next_page="$(jq -r '.data.repository.pullRequest.reviewThreads.pageInfo.hasNextPage' <<<"$page_json")"
  if [ "$has_next_page" != "true" ]; then
    break
  fi
  after="$(jq -r '.data.repository.pullRequest.reviewThreads.pageInfo.endCursor' <<<"$page_json")"
done
```

Filter criteria:
- `isResolved == false`
- root comment author is one of: `coderabbitai`, `coderabbit[bot]`, `coderabbitai[bot]`

Use the root comment body for the issue prompt.

Use `jq` or equivalent to filter `all_threads` down to unresolved CodeRabbit
threads after the loop completes.

### 3. Post Summary Comment


```bash
gh pr comment <pr-number> --body "$(cat <<'EOF'
## Fixes Applied Successfully

Fixed <file-count> file(s) based on <issue-count> unresolved review comment(s).

**Files modified:**
${MODIFIED_FILES_MARKDOWN}

**Commit:** `<commit-sha>`

The latest autofix changes are on the `<branch-name>` branch.

EOF
)"
```

Post after the push step (if pushing) so branch state is final.

### 4. Acknowledge Review

```bash
# React with thumbs up to the CodeRabbit comment
gh api repos/{owner}/{repo}/issues/comments/<comment-id>/reactions \
  -X POST \
  -f content='+1'
```

Find the comment ID from step 2.

### 5. Create PR (if needed)

```bash
gh pr create --title '<title>' --body '<body>'
```

## Error Handling

**Missing `gh` CLI:**
- Inform user and provide install instructions
- Exit skill

**API failures:**
- Abort the workflow when critical discovery/fetch calls fail, including PR lookup and unresolved thread retrieval
- Log and continue only for non-critical write operations such as comment posting or reactions

**Getting repo info:**
```bash
gh repo view --json owner,name,nameWithOwner
```
