CREATE TABLE historical_ohlcv (
    ticker     TEXT        NOT NULL,
    provider   TEXT        NOT NULL,
    timeframe  TEXT        NOT NULL,
    bar_time   TIMESTAMPTZ NOT NULL,
    open       DOUBLE PRECISION NOT NULL,
    high       DOUBLE PRECISION NOT NULL,
    low        DOUBLE PRECISION NOT NULL,
    close      DOUBLE PRECISION NOT NULL,
    volume     DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (ticker, provider, timeframe, bar_time)
);

CREATE INDEX idx_historical_ohlcv_ticker_timeframe_bar_time
    ON historical_ohlcv (ticker, timeframe, bar_time);

CREATE TABLE historical_ohlcv_coverage (
    ticker     TEXT        NOT NULL,
    provider   TEXT        NOT NULL,
    timeframe  TEXT        NOT NULL,
    range_from TIMESTAMPTZ NOT NULL,
    range_to   TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (ticker, provider, timeframe, range_from, range_to)
);

CREATE INDEX idx_historical_ohlcv_coverage_ticker_timeframe_range
    ON historical_ohlcv_coverage (ticker, timeframe, range_from, range_to);
