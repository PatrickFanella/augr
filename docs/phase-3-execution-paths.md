---
title: Phase 3 Execution Paths
type: tracking
created: 2026-03-21
tags: [tracking, phase-3, execution]
---

# Phase 3: Execution Paths

> 59 issues across 13 tracks. **25 ready now**, 34 blocked by dependencies.
> Updated: 2026-03-21

## Summary

| Track | Name                   | Total  | Ready  | Blocked | Models   |
| ----- | ---------------------- | :----: | :----: | :-----: | -------- |
| A     | Pipeline Orchestration |   5    |   3    |    2    | Claude Opus 4.6 |
| B     | Analysis Agents        |   9    |   5    |    4    | Mixed    |
| C1    | Research Debate        |   6    |   2    |    4    | Claude Opus 4.6 |
| C2    | Trader Agent           |   1    |   1    |    0    | Claude Opus 4.6 |
| C3    | Risk Debate            |   5    |   0    |    5    | Claude Opus 4.6 |
| D1    | Data Providers         |   12   |   2    |   10    | GPT-5.4  |
| D2    | Technical Indicators   |   4    |   3    |    1    | GPT-5.4  |
| E1    | Alpaca Broker          |   3    |   1    |    2    | GPT-5.4  |
| E2    | Binance Broker         |   2    |   1    |    1    | GPT-5.4  |
| E3    | Paper Broker           |   2    |   1    |    1    | GPT-5.4  |
| F     | Risk Engine            |   3    |   1    |    2    | Claude Opus 4.6 |
| G     | Order Management       |   2    |   1    |    1    | Mixed    |
| H     | Memory & Scheduler     |   4    |   4    |    0    | Mixed    |
|       | **Total**              | **59** | **25** | **34**  |          |

---

## Track A: Pipeline Orchestration

> Must complete first — all agents plug into this.
> Depends on: Nothing (Track A is the critical path root)

