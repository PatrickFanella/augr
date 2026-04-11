2026-04-11: Retained retry behavior for 429/5xx/context canceled; snip only DeadlineExceeded changed.
2026-04-11: Kept 429/5xx retryable; only DeadlineExceeded moved to fail-fast so fallback can take over.
2026-04-11: FallbackProvider uses context.WithTimeout(context.Background(), remaining-parent-timeout) when parent still has time; else falls back to 60s default so expired parent ctx does not poison secondary.
