# 002: SQL-first transactional migrations

Own a small runner with a Store/Session contract and a PostgreSQL adapter using
database/sql. The CLI registers github.com/lib/pq for the wire protocol; neither
the HTTP package nor the PostgreSQL adapter chooses the application's driver.
No ORM, schema DSL or generic database abstraction is introduced.

Files have positive int64 numeric versions and exact Up/Down markers. Numeric
ordering, duplicate detection and SHA-256 over original bytes make execution
deterministic. Missing, renamed, changed or retroactively inserted migrations
fail before executing pending SQL. Preserve file line endings after application.

One advisory lock spans each command on one dedicated session, including status.
Contenders fail fast. Cleanup uses a fresh timeout; uncertain unlocks discard the
connection. Use PostgreSQL directly or a session-mode pooler, never transaction
pooling. The history table is explicitly public.graft_migrations to avoid implicit
search_path-dependent state. There is one migration stream per database.

Each migration and its history change share one transaction. A failed batch may
have earlier successful migrations; retry skips those. Rollback without a step
reverses the latest batch; --step N reverses N latest migrations across batches.
Rollback removes applied-history rows, so history tracks current applied state,
not an immutable audit log. Durations record SQL execution, excluding commit.

Graft owns transaction boundaries. A small lexical check rejects explicit
transaction-control statements while allowing dollar-quoted function bodies.
This is not a SQL parser or sandbox: migration authors are trusted. SQL requiring
nontransactional execution (e.g. CREATE INDEX CONCURRENTLY), psql commands and
COPY FROM STDIN are outside v0.1. Use separately reviewed operational SQL for those.
