# Sample files

Synthetic, non-sensitive data exposed as the `samples` file root by `make run`, `make dev`
and `deploy/docker-compose.yml` (`DV_FILE_ROOTS=samples=…`, mounted read-only).

`formulas.csv` holds text that spreadsheet apps would treat as formulas (`=1+1`,
`@SUM(A1:A2)`). It exists to check that CSV downloads from Explore neutralise them
(NFR-SEC-018); the values are inert.
