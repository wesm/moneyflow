# Security Review Bot

This repository uses an automated security review bot powered by Claude 4.5 Sonnet to review all pull requests from external contributors.

## 🎯 Purpose

Since moneyflow handles sensitive financial data (account balances, transactions, encrypted credentials), we maintain strict security standards. This bot provides:

- **Consistent baseline security review** for all external contributions
- **Early detection** of common security issues before human review
- **Educational feedback** to contributors about security best practices

**Important:** This bot supplements, but does not replace, human security review.

## 🔧 Setup

### 1. Get Anthropic API Key

1. Sign up at https://console.anthropic.com/
2. Add a payment method (pay-as-you-go)
3. Generate an API key from the dashboard
4. **Optional:** Set spending limits to control costs

### 2. Add API Key to GitHub Secrets

1. Go to your repository's **Settings** → **Secrets and variables** → **Actions**
2. Click **New repository secret**
3. Name: `ANTHROPIC_API_KEY`
4. Value: Your API key from step 1
5. Click **Add secret**

### 3. That's it!

The workflow will automatically run on all new PRs from external contributors.

## 👥 Trusted Contributors

PRs from trusted contributors (owners/maintainers) bypass the automated review to:
- Save API costs
- Speed up internal development
- Avoid noise on PRs from experienced maintainers

### Managing the Trusted List

Edit `.github/trusted-contributors.json`:

```json
{
  "trusted_github_usernames": [
    "wesm",
    "another-maintainer"
  ]
}
```

**When to add someone:**
- They're a repository owner/maintainer
- They have write access to the repository
- They have a proven track record with security

**When NOT to add someone:**
- They're an occasional contributor
- They're external to the project
- You want their PRs reviewed (even if trusted)

## 📊 What the Bot Reviews

The bot looks for:

**High Priority:**
- 🔑 Hardcoded secrets, API keys, passwords
- 🔐 Weakened encryption or credential handling
- 💉 Injection vulnerabilities (SQL, command, path traversal)
- 📝 Logging of sensitive data (PII, credentials)
- 🔓 Authentication/authorization bypasses

**Medium Priority:**
- 📦 Dependencies with known vulnerabilities
- 🎯 Input validation issues
- 🗂️ Unsafe file operations
- ⚠️ Error messages leaking sensitive info

**Low Priority:**
- 📚 Security documentation gaps
- 🧪 Test data with real credentials
- ⚙️ Insecure default configurations

## 📝 How It Works

1. **Trigger:** PR opened/updated from non-trusted contributor
2. **Fetch:** Bot retrieves the full PR diff
3. **Analyze:** Claude reviews the changes with security context
4. **Report:** Bot posts inline comments on specific issues
5. **Summary:** Bot posts overall summary comment

## 💬 Example Output

```
🚨 Hardcoded encryption key (high severity)

The encryption key is hardcoded in the source. This means all users
would share the same key, defeating the purpose of encryption.
Instead, derive the key from a user-specific passphrase or use
the system keyring.

---
Automated security review by Claude 4.5 Sonnet - Human review still required
```

## 💰 Cost Monitoring

### Expected Costs

**Typical usage:**
- ~10 external PRs per month
- ~$0.05-0.15 per review
- **Total: $1-2/month**

**Higher volume:**
- ~50 external PRs per month
- **Total: $5-10/month**

### Monitoring Costs

1. View usage at https://console.anthropic.com/
2. Check the **Usage** tab for daily/monthly costs
3. Set spending limits under **Settings** → **Limits**

### If Costs Get Too High

If you're getting excessive PRs:
1. Consider raising the barrier for first-time contributors
2. Add more usernames to the trusted list
3. Disable the workflow temporarily during spam waves

## 🔍 Interpreting Results

### ✅ No Issues Found

The bot posts:
> 🔒 Security Review: No Issues Found

**This means:** No obvious security concerns detected. Still do human review.

### ⚠️ Issues Found

The bot posts inline comments on specific files/lines.

**How to respond:**
1. **Review each issue carefully** - false positives are possible
2. **Assess severity** - high > medium > low priority
3. **Discuss with contributor** - help them understand the concern
4. **Request changes** or **accept risk** with justification
5. **Document your decision** in the PR discussion

### 🚨 High Severity Issues

**Never merge without addressing these:**
- Hardcoded secrets or credentials
- Obvious injection vulnerabilities
- Disabled security controls
- Cleartext storage of sensitive data

**Either:**
- Work with contributor to fix
- Fix it yourself before merge
- Reject the PR if unfixable

## 🛠️ Troubleshooting

### Bot Doesn't Run

**Check:**
- Is PR from a trusted contributor? (Expected - no review needed)
- Is `ANTHROPIC_API_KEY` set in GitHub Secrets?
- Check Actions tab for error messages

### Bot Posts Too Many False Positives

**Solutions:**
1. Adjust the prompt in `.github/scripts/security_review.py`
2. Make the severity threshold higher
3. Add project-specific context to the prompt

### Bot Misses Real Issues

**Solutions:**
1. Improve the prompt with examples of missed issues
2. Add more security context from documentation
3. Consider switching to Claude Opus (more expensive, better reasoning)

### API Key Issues

**Error: "Invalid API key"**
- Regenerate key in Anthropic console
- Update GitHub secret

**Error: "Rate limit exceeded"**
- Anthropic API has rate limits for new accounts
- Contact Anthropic support to increase limits

## 🔒 Security of the Bot Itself

**The bot has access to:**
- PR contents (code being reviewed)
- GitHub API (read PRs, write comments)
- Anthropic API (send code for review)

**Security measures:**
- API key stored in GitHub Secrets (encrypted)
- Minimal GitHub permissions (pull-requests: write, contents: read)
- No persistent storage of PR contents
- Anthropic doesn't train on API data (per their policy)

**Risks to consider:**
- PR contents sent to Anthropic's API (third party)
- If API key leaks, attacker could incur API costs
- Bot could post spam comments (if compromised)

## 📚 Further Reading

- [Anthropic API Documentation](https://docs.anthropic.com/)
- [GitHub Actions Security](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [OWASP Top 10](https://owasp.org/www-project-top-ten/)

## 🤝 Contributing

Improvements to the security bot are welcome! If you have ideas:

1. Test changes locally first
2. Consider impact on API costs
3. Validate prompt changes don't increase false positives
4. Document any new features here

## 📞 Support

**Questions or issues?**
- Open a GitHub issue
- Tag the repository owner (@wesm)
