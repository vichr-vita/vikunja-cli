# Vikunja agent skill setup wizard

This file is for an AI coding assistant to read **with the user**. It is not a
skill to install. Its job is to create a short, personalized `vikunja/SKILL.md`
on the current computer after a conversation. Keep the generated skill and all
credentials outside this repository.

## Instructions for the assistant

1. Read this wizard and the repository [README](README.md). Use the README as
   the source for current CLI commands and configuration behavior.
2. Inspect the current machine before asking questions. Identify the OS, shell,
   available Go version, whether `vikunja-cli` is on `PATH`, and whether a
   Vikunja skill already exists in the user's agent setup. Check only for the
   existence and permissions of the config file; **do not read or display its
   contents**. Do not run a CLI command that connects to Vikunja until the
   user has selected the instance and account for this setup.
3. Ask only for decisions you cannot infer. Have a short back and forth with
   the user, using these topics as needed:

   - Which agent will use the skill: Codex, Hermes, or another harness? If it
     does not have a known skill directory, ask for that directory. If the user
     uses multiple agents, ask which ones should receive a local skill.
   - Is this a new installation, or should an existing Vikunja skill be updated?
     If one exists, inspect it and preserve useful local behavior. Show the
     proposed changes before replacing it; never blindly overwrite it.
   - Which Vikunja instance and account should the CLI use on this computer?
     The user can answer with a name; the URL belongs in the protected CLI
     config file. Ask whether network access requires a VPN or tailnet.
   - Which operations should the agent handle: read-only lookup, ordinary task
     and project edits, labels/comments, subtasks, or uncommon raw API calls?
     Ask about preferred task breakdown and reminder behavior only if relevant.
   - Are there local integrations, such as Home Assistant notifications, that
     the skill should account for? If yes, ask for their trigger rules and
     verification method. Keep personal addresses and exact locations out of
     the skill unless they are necessary for a requested operation.

   Never ask the user to paste an API token, password, backup, or full config
   file into chat. Do not assume household workflow, label names, task IDs,
   geofences, or device entity IDs from someone else's installation.
4. If needed, install the CLI from this checkout using the README's commands.
   Help the user create `~/.config/vikunja/config.json` (or a chosen
   `VIKUNJA_CONFIG` file) **locally** with the instance URL and a least-privilege
   API token. On Unix, ensure the file is regular and mode `0600` or stricter.
   The user enters the token in their local editor or credential workflow; do
   not put it in a command argument, generated skill, transcript, or Git file.
   On Windows, use the platform's private user file permissions rather than
   Unix `chmod`. If the account or token is not ready, finish the non-secret
   skill setup and clearly identify the remaining local setup step.
5. Once the instance/account is selected and credentials are ready, run the
   read-only `vikunja-cli user get`. Confirm the returned account with the user
   if it is ambiguous. Do not create a test task or run the live smoke target
   unless the user requests that extra verification.
6. Generate a **new local skill**, tailored to the answers. For Codex, use
   `$CODEX_HOME/skills/vikunja/SKILL.md` when `CODEX_HOME` is set, otherwise
   `~/.codex/skills/vikunja/SKILL.md`. For Hermes, use the
   user's Hermes skill directory (commonly
   `~/.hermes/skills/productivity/vikunja/SKILL.md`). For other harnesses, use
   their documented skill location or the directory the user supplied. If the
   target has an existing skill, retain a local backup and merge deliberately.
   Do not write the generated skill into this repository or commit it.

## What the generated skill should contain

- YAML frontmatter with `name: vikunja` and a description that routes Vikunja
  task/project requests to the skill. Use any additional frontmatter supported
  by the chosen harness.
- The installed CLI command, selected **config path** (never its contents),
  and any network prerequisite. Prefer referring to the protected config over
  duplicating the instance URL in the skill.
- The operations the user selected, with concise examples adapted from the
  README. State that the CLI uses `/api/v2`, emits JSON, and that list results
  are paginated. Use raw `api` calls only when typed commands do not cover the
  requested operation; check the live API contract before an uncommon write.
- A mutation workflow: resolve the exact resource by listing/searching, inspect
  it, change only the requested fields, read it back, and report its ID/title.
  Do not guess IDs or claim an incomplete first page is exhaustive. Deletions
  need the CLI's exact target-specific `--confirm` argument and the user's
  authorization for that target.
- The user's chosen rules for task breakdown, reminders, labels, and local
  integrations, only where they change the agent's actions. If a notification
  bridge exists, explain how to avoid duplicate reminders and how to verify
  that a completed or moved task stops triggering it.
- A clear boundary between a task/reminder and a calendar event if the user's
  workflow needs one. Do not invent calendar or device integrations.

Keep the skill concise. Put machine-specific details only in this local file or
in other private local references. It must never contain a token, password,
full config JSON, or task data copied from the live instance.

## Finish and verify

Read back the generated `SKILL.md` and check that its frontmatter is valid, its
paths and commands match this machine, and no unanswered placeholders remain.
Check that the protected config is outside Git and has appropriate local
permissions without printing it. If the CLI can connect, the read-only user
lookup is sufficient to verify authentication. Tell the user where the skill
was installed, which account was verified, and any setup step still pending.
