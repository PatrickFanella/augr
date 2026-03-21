---
title: LLM Backtesting Challenges
description: Non-determinism, temporal contamination, cost, and prompt sensitivity unique to LLM strategy testing
type: reference
tags:
  [
    LLM,
    backtesting,
    non-determinism,
    prompt-sensitivity,
    temporal-contamination,
  ]
created: 2026-03-20
---

# LLM Backtesting Challenges

LLM strategies face fundamental backtesting problems beyond standard [[backtesting-methodology|pitfalls]].

## The Non-Determinism Problem

Even at temperature zero, LLMs produce varying outputs due to:

- Floating-point drift
- Non-deterministic GPU operations
- Batch differences

OpenAI's `seed` parameter provides "best effort" reproducibility but **does not guarantee deterministic outputs**. Anthropic similarly notes temperature 0.0 results "will not be fully deterministic."

This fundamentally distinguishes LLM backtesting from traditional quant backtesting where signals are perfectly reproducible.

## Prompt Sensitivity

**Extreme**: One developer reported switching system prompts turned -15% returns into +17%. Requires systematic prompt versioning and testing multiple prompt configurations.

## Temporal Contamination

Backtesting LLM responses to historical news introduces contamination: the model may have seen outcomes during training. FINSABER addresses this by testing across periods both before and after LLM training data cutoffs, but the challenge remains inherent.

## Cost

| Item                          | Cost                         |
| ----------------------------- | ---------------------------- |
| Single comprehensive backtest | $10-40 in inference fees     |
| Total development costs       | Can exceed $150 in API calls |
| Live operation                | Continuous inference costs   |

## Practical Mitigations

1. **Cache LLM outputs** with hash of prompt, model version, and timestamp
2. **Run multiple backtest iterations** at each configuration to evaluate robustness
3. Use the **exact same execution engine** for backtesting and live trading
4. **Systematic prompt versioning** -- treat prompts as code, version and test them
5. Test across periods before and after model training data cutoffs

## Related

- [[backtesting-methodology]] - General backtesting best practices
- [[llm-strategy-limitations]] - Broader performance concerns
- [[paper-trading]] - Next validation step
