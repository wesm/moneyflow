#!/usr/bin/env python3
"""
Security Review Bot - Uses Claude to review PRs for security issues

This script:
1. Fetches the PR diff from GitHub
2. Sends it to Claude 4.5 Sonnet for security analysis
3. Posts inline comments on security concerns
"""

import json
import os
import sys
from pathlib import Path
from typing import Any

import anthropic
from github import Github


def get_pr_diff() -> str:
    """Get the full diff for this PR."""
    base_sha = os.environ["BASE_SHA"]
    head_sha = os.environ["HEAD_SHA"]

    # Use git to get the diff
    import subprocess

    result = subprocess.run(
        ["git", "diff", f"{base_sha}...{head_sha}"],
        capture_output=True,
        text=True,
        check=True,
    )

    return result.stdout


def get_changed_files() -> list[dict[str, Any]]:
    """Get list of changed files with their patches."""
    g = Github(os.environ["GITHUB_TOKEN"])
    repo = g.get_repo(os.environ["REPO_NAME"])
    pr = repo.get_pull(int(os.environ["PR_NUMBER"]))

    files = []
    for file in pr.get_files():
        files.append(
            {
                "filename": file.filename,
                "status": file.status,  # added, modified, removed
                "patch": file.patch if file.patch else "",
                "additions": file.additions,
                "deletions": file.deletions,
            }
        )

    return files


def read_security_context() -> str:
    """Read security documentation to provide context to Claude."""
    context_files = [
        "SECURITY.md",
        "CLAUDE.md",
        "README.md",
    ]

    context = []
    for filename in context_files:
        filepath = Path(filename)
        if filepath.exists():
            context.append(f"\n# {filename}\n\n{filepath.read_text()}")

    return "\n".join(context)


def build_security_prompt(diff: str, files: list[dict], context: str) -> str:
    """Build the prompt for Claude's security review."""

    files_summary = "\n".join(
        [f"- {f['filename']} ({f['status']}, +{f['additions']} -{f['deletions']})" for f in files]
    )

    return f"""You are a security reviewer for a personal finance management application called "moneyflow". This application handles sensitive financial data including:
- Bank account balances and transactions
- Encrypted credentials for financial APIs
- Personal spending patterns and merchant information

# Your Task

Review this pull request for security vulnerabilities and concerns. Focus on issues that could:
- Expose sensitive financial data
- Compromise credential encryption
- Allow unauthorized access to user data
- Introduce injection vulnerabilities
- Leak secrets or API keys
- Weaken existing security controls

# Project Context

{context}

# Changed Files

{files_summary}

# Pull Request Diff

```diff
{diff}
```

# Your Response Format

If you find security concerns, respond with a JSON array of issues. Each issue should have:
- `file`: The filename with the issue
- `line`: Approximate line number in the NEW version of the file (use your best judgment from the diff)
- `severity`: "high", "medium", or "low"
- `title`: Brief title (max 60 chars)
- `description`: Detailed explanation with suggested fix (2-4 sentences)

Example:
```json
[
  {{
    "file": "moneyflow/credentials.py",
    "line": 42,
    "severity": "high",
    "title": "Hardcoded encryption key",
    "description": "The encryption key is hardcoded in the source. This means all users would share the same key, defeating the purpose of encryption. Instead, derive the key from a user-specific passphrase or use the system keyring."
  }}
]
```

If NO security concerns are found, respond with:
```json
[]
```

**Important:**
- Only flag genuine security issues, not style or code quality
- Be specific about the risk and impact
- Suggest concrete fixes
- Consider false positives - if unsure, err on the side of flagging it
- Focus on high-impact issues for this sensitive financial application

Provide ONLY the JSON array in your response, no other text.
"""


def parse_claude_response(response: str) -> list[dict]:
    """Parse Claude's JSON response into issues."""
    # Claude might wrap JSON in markdown code blocks
    response = response.strip()

    if response.startswith("```json"):
        response = response[7:]
    if response.startswith("```"):
        response = response[3:]
    if response.endswith("```"):
        response = response[:-3]

    response = response.strip()

    try:
        issues = json.loads(response)
        if not isinstance(issues, list):
            print(f"Warning: Expected list, got {type(issues)}", file=sys.stderr)
            return []
        return issues
    except json.JSONDecodeError as e:
        print(f"Error parsing Claude response: {e}", file=sys.stderr)
        print(f"Response was: {response[:500]}", file=sys.stderr)
        return []


