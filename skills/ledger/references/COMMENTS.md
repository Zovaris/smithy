# Comment policy and enforcement

## Policy

Code carries no explanatory comments. Allowed:
- Doc comments on public API: TSDoc/JSDoc `/** */`, Python docstrings, GoDoc `// Name …` right
  above an exported symbol, Javadoc, Rust `///` and `//!`, KDoc.
- License or SPDX headers, shebangs, encoding lines, and a script's opening usage block (the
  comment header right after the shebang).
- Tool directives **with a reason**: `eslint-disable-next-line rule -- reason`,
  `@ts-expect-error reason`, `# noqa: E501 reason`, `# type: ignore[code]`, `//nolint:name // reason`.
- `//go:build`, `//go:generate`, Rust `// SAFETY:` on unsafe blocks.

Forbidden: explanatory inline or block comments, commented-out code, TODO/FIXME/HACK/XXX (open
an issue), bare directives without a reason. Enforce with the stack's linter
(below) plus `smithy comments` in pre-commit (and in CI only where smithy is installed on the runner).

## Enforcing it

Linters do most of the work; `smithy comments` is the cross-language catch-all for what they
can't express (a blanket ban on explanatory comments). Offer the snippet for the repo's stack and
merge it into the existing config; never replace a config wholesale.

## TypeScript / JavaScript (ESLint flat config)

Plugins: `eslint-plugin-jsdoc`, `eslint-plugin-tsdoc` (TS), `@eslint-community/eslint-plugin-eslint-comments`.

```js
rules: {
  "jsdoc/require-jsdoc": ["error", { publicOnly: true, require: { FunctionDeclaration: true, ClassDeclaration: true, MethodDefinition: true } }],
  "jsdoc/require-description": "error",
  "tsdoc/syntax": "warn",
  "@typescript-eslint/ban-ts-comment": ["error", { "ts-expect-error": "allow-with-description", "ts-ignore": true, "ts-nocheck": true }],
  "@eslint-community/eslint-comments/require-description": "error",
  "@eslint-community/eslint-comments/no-unlimited-disable": "error",
  "no-warning-comments": ["error", { terms: ["todo", "fixme", "hack", "xxx"], location: "anywhere" }],
  "no-inline-comments": "error",
}
```

## Python (ruff)

```toml
[tool.ruff.lint]
extend-select = ["D", "ERA", "FIX", "TD", "PGH003", "PGH004", "RUF100"]
[tool.ruff.lint.pydocstyle]
convention = "google"
```

`D` covers docstrings on public modules, classes and functions; `ERA001` commented-out code;
`FIX`/`TD` TODO-style comments; `PGH003/004` blanket `type: ignore` / `noqa`; `RUF100` unused
`noqa`. Ruff has no rule banning all comments: `smithy comments` covers that.

## Go (golangci-lint)

```yaml
version: "2"   # golangci-lint v2; on v1 move `settings` to a top-level `linters-settings`
linters:
  enable: [revive, godot, godox, gocritic, nolintlint]
  settings:
    revive:
      rules: [{ name: exported }, { name: package-comments }]
    gocritic:
      enabled-checks: [commentedOutCode]
    nolintlint:
      require-explanation: true
      require-specific: true
```

## Java (Checkstyle)

```xml
<module name="MissingJavadocType"><property name="scope" value="public"/></module>
<module name="MissingJavadocMethod"><property name="scope" value="public"/></module>
<module name="JavadocMethod"/>
<module name="TodoComment"><property name="format" value="(TODO|FIXME|HACK|XXX)"/></module>
<module name="TrailingComment"/>
<module name="RegexpSinglelineJava">
  <property name="format" value="^\s*//(?!\s*(SPDX|Copyright))"/>
  <property name="message" value="Explanatory comments belong in docs; use Javadoc for API."/>
</module>
```

## Rust

```rust
#![deny(missing_docs)]
#![deny(rustdoc::broken_intra_doc_links)]
#![warn(clippy::missing_errors_doc, clippy::missing_panics_doc, clippy::undocumented_unsafe_blocks)]
```

CI: `RUSTDOCFLAGS="-D warnings" cargo doc --no-deps`.

## Kotlin (detekt)

```yaml
comments:
  UndocumentedPublicClass: { active: true }
  UndocumentedPublicFunction: { active: true }
  UndocumentedPublicProperty: { active: true }
style:
  ForbiddenComment: { active: true, comments: [{ value: 'TODO' }, { value: 'FIXME' }, { value: 'HACK' }] }
```

## Everywhere

Run `smithy comments --diff <base>` in a pre-commit hook (and in CI where the runner has smithy installed) so the policy holds
whatever editor or agent wrote the code.
