# Learnings

- Grafana dashboards here use flat v1 JSON: uid, title, schemaVersion 39, version 1, timezone browser, refresh 10s, time now-1h→now.
- Prometheus datasource UID is `prometheus`.
- Existing dashboards keep panels simple: timeseries/stat only, no templating blocks.