def post_review_comments(issues: list[dict]) -> None:
    """Post review comments on the PR."""
    if not issues:
        print("✅ No security issues found")
        post_summary_comment(0)
        return

    g = Github(os.environ["GITHUB_TOKEN"])
    repo = g.get_repo(os.environ["REPO_NAME"])
    pr = repo.get_pull(int(os.environ["PR_NUMBER"]))

    # Post each issue as a review comment
    severity_emoji = {"high": "🚨", "medium": "⚠️", "low": "ℹ️"}

    comments_posted = 0
    for issue in issues:
        emoji = severity_emoji.get(issue["severity"], "⚠️")
        comment_body = f"""{emoji} **{issue["title"]}** ({issue["severity"]} severity)

{issue["description"]}

---
*Automated security review by Claude 4.5 Sonnet - Human review still required*
"""

        try:
            # Try to post as inline comment if we have a valid line number
            if issue.get("line") and issue.get("file"):
                # Get the file object to find the actual position
                files = list(pr.get_files())
                target_file = next((f for f in files if f.filename == issue["file"]), None)

                if target_file and target_file.patch:
                    # Post at the first line of the patch if we can't determine exact line
                    pr.create_review_comment(
                        body=comment_body,
                        commit=pr.get_commits().reversed[0],
                        path=issue["file"],
                        line=issue["line"],
                    )
                    comments_posted += 1
                else:
                    # Fall back to PR comment if file not found
                    pr.create_issue_comment(f"**In `{issue['file']}`:**\n\n{comment_body}")
                    comments_posted += 1
            else:
                # Post as general PR comment if no file/line specified
                pr.create_issue_comment(comment_body)
                comments_posted += 1

        except Exception as e:
            print(f"Error posting comment: {e}", file=sys.stderr)
            # Fall back to general comment
            try:
                pr.create_issue_comment(
                    f"**In `{issue.get('file', 'unknown')}`:**\n\n{comment_body}"
                )
                comments_posted += 1
            except Exception as e2:
                print(f"Error posting fallback comment: {e2}", file=sys.stderr)

    print(f"Posted {comments_posted} security review comments")
    post_summary_comment(len(issues))


def post_summary_comment(num_issues: int) -> None:
    """Post a summary comment on the PR."""
    g = Github(os.environ["GITHUB_TOKEN"])
    repo = g.get_repo(os.environ["REPO_NAME"])
    pr = repo.get_pull(int(os.environ["PR_NUMBER"]))

    if num_issues == 0:
        summary = """## 🔒 Security Review: No Issues Found

Claude's automated security review did not identify any obvious security concerns in this PR.

**Note:** This is an automated review and should not replace human security review, especially for changes involving:
- Credential handling
- Data encryption
- API authentication
- File system access
- Input validation

---
*Powered by Claude 4.5 Sonnet*
"""
    else:
        summary = f"""## 🔒 Security Review: {num_issues} Issue{"s" if num_issues != 1 else ""} Found

Claude's automated security review identified potential security concerns. Please review the inline comments.

**Note:** This is an automated review. False positives are possible. Please review each issue carefully and use your judgment.

---
*Powered by Claude 4.5 Sonnet*
"""

    pr.create_issue_comment(summary)


def main() -> None:
    """Main entry point."""
    print("🔍 Starting security review...")

    # Check for required environment variables
    required_vars = [
        "ANTHROPIC_API_KEY",
        "GITHUB_TOKEN",
        "PR_NUMBER",
        "REPO_NAME",
        "BASE_SHA",
        "HEAD_SHA",
    ]
    missing = [var for var in required_vars if not os.environ.get(var)]
    if missing:
        print(f"Error: Missing environment variables: {', '.join(missing)}", file=sys.stderr)
        sys.exit(1)

    # Get PR information
    print("📥 Fetching PR diff...")
    diff = get_pr_diff()
    files = get_changed_files()

    if not diff.strip():
        print("No changes to review")
        return

    print(f"📄 Reviewing {len(files)} changed file(s)...")

    # Get security context
    context = read_security_context()

    # Build prompt
    prompt = build_security_prompt(diff, files, context)

    # Call Claude
    print("🤖 Calling Claude for security analysis...")
    client = anthropic.Anthropic(api_key=os.environ["ANTHROPIC_API_KEY"])

    try:
        message = client.messages.create(
            model="claude-sonnet-4-5-20250929",
            max_tokens=4096,
            messages=[{"role": "user", "content": prompt}],
        )

        response_text = message.content[0].text
        print(f"📊 Received response ({len(response_text)} chars)")

        # Parse response
        issues = parse_claude_response(response_text)
        print(f"Found {len(issues)} issue(s)")

        # Post comments
        post_review_comments(issues)

        print("✅ Security review complete")

    except Exception as e:
        print(f"Error calling Claude API: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
