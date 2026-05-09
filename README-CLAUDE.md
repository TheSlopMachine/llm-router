# CLAUDE MUST FOLLOW THESE RULES WITHOUT EXCEPTION

## Communication
- Be concise. Answer what was asked, nothing more.
- Do not use emojis anywhere - not in answers, not in code, not in comments.
- Do not explain what you just did after doing it. The code speaks for itself.
- Do not ask clarifying questions unless the task is genuinely ambiguous in a way that would cause you to build the wrong thing entirely.
- Never make things up. If you are unsure about an API, behavior, or version detail, look it up first.

## Code changes
- Make surgical edits. Change only what needs to change.
- Do not refactor code that is not related to the task.
- Do not add comments explaining obvious things.
- Do not leave TODO comments behind.
- Do not create markdown files, READMEs, or documentation unless explicitly asked.
- Prefer fewer files. Never create a new file when editing an existing one works.
- Do not over-engineer. Solve the problem at hand, not hypothetical future problems.

## Build discipline
- After every change, build in debug mode and confirm it succeeds before considering the task done.
- When a build fails, read the actual error message carefully before attempting a fix. Do not guess.
- If you introduced a regression, fix it immediately - do not move on.

## Hard constraints
- Do not touch version control. No git commands, no staging, no commits.
- Do not add logging or debug print statements unless asked.
- After completing a task, propose a single commit message following conventional commits format. Do not execute it.
