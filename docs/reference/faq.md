# Common questions

Short answers, each with a link to the page that has the details.

## Do I need Python?

No. Moneyflow is one binary and needs no Python, Go, or Bun runtime. Maintainers use Python and
uv only to build this website.

## Does Moneyflow change my bank data?

Only Monarch and YNAB, and only after you review and commit the edits. Amazon, bank CSV, and
SimpleFIN edits stay in Moneyflow. See [editing](../guide/editing.md).

## I use Python Moneyflow. What happens to my data?

Read [Moving to Go](../getting-started/transition.md).

## Does Amazon import need my Amazon password?

No. It reads the order-history CSV files you download from Amazon. See
[Amazon purchases](../guide/amazon-mode.md).

## Can I use the same profile in the terminal and the browser?

Yes, at the same time. Both read and write the same profile, with the same keys. If one changes
the profile while the other shows a review, the commit is refused; review again.

## Is SimpleFIN ready?

SimpleFIN is experimental. Automated tests pass, but it has not been tested against live banks.
See [SimpleFIN validation](../guide/simplefin.md#help-validate-an-institution).

## Why does the website differ from my build?

The website documents the latest release. A build from source can include newer changes. Run
`moneyflow version` and check the [release notes](changelog.md).
