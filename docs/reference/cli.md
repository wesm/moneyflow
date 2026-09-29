# Command reference

Run `moneyflow --help` to list commands and add `--help` at any level for accepted options.
Command help belongs to the installed binary; it may differ from newer source documentation.

| Command | Purpose and guide |
| --- | --- |
| `moneyflow tui` | [Terminal application](../getting-started/quickstart.md) |
| `moneyflow web` | [Browser application and proxy settings](../config/advanced.md) |
| `moneyflow mcp` | [Profile-scoped MCP server](../guide/mcp.md) |
| `moneyflow profile export --profile NAME_OR_ID --output FILE` | [Export saved Go profile data](../getting-started/transition.md#transfer-a-go-profile-with-jsonl) |
| `moneyflow profile import --input FILE --name NAME` | [Import into a new Go profile](../getting-started/transition.md#transfer-a-go-profile-with-jsonl) |
| `moneyflow provider connect monarch` | [Connect Monarch](../guide/monarch.md) |
| `moneyflow provider connect ynab` | [Connect YNAB](../guide/ynab.md) |
| `moneyflow provider connect simplefin` | [Connect experimental SimpleFIN](../guide/simplefin-cli.md) |
| `moneyflow provider disconnect monarch` | Remove the local Monarch session |
| `moneyflow provider import amazon DIRECTORY` | [Import Amazon files](../guide/amazon-mode.md) |
| `moneyflow provider import list` | [List bank CSV mappings](../guide/bank-csv.md) |
| `moneyflow provider import institution MAPPING PATH --profile NAME_OR_ID` | [Import bank CSV files](../guide/bank-csv.md) |
| `moneyflow version` | Print version, commit, and build time |
| `moneyflow openapi --format json` | Print the HTTP API contract |
| `moneyflow completion` | Generate shell completion instructions |

Use `--profile NAME_OR_ID` to select an existing profile. Amazon and bank CSV import can
create named profiles; connect commands do not create arbitrary named profiles. Add those in TUI or web.

Never pass credentials in command arguments. Use masked prompts or the provider command's
documented standard-input path. Old Python commands are not compatibility aliases.
