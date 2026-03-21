# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) that document significant technical decisions.

## Lifecycle

ADRs use the following status lifecycle:

- **proposed**: Decision is under discussion and not yet approved.
- **accepted**: Decision is approved and should be followed.
- **superseded**: Decision has been replaced by a newer ADR.
- **deprecated**: Decision is no longer recommended but has not been formally replaced.

When an ADR becomes **superseded**, update it to reference the replacing ADR.

## Numbering and naming

- ADRs are numbered sequentially with three digits: `001`, `002`, `003`, ...
- File format: `<number>-<short-kebab-title>.md`
- Examples:
  - `001-go-backend.md`
  - `002-event-driven-architecture.md`

Numbers are never reused, even if an ADR is deprecated or superseded.

## Authoring conventions

1. Start from `template.md`.
2. Keep the title format: `ADR-<number>: <Title>`.
3. Include required sections:
   - Context
   - Decision
   - Consequences
4. Keep consequences balanced (positive, negative, neutral where applicable).
5. Link related issues/epics in **Technical Story** when available.
