# Categories and groups

Use categories to classify transactions and groups to organize related categories.
Categories and groups belong to a profile; they are not a shared configuration file.

Press `c` to assign a category. Press `C` to manage categories or `G` to manage groups
where the provider allows it. Creation, rename, move, merge, and deletion are staged changes.
Review with `w` before committing. Uncategorized is a protected fallback, not an ordinary
category to remove.

Amazon and SimpleFIN manage categories locally. Amazon can copy a committed taxonomy from
another profile once, during [initial import](guide/amazon-mode.md#import-purchases).
Monarch taxonomy is managed in Monarch. YNAB writes must target an existing provider category;
creating provider categories is not supported.

Category definitions from Python configuration files are not imported automatically. See
[moving to Go](getting-started/transition.md).
