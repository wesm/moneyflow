# Categories and groups

Sort transactions into categories, and collect related categories into groups. Each profile has
its own categories and groups.

| Task | Key | Browser control |
| --- | --- | --- |
| Assign a category | `c` | Pencil button or double-click on the Category cell; **Category** in the selection bar |
| Manage categories | `C` | **Edit categories** in the sidebar |
| Manage groups | `G` | **Edit groups** in the sidebar |

Creating, renaming, moving, merging, and deleting are staged changes. Review them with `w` before
committing. **Uncategorized** is a protected fallback, not an ordinary category to remove. The
[manager guide](guide/editing.md#manage-categories-and-groups) lists its keys and how deletion
replacements work.

Where categories are managed depends on the provider:

| Provider | Categories and groups |
| --- | --- |
| Amazon, bank CSV, SimpleFIN | Managed in Moneyflow, locally |
| Monarch | Managed in Monarch. `C` and `G` show `Manage Monarch categories and groups in Monarch.` |
| YNAB | Managed in YNAB. `C` and `G` show `Manage YNAB categories and groups in YNAB.` A category change must target an existing YNAB category; Moneyflow cannot create YNAB categories |

Amazon can copy committed categories and groups from another profile once, during
[initial import](guide/amazon-mode.md#import-purchases).

Category definitions from Python configuration files are not imported. See
[moving to Go](getting-started/transition.md).
