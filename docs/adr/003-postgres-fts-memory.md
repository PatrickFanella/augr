# ADR-003: PostgreSQL full-text search for agent memory retrieval

- **Status:** accepted
- **Date:** 2026-03-21
- **Deciders:** Engineering
- **Technical Story:** Issue: "ADR-003: PostgreSQL full-text search vs vector database for memory"

## Context

Agents in the pipeline recall relevant past situations to improve future decisions (see
`docs/design/backend/memory-and-learning.md`). Each memory consists of a
free-text `situation` description, a `recommendation`, and an eventual `outcome`. At
query time an agent provides a free-text description of the current situation and
expects the top-N most relevant past memories returned, ranked by relevance.

Two broad retrieval strategies were evaluated:

**PostgreSQL full-text search (FTS)** uses the built-in `tsvector`/`tsquery` machinery.
A trigger auto-populates the `situation_tsv` column on every insert or update, and
`ts_rank` / `plainto_tsquery` power relevance-ranked retrieval.

**Vector / embedding search** (e.g. pgvector, Pinecone, Weaviate) converts texts to
dense numerical vectors and ranks by cosine similarity. It can surface semantically
similar memories even when they share no common tokens.

Key constraints at the current stage:

- The corpus is small: one memory per agent role per pipeline run, across at most a few
  thousand runs. FTS performs well into the millions of rows.
- All infrastructure already lives in PostgreSQL; adding a vector store means an extra
  service and operational burden.
- Memory situations are written in a consistent, structured vocabulary (ticker symbols,
  agent roles, outcome labels). Keyword overlap reliably captures relevance without
  needing semantic embeddings.
- Embedding calls add latency and cost on every write and every retrieval; FTS is
  in-process and adds negligible overhead.
- The project is pre-revenue; operational simplicity outweighs marginal retrieval
  quality gains.

## Decision

We will use **PostgreSQL `tsvector` / `tsquery` full-text search** for agent memory
retrieval.

The `agent_memories` table carries a `situation_tsv TSVECTOR` column populated by a
`BEFORE INSERT OR UPDATE` trigger (`to_tsvector('english', situation)`). Retrieval uses
`plainto_tsquery` with `ts_rank` ordering, scoped by `agent_role`. A GIN index on
`situation_tsv` keeps searches fast as the corpus grows.

We will revisit this decision and migrate to embedding-based search when any of the
following conditions are met:

- The `agent_memories` table exceeds **500,000 rows** and FTS precision degrades
  measurably (i.e. top-5 recall in offline evaluation drops below 70 %).
- Situations start to contain domain jargon or paraphrases with low token overlap where
  semantic search would yield meaningfully better recall.
- A vector store (e.g. pgvector extension) is already present in the stack for another
  feature, eliminating the additional operational cost.

## Consequences

### Positive

- Zero additional infrastructure: FTS is a native PostgreSQL feature already used by
  the project.
- Consistent deployment and backup story — memories live in the same database as all
  other application data.
- `ts_rank` and `ts_rank_cd` (cover density) provide relevance ranking comparable to
  BM25 for structured, domain-specific text.
- GIN index on `situation_tsv` gives sub-millisecond search at current and near-term
  data volumes.
- No embedding API calls means lower latency and no per-token cost on every memory
  write or retrieval.

### Negative

- Pure keyword matching misses synonyms and paraphrases; two situations describing the
  same event in different words may not be linked.
- Relevance quality plateaus as corpus grows beyond the size where BM25-style ranking
  saturates; embeddings would continue to improve.
- Migrating to pgvector or an external vector store later requires a one-time
  backfill: embed all existing `situation` texts and write the resulting vectors into
  a new column or table.

### Neutral

- The migration path is straightforward: add a `situation_vec vector(N)` column via
  pgvector, backfill with an embedding model, add an ivfflat or hnsw index, and swap
  the query in `MemoryRepository.Search` from `tsquery` to cosine-distance ordering.
  The `MemoryRepository` interface does not expose the underlying search mechanism, so
  no callers need to change.
- FTS and vector search can coexist during a transition period by running both queries
  and merging results (reciprocal rank fusion).
