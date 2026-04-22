# Style Guide

## Go

Please follow [Google Go style][go-style], with the following exceptions:

### Go Mock

Use of gomock should be a deliberate and informed choice. See
[when-to-use-gomock][when-to-use-gomock] for when this might (not) be the right
choice.

## Markdown

Please follow [g3doc style guide][g3doc-style]. Please use [mdformat][mdformat]
to format the markdown files.

## Commit Message

Please follow [Conventional Commits][conventional-commits] for your git commit
messages, and refer to the following guides for writing good commit messages:

-   [Writing Good CL Descriptions][cl-description-rule]
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

### Linking a Buganizer Issue on the Commit Message

GKE Gerrit enabled go/gitwatcher, so if your commit message contains a buganizer
issue in the form of 'b/xxxxxx', the issue will be updated once commit is
merged. For consistency please use a git-trailer format to link the issue. For
example:

```text
Bug: b/234567
```

[go-style]: https://g3doc.corp.google.com/go/g3doc/style/index.md
[g3doc-style]: https://g3doc.corp.google.com/corp/g3doc/docs/reference/style.md
[mdformat]: https://g3doc.corp.google.com/devtools/markdown/mdformat/g3doc/index.md
[conventional-commits]: https://www.conventionalcommits.org/en/v1.0.0
[git-interpret-trailers]: https://git-scm.com/docs/git-interpret-trailers
[cl-description-rule]: https://goto.google.com/cl-descriptions
[commit-message-rule]: https://chris.beams.io/posts/git-commit/
[when-to-use-gomock]: http://go/when-to-use-gomock
