# README Redesign

## Goal

Make the project page immediately useful to a Go developer evaluating or
adopting the library.

## Logo Asset

Use the supplied 1448×1086 PNG as the edit target. Remove excess white space
above and below the horizontal logo while preserving the mascot, puzzle board,
graph marks, wordmark, colors, proportions, and white background unchanged.

Keep a small even margin around the visible artwork. Do not redraw, restyle,
sharpen, recolor, or add text. Save the final project asset as:

```text
docs/assets/go-minizinc-logo.png
```

Embed it near the top of `README.md` with centered HTML, descriptive alt text,
and a display width around 720 pixels so GitHub can scale it responsively.

## Tone

Write concise technical English. Prefer concrete behavior and runnable examples
over claims such as “smart”, “full functionality”, or “type-safe”. Avoid emoji,
sales language, excessive bold text, repeated feature lists, and generated-sounding
transitions.

## Structure

1. Logo and one-sentence description.
2. Requirements and installation.
3. Copy-paste quick start using the practical API.
4. Model sources and solve-scoped parameters.
5. Explicit and automatic solver selection.
6. Single, multiple, and streaming solutions.
7. Typed decoding, raw output, statistics, and errors.
8. Cancellation, compatibility, examples, and testing.

## Content Rules

Every code sample handles returned errors unless the omitted handling is not
part of the point and is explicitly indicated outside the snippet. The first
example uses `NewModel(code...)`, `NewInstanceAuto`, `WithParams`, and
`Result.Decode`.

Keep advanced escape hatches discoverable: `AddString`, `AddFile`,
`WithExtraArgs`, custom `Driver`, explicit `Solver`, output modes, and the
Builder DSL. Remove the long duplicated API inventory; Go documentation remains
the authoritative symbol reference.

State that each CLI solve starts MiniZinc and that instance reuse is an API
convenience rather than incremental compilation.

## Validation

Verify the asset exists at the referenced relative path, inspect the cropped
image, check Markdown links, compile all examples, and run the full Go test and
lint suite.

## Non-Goals

- No new logo design or visual identity.
- No generated badges without an existing project endpoint.
- No documentation site or README table of contents.
- No code behavior changes solely for presentation.
