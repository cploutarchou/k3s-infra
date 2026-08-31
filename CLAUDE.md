## Commit policy
- Batch work: at most 1 commit per 30 minutes. Accumulate changes and commit
  as one logical unit. Never commit per-file or per-edit.
- Conventional commits (feat/fix/docs/chore(scope): ...), no AI attribution,
  no emoji.
- Author is the machine's git identity (Christos Ploutarchou). Never override it.
- Never commit secrets. Encrypted SOPS files only.