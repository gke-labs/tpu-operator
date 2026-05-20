# Style Guide

## Go

For Go style, please follow the standard Go guidelines and use `gofmt`.

## Markdown

Please use standard markdown formatting.

## Commit Message

Please follow [Conventional Commits][conventional-commits] for your git commit
messages, and refer to the following guide for writing good commit messages:

-   [The seven rules of a great Git commit message][commit-message-rule].

The commit message should be structured as follows:

```text
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

The commit contains the following structural elements, to communicate intent to
the consumers of your library:

-   **fix**: a commit of the type `fix` patches a bug in your codebase (this
    correlates with `PATCH` in semantic versioning).
-   **feat**: a commit of the type `feat` introduces a new feature to the
    codebase (this correlates with `MINOR` in semantic versioning).
-   **BREAKING CHANGE**: a commit that has a footer `BREAKING CHANGE:`, or
    appends a `!` after the type/scope, introduces a breaking API change
    (correlating with `MAJOR` in semantic versioning). A BREAKING CHANGE can be
    part of commits of any type.
-   Types other than `fix:` and `feat:` are allowed, for example:, `chore:`,
    `ci:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, and others.
-   Footers other than `BREAKING CHANGE: <description>` may be provided and
    follow a convention similar to [git trailer format][git-interpret-trailers].

### Commit Message Etiquette

-   The subject line should
    -   contain a short summary of what is being done
    -   use imperative mood
    -   be concise (< 72 chars)
    -   not end with a period
    -   be followed by an empty line
-   The body should
    -   be informative
    -   focus on *what* and *why*, instead of *how*
    -   wrap at 72 chars

### Gerrit Footers and Agent Tracking

When amending commits for Gerrit review, ensure that the `Change-Id` footer is preserved without duplication. To prevent the Gerrit `commit-msg` hook from appending a duplicate `Change-Id`, place an **empty line** immediately before the `Change-Id` line, keeping it in its own paragraph at the very end of the commit message.

Additionally, if AI agents append conversation tracking IDs (`CONV=`), include only the **single most recent** `CONV=` line corresponding to the current conversation. Remove older, stacked `CONV=` entries to avoid footer clutter.

Example:
```text
Issue: #123
CONV=3480ad33-79f1-43a5-bf23-7201ad96c2d0

Change-Id: I6ce475e821c12e68955a416038c7294f94edf587
```


[conventional-commits]: https://www.conventionalcommits.org/en/v1.0.0
[git-interpret-trailers]: https://git-scm.com/docs/git-interpret-trailers
[commit-message-rule]: https://chris.beams.io/posts/git-commit/