| #   | Issue                                                               | Title                                                             | Size | Blocker          | Status  | Model    |
| --- | ------------------------------------------------------------------- | ----------------------------------------------------------------- | :--: | ---------------- | ------- | -------- |
| 1   | [#158](https://github.com/PatrickFanella/get-rich-quick/issues/158) | Implement Phase 2 executor: research debate loop                  |  M   | None             | READY   | Claude Opus 4.6 |
| 2   | [#159](https://github.com/PatrickFanella/get-rich-quick/issues/159) | Implement Phase 3 executor: trader agent                          |  XS  | None             | READY   | GPT-5.4 mini |
| 3   | [#160](https://github.com/PatrickFanella/get-rich-quick/issues/160) | Implement Phase 4 executor: risk debate loop                      |  M   | None             | READY   | Claude Opus 4.6 |
| 4   | [#161](https://github.com/PatrickFanella/get-rich-quick/issues/161) | Implement top-level Pipeline.Execute method with phase sequencing |  M   | #158, #159, #160 | BLOCKED | Claude Opus 4.6 |
| 5   | [#162](https://github.com/PatrickFanella/get-rich-quick/issues/162) | Add agent decision persistence to pipeline executor               |  S   | #161             | BLOCKED | GPT-5.4  |

```mermaid
graph LR
    158[#158 Research debate loop] --> 161
    159[#159 Trader executor] --> 161
    160[#160 Risk debate loop] --> 161
    161[#161 Pipeline.Execute] --> 162[#162 Decision persistence]

    style 158 fill:#22c55e
    style 159 fill:#22c55e
    style 160 fill:#22c55e
    style 161 fill:#ef4444
    style 162 fill:#ef4444
```

**Parallelizable:** #158, #159, #160 can all run simultaneously.

---

## Track B: Analysis Agents

> Depends on: Nothing external (self-contained)
> #163 (base struct) unblocks all 4 agent implementations.

| #   | Issue                                                               | Title                                                          | Size | Blocker    | Status  | Model    |
| --- | ------------------------------------------------------------------- | -------------------------------------------------------------- | :--: | ---------- | ------- | -------- |
| 1   | [#163](https://github.com/PatrickFanella/get-rich-quick/issues/163) | Create base analyst agent struct with shared LLM call logic    |  S   | None       | READY   | GPT-5.4  |
| 2   | [#164](https://github.com/PatrickFanella/get-rich-quick/issues/164) | Write Market Analyst system prompt template                    |  XS  | None       | READY   | Claude Opus 4.6 |
| 3   | [#165](https://github.com/PatrickFanella/get-rich-quick/issues/165) | Implement Market Analyst agent struct and Execute method       |  S   | #163, #164 | BLOCKED | Claude Opus 4.6 |
| 4   | [#167](https://github.com/PatrickFanella/get-rich-quick/issues/167) | Write Fundamentals Analyst system prompt template              |  XS  | None       | READY   | Claude Opus 4.6 |
| 5   | [#168](https://github.com/PatrickFanella/get-rich-quick/issues/168) | Implement Fundamentals Analyst agent struct and Execute method |  S   | #163, #167 | BLOCKED | Claude Opus 4.6 |
| 6   | [#170](https://github.com/PatrickFanella/get-rich-quick/issues/170) | Write News Analyst system prompt template                      |  XS  | None       | READY   | Claude Opus 4.6 |
| 7   | [#172](https://github.com/PatrickFanella/get-rich-quick/issues/172) | Implement News Analyst agent struct and Execute method         |  S   | #163, #170 | BLOCKED | Claude Opus 4.6 |
| 8   | [#174](https://github.com/PatrickFanella/get-rich-quick/issues/174) | Write Social Media Analyst system prompt template              |  XS  | None       | READY   | Claude Opus 4.6 |
| 9   | [#176](https://github.com/PatrickFanella/get-rich-quick/issues/176) | Implement Social Media Analyst agent struct and Execute method |  S   | #163, #174 | BLOCKED | Claude Opus 4.6 |

```mermaid
graph TD
    163[#163 Base analyst struct] --> 165[#165 Market agent]
    164[#164 Market prompt] --> 165
    163 --> 168[#168 Fundamentals agent]
    167[#167 Fundamentals prompt] --> 168
    163 --> 172[#172 News agent]
    170[#170 News prompt] --> 172
    163 --> 176[#176 Social agent]
    174[#174 Social prompt] --> 176

    style 163 fill:#22c55e
    style 164 fill:#22c55e
    style 167 fill:#22c55e
    style 170 fill:#22c55e
    style 174 fill:#22c55e
    style 165 fill:#ef4444
    style 168 fill:#ef4444
    style 172 fill:#ef4444
    style 176 fill:#ef4444
```

**Strategy:** Assign #163 first. While waiting, assign all 4 prompt issues (#164, #167, #170, #174) in parallel. Once #163 + prompts merge, all 4 agents can be assigned simultaneously.

---

## Track C1: Research Debate

> Depends on: Nothing external
> #169 (base debater) unblocks bull, bear, and manager agents.

| #   | Issue                                                               | Title                                                                                   | Size | Blocker          | Status  | Model    |
| --- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | :--: | ---------------- | ------- | -------- |
| 1   | [#166](https://github.com/PatrickFanella/get-rich-quick/issues/166) | Add missing AgentRole constants for risk debate agents                                  |  XS  | None             | READY   | GPT-5.4  |
| 2   | [#169](https://github.com/PatrickFanella/get-rich-quick/issues/169) | Create base debate agent struct with shared LLM call and round context logic            |  S   | None             | READY   | GPT-5.4  |
| 3   | [#171](https://github.com/PatrickFanella/get-rich-quick/issues/171) | Write Bull Researcher system prompt and implement agent                                 |  S   | #169             | BLOCKED | Claude Opus 4.6 |
| 4   | [#173](https://github.com/PatrickFanella/get-rich-quick/issues/173) | Write Bear Researcher system prompt and implement agent                                 |  S   | #169             | BLOCKED | Claude Opus 4.6 |
| 5   | [#175](https://github.com/PatrickFanella/get-rich-quick/issues/175) | Write Research Manager system prompt and implement agent with structured output parsing |  M   | #169             | BLOCKED | Claude Opus 4.6 |
| 6   | [#177](https://github.com/PatrickFanella/get-rich-quick/issues/177) | Implement research debate orchestration node                                            |  M   | #171, #173, #175 | BLOCKED | Claude Opus 4.6 |

```mermaid
graph TD
    166[#166 Role constants] --> 179
    166 --> 180
    166 --> 181
    169[#169 Base debater] --> 171[#171 Bull]
    169 --> 173[#173 Bear]
    169 --> 175[#175 Research Mgr]
    171 --> 177[#177 Research debate orch]
    173 --> 177
    175 --> 177

    style 166 fill:#22c55e
    style 169 fill:#22c55e
    style 171 fill:#ef4444
    style 173 fill:#ef4444
    style 175 fill:#ef4444
    style 177 fill:#ef4444
```

**Strategy:** Assign #166 and #169 together. Once #169 merges, assign #171 and #173 in parallel (bull/bear). Then #175, then #177.

---

## Track C2: Trader Agent

> Depends on: Nothing (standalone)

| #   | Issue                                                               | Title                                                                         | Size | Blocker | Status | Model    |
| --- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------- | :--: | ------- | ------ | -------- |
| 1   | [#178](https://github.com/PatrickFanella/get-rich-quick/issues/178) | Write Trader agent system prompt and implement with structured output parsing |  M   | None    | READY  | Claude Opus 4.6 |

**Single issue, no dependencies.** Can be assigned anytime.

---

## Track C3: Risk Debate

> Depends on: Track C1 (#166 role constants + #169 base debater)

| #   | Issue                                                               | Title                                                                       | Size | Blocker                | Status  | Model    |
| --- | ------------------------------------------------------------------- | --------------------------------------------------------------------------- | :--: | ---------------------- | ------- | -------- |
| 1   | [#179](https://github.com/PatrickFanella/get-rich-quick/issues/179) | Write Aggressive Risk Analyst system prompt and implement agent             |  S   | #169, #166             | BLOCKED | Claude Opus 4.6 |
| 2   | [#180](https://github.com/PatrickFanella/get-rich-quick/issues/180) | Write Conservative Risk Analyst system prompt and implement agent           |  S   | #169, #166             | BLOCKED | Claude Opus 4.6 |
| 3   | [#181](https://github.com/PatrickFanella/get-rich-quick/issues/181) | Write Neutral Risk Analyst system prompt and implement agent                |  S   | #169, #166             | BLOCKED | Claude Opus 4.6 |
| 4   | [#183](https://github.com/PatrickFanella/get-rich-quick/issues/183) | Write Risk Manager system prompt and implement with final signal extraction |  M   | #169                   | BLOCKED | Claude Opus 4.6 |
| 5   | [#185](https://github.com/PatrickFanella/get-rich-quick/issues/185) | Implement risk debate orchestration node                                    |  M   | #179, #180, #181, #183 | BLOCKED | Claude Opus 4.6 |

**Fully blocked** until C1's #166 and #169 merge. Then #179, #180, #181 can run in parallel.

---

## Track D1: Data Providers

> Depends on: Nothing external
> #182 (rate limiter) unblocks most providers.

| #   | Issue                                                               | Title                                                           | Size | Blocker   | Status  | Model   |
| --- | ------------------------------------------------------------------- | --------------------------------------------------------------- | :--: | --------- | ------- | ------- |
| 1   | [#182](https://github.com/PatrickFanella/get-rich-quick/issues/182) | Implement token bucket rate limiter for data provider API calls |  S   | None      | READY   | GPT-5.4 |
| 2   | [#184](https://github.com/PatrickFanella/get-rich-quick/issues/184) | Implement Polygon.io HTTP client and authentication             |  S   | #182      | BLOCKED | GPT-5.4 |
| 3   | [#186](https://github.com/PatrickFanella/get-rich-quick/issues/186) | Implement Polygon.io GetOHLCV (aggregates/bars endpoint)        |  S   | #184      | BLOCKED | GPT-5.4 |
| 4   | [#187](https://github.com/PatrickFanella/get-rich-quick/issues/187) | Implement Polygon.io GetNews endpoint                           |  S   | #184      | BLOCKED | GPT-5.4 |
| 5   | [#188](https://github.com/PatrickFanella/get-rich-quick/issues/188) | Implement Alpha Vantage HTTP client and authentication          |  S   | #182      | BLOCKED | GPT-5.4 |
| 6   | [#189](https://github.com/PatrickFanella/get-rich-quick/issues/189) | Implement Alpha Vantage GetOHLCV                                |  S   | #188      | BLOCKED | GPT-5.4 |
| 7   | [#190](https://github.com/PatrickFanella/get-rich-quick/issues/190) | Implement Alpha Vantage GetFundamentals                         |  S   | #188      | BLOCKED | GPT-5.4 |
| 8   | [#191](https://github.com/PatrickFanella/get-rich-quick/issues/191) | Implement Alpha Vantage GetNews                                 |  S   | #188      | BLOCKED | GPT-5.4 |
| 9   | [#192](https://github.com/PatrickFanella/get-rich-quick/issues/192) | Implement Yahoo Finance data provider (OHLCV only)              |  S   | None      | READY   | GPT-5.4 |
| 10  | [#193](https://github.com/PatrickFanella/get-rich-quick/issues/193) | Implement Binance data provider (crypto OHLCV)                  |  S   | #182      | BLOCKED | GPT-5.4 |
| 11  | [#194](https://github.com/PatrickFanella/get-rich-quick/issues/194) | Implement NewsAPI provider                                      |  S   | #182      | BLOCKED | GPT-5.4 |
| 12  | [#195](https://github.com/PatrickFanella/get-rich-quick/issues/195) | Wire up data provider chains with cache integration             |  M   | All above | BLOCKED | GPT-5.4 |

```mermaid
graph TD
    182[#182 Rate limiter] --> 184[#184 Polygon client]
    182 --> 188[#188 AV client]
    182 --> 193[#193 Binance data]
    182 --> 194[#194 NewsAPI]
    184 --> 186[#186 Polygon OHLCV]
    184 --> 187[#187 Polygon News]
    188 --> 189[#189 AV OHLCV]
    188 --> 190[#190 AV Fundamentals]
    188 --> 191[#191 AV News]
    192[#192 Yahoo Finance]
    186 --> 195[#195 Chain + Cache]
    187 --> 195
    189 --> 195
    190 --> 195
    191 --> 195
    192 --> 195
    193 --> 195
    194 --> 195

    style 182 fill:#22c55e
    style 192 fill:#22c55e
    style 184 fill:#ef4444
    style 188 fill:#ef4444
    style 193 fill:#ef4444
    style 194 fill:#ef4444
    style 186 fill:#ef4444
    style 187 fill:#ef4444
    style 189 fill:#ef4444
    style 190 fill:#ef4444
    style 191 fill:#ef4444
    style 195 fill:#ef4444
```

**Strategy:** Assign #182 first. Once it merges, #184 and #188 can run in parallel (Polygon + Alpha Vantage clients). #192 (Yahoo) has no deps and can start anytime.

---

## Track D2: Technical Indicators

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                              | Size | Blocker          | Status  | Model   |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------ | :--: | ---------------- | ------- | ------- |
| 1   | [#197](https://github.com/PatrickFanella/get-rich-quick/issues/197) | Implement SMA, EMA, and MACD indicators                            |  M   | None             | READY   | GPT-5.4 |
| 2   | [#199](https://github.com/PatrickFanella/get-rich-quick/issues/199) | Implement RSI, MFI, Stochastic, Williams %R, CCI, ROC indicators   |  M   | None             | READY   | GPT-5.4 |
| 3   | [#200](https://github.com/PatrickFanella/get-rich-quick/issues/200) | Implement Bollinger Bands, ATR, VWMA, OBV, ADL indicators          |  M   | None             | READY   | GPT-5.4 |
| 4   | [#202](https://github.com/PatrickFanella/get-rich-quick/issues/202) | Create ComputeAllIndicators helper that returns full indicator set |  XS  | #197, #199, #200 | BLOCKED | GPT-5.4 |

**Parallelizable:** #197, #199, #200 can all run simultaneously. #202 is a small wrapper after all three merge.

---

## Track E1: Alpaca Broker

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                                          | Size | Blocker | Status  | Model   |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------ | :--: | ------- | ------- | ------- |
| 1   | [#196](https://github.com/PatrickFanella/get-rich-quick/issues/196) | Implement Alpaca HTTP client with authentication and paper/live mode switching |  S   | None    | READY   | GPT-5.4 |
| 2   | [#198](https://github.com/PatrickFanella/get-rich-quick/issues/198) | Implement Alpaca SubmitOrder and CancelOrder                                   |  M   | #196    | BLOCKED | GPT-5.4 |
| 3   | [#201](https://github.com/PatrickFanella/get-rich-quick/issues/201) | Implement Alpaca GetOrderStatus, GetPositions, GetAccountBalance               |  S   | #196    | BLOCKED | GPT-5.4 |

**Sequential:** #196 first, then #198 and #201 can run in parallel.

---

## Track E2: Binance Broker

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                                                       | Size | Blocker | Status  | Model   |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | :--: | ------- | ------- | ------- |
| 1   | [#203](https://github.com/PatrickFanella/get-rich-quick/issues/203) | Implement Binance broker HTTP client with HMAC-SHA256 signing                               |  M   | None    | READY   | GPT-5.4 |
| 2   | [#204](https://github.com/PatrickFanella/get-rich-quick/issues/204) | Implement Binance SubmitOrder, CancelOrder, GetOrderStatus, GetPositions, GetAccountBalance |  M   | #203    | BLOCKED | GPT-5.4 |

**Sequential:** #203 first, then #204.

---

## Track E3: Paper Broker

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                                                | Size | Blocker | Status  | Model   |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | :--: | ------- | ------- | ------- |
| 1   | [#205](https://github.com/PatrickFanella/get-rich-quick/issues/205) | Implement paper broker: order fill simulation with configurable slippage             |  S   | None    | READY   | GPT-5.4 |
| 2   | [#206](https://github.com/PatrickFanella/get-rich-quick/issues/206) | Implement paper broker: CancelOrder, GetOrderStatus, GetPositions, GetAccountBalance |  S   | #205    | BLOCKED | GPT-5.4 |

**Sequential:** #205 first, then #206.

---

## Track F: Risk Engine

> Depends on: Nothing (fully independent)

| #   | Issue                                                               | Title                                                                 | Size | Blocker | Status  | Model    |
| --- | ------------------------------------------------------------------- | --------------------------------------------------------------------- | :--: | ------- | ------- | -------- |
| 1   | [#209](https://github.com/PatrickFanella/get-rich-quick/issues/209) | Implement risk engine: pre-trade validation and position limit checks |  M   | None    | READY   | Claude Opus 4.6 |
| 2   | [#210](https://github.com/PatrickFanella/get-rich-quick/issues/210) | Implement risk engine: circuit breaker state machine                  |  M   | #209    | BLOCKED | Claude Opus 4.6 |
| 3   | [#211](https://github.com/PatrickFanella/get-rich-quick/issues/211) | Implement risk engine: kill switch with three mechanisms              |  S   | #209    | BLOCKED | Claude Opus 4.6 |

**Sequential start:** #209 first, then #210 and #211 can run in parallel.

---

## Track G: Order Management

> Depends on: Track E (brokers) + Track F (risk engine) for #208

| #   | Issue                                                               | Title                                                                     | Size | Blocker | Status  | Model    |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------- | :--: | ------- | ------- | -------- |
| 1   | [#207](https://github.com/PatrickFanella/get-rich-quick/issues/207) | Implement position sizing calculator (ATR-based, Kelly, Fixed Fractional) |  M   | None    | READY   | GPT-5.4  |
| 2   | [#208](https://github.com/PatrickFanella/get-rich-quick/issues/208) | Implement order state machine and lifecycle manager                       |  M   | #207    | BLOCKED | Claude Opus 4.6 |

**Sequential:** #207 first (no deps), #208 after (also needs brokers + risk engine in practice).

---

## Track H: Memory & Scheduler

> Depends on: Track A (pipeline) for scheduler integration

| #   | Issue                                                               | Title                                                                           | Size | Blocker | Status | Model    |
| --- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------- | :--: | ------- | ------ | -------- |
| 1   | [#212](https://github.com/PatrickFanella/get-rich-quick/issues/212) | Implement memory reflection: generate agent memories from trade outcomes        |  M   | None    | READY  | Claude Opus 4.6 |
| 2   | [#213](https://github.com/PatrickFanella/get-rich-quick/issues/213) | Implement memory injector: retrieve and format memories for agent prompts       |  S   | None    | READY  | GPT-5.4  |
| 3   | [#215](https://github.com/PatrickFanella/get-rich-quick/issues/215) | Implement market hours checker for equity trading schedule                      |  XS  | None    | READY  | GPT-5.4  |
| 4   | [#214](https://github.com/PatrickFanella/get-rich-quick/issues/214) | Implement cron scheduler: load strategies and trigger pipeline runs on schedule |  M   | None    | READY  | GPT-5.4  |

**All ready.** No internal dependencies. Can all run in parallel.

---

## Cross-Track Dependencies

```mermaid
graph LR
    A[Track A: Pipeline] -.-> H[Track H: Scheduler needs pipeline]
    C1[Track C1: Research Debate] -.-> C3[Track C3: Risk Debate needs base debater + role constants]
    E[Tracks E1-E3: Brokers] -.-> G2[Track G: #208 needs brokers]
    F[Track F: Risk Engine] -.-> G2
    G1[Track G: #207 Position Sizing] -.-> G2

    style A fill:#3b82f6
    style C1 fill:#a855f7
    style C3 fill:#a855f7
    style E fill:#22c55e
    style F fill:#ef4444
    style G1 fill:#eab308
    style G2 fill:#eab308
    style H fill:#6b7280
```

---

## Model Selection Guide

Pick the model that fits the task. Any model available in the Copilot coding agent picker is fair game.

### Available Models

| Model | Strengths | Best for |
|-------|-----------|----------|
| **Claude Opus 4.6** | Strongest reasoning, best at complex multi-step logic, excellent system prompt writing | Pipeline orchestration, debate orchestration, state machines, agent implementations with prompts |
| **Claude Sonnet 4.6** | Strong reasoning at faster speed, good code generation | Agent implementations, structured output parsing, risk engine logic, memory reflection |
| **GPT-5.4** | Strong code generation, good at following patterns, reliable | HTTP clients, broker adapters, data providers, position sizing, test writing |
| **GPT-5.4 mini** | Fast, efficient, good for mechanical/repetitive code | Constants, simple structs, utility functions, market hours checker, boilerplate |
| **GPT-5.3-Codex** | Code-specialized, strong at precise implementations | Technical indicators (math-heavy), algorithms, pure computation |
| **Gemini 3 Pro** | Good general purpose, strong at docs/analysis | Documentation tasks, ADRs, research notes |

### Task-to-Model Mapping

| Task type | Recommended model | Why |
|-----------|-------------------|-----|
| Pipeline/debate orchestration | Claude Opus 4.6 | Complex control flow, multiple interacting components |
| System prompts (agent personality) | Claude Opus 4.6 | Needs nuanced writing that shapes LLM behavior |
| Agent struct + Execute + prompt combined | Claude Sonnet 4.6 | Good balance of reasoning + speed for medium complexity |
| Structured output parsing (JSON) | Claude Sonnet 4.6 | Reliable at building robust parsers with edge cases |
| State machines (circuit breaker, orders) | Claude Sonnet 4.6 | State transition logic needs careful reasoning |
| Memory reflection | Claude Sonnet 4.6 | LLM reasoning about trade outcomes |
| Risk engine validation | Claude Sonnet 4.6 | Financial logic needs precision |
| HTTP clients and API adapters | GPT-5.4 | Well-defined patterns, good at REST client code |
| Data provider implementations | GPT-5.4 | Straightforward API integration |
| Broker CRUD methods | GPT-5.4 | Follows established interface patterns |
| Technical indicators (SMA, RSI, etc.) | GPT-5.3-Codex | Math-heavy, algorithm-focused |
| Position sizing formulas | GPT-5.3-Codex | Mathematical precision |
| Rate limiters, utility functions | GPT-5.4 mini | Simple, well-scoped algorithms |
| Constants, type definitions | GPT-5.4 mini | Mechanical, no reasoning needed |
| Market hours checker | GPT-5.4 mini | Simple time logic |
| Cache wiring, chain configuration | GPT-5.4 | Follows existing patterns |
| Cron scheduler | GPT-5.4 | Standard library usage |

---

## Recommended Assignment Order

### Wave 1 (start now — 4 parallel, all independent tracks)

| Issue                                                               | Track        | Model             |
| ------------------------------------------------------------------- | ------------ | ----------------- |
| [#158](https://github.com/PatrickFanella/get-rich-quick/issues/158) | A - Pipeline | Claude Opus 4.6   |
| [#182](https://github.com/PatrickFanella/get-rich-quick/issues/182) | D1 - Data    | GPT-5.4           |
| [#196](https://github.com/PatrickFanella/get-rich-quick/issues/196) | E1 - Alpaca  | GPT-5.4           |
| [#209](https://github.com/PatrickFanella/get-rich-quick/issues/209) | F - Risk     | Claude Sonnet 4.6 |

### Wave 2 (after wave 1 merges)

| Issue                                                               | Track           | Model             | Unblocked by |
| ------------------------------------------------------------------- | --------------- | ----------------- | ------------ |
| [#159](https://github.com/PatrickFanella/get-rich-quick/issues/159) | A - Pipeline    | GPT-5.4 mini      | —            |
| [#184](https://github.com/PatrickFanella/get-rich-quick/issues/184) | D1 - Polygon    | GPT-5.4           | #182         |
| [#197](https://github.com/PatrickFanella/get-rich-quick/issues/197) | D2 - Indicators | GPT-5.3-Codex     | —            |
| [#198](https://github.com/PatrickFanella/get-rich-quick/issues/198) | E1 - Alpaca     | GPT-5.4           | #196         |
| [#210](https://github.com/PatrickFanella/get-rich-quick/issues/210) | F - Risk        | Claude Sonnet 4.6 | #209         |
| [#205](https://github.com/PatrickFanella/get-rich-quick/issues/205) | E3 - Paper      | Claude Sonnet 4.6 | —            |

### Wave 3 (after wave 2)

| Issue                                                               | Track           | Model             | Unblocked by |
| ------------------------------------------------------------------- | --------------- | ----------------- | ------------ |
| [#160](https://github.com/PatrickFanella/get-rich-quick/issues/160) | A - Pipeline    | Claude Opus 4.6   | —            |
| [#163](https://github.com/PatrickFanella/get-rich-quick/issues/163) | B - Analysts    | Claude Sonnet 4.6 | —            |
| [#169](https://github.com/PatrickFanella/get-rich-quick/issues/169) | C1 - Debate     | Claude Sonnet 4.6 | —            |
| [#166](https://github.com/PatrickFanella/get-rich-quick/issues/166) | C1 - Constants  | GPT-5.4 mini      | —            |
| [#186](https://github.com/PatrickFanella/get-rich-quick/issues/186) | D1 - Polygon    | GPT-5.4           | #184         |
| [#188](https://github.com/PatrickFanella/get-rich-quick/issues/188) | D1 - Alpha V    | GPT-5.4           | #182         |
| [#199](https://github.com/PatrickFanella/get-rich-quick/issues/199) | D2 - Indicators | GPT-5.3-Codex     | —            |
| [#211](https://github.com/PatrickFanella/get-rich-quick/issues/211) | F - Kill switch | GPT-5.4           | #209         |
| [#207](https://github.com/PatrickFanella/get-rich-quick/issues/207) | G - Pos sizing  | GPT-5.3-Codex     | —            |
