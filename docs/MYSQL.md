# MySQL migrations

Graft supports MySQL 8.0+; integration checks run on MySQL 8.4. PostgreSQL remains
the default. MariaDB and other MySQL-compatible products are not yet certified.
Create the database first and grant the migration user the DDL/DML privileges
needed by your SQL, plus CREATE/SELECT/INSERT/UPDATE/DELETE on graft_migrations.

```dotenv
DATABASE_DRIVER=mysql
DATABASE_URL=user:password@tcp(localhost:3306)/app
```

Use the Go MySQL driver's DSN, not a mysql:// URL. Quote the entire value in .env
if it contains spaces or #. Configure TLS for remote production connections, for
example `?tls=true` with a trusted server certificate. Never commit real credentials.
The CLI enables parseTime, UTC timestamp parsing and multiStatements automatically.
Use a direct writable-server connection; named locks are session-local and do not
coordinate separate servers in a multi-primary cluster.

```sh
graft make:migration create_users
graft migrate
graft migrate:status
graft migrate:rollback
graft migrate:rollback --step 1
```

Edit the generated migration before running it:

```sql
-- +graft Up
CREATE TABLE users (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;

-- +graft Down
DROP TABLE users;
```

`--dir` and `--timeout` work as on PostgreSQL. Rollback without --step undoes the
last batch. Already-applied migrations are skipped; names/checksums, missing files
and out-of-order versions are checked. History is in the selected database's
graft_migrations table. Do not edit applied files. Existing PostgreSQL SQL (SERIAL,
TIMESTAMPTZ, $1 placeholders, public schema, etc.) is not converted automatically.
Keep separate migration directories if maintaining both dialects.

## Failure and recovery

MySQL DDL implicitly commits. Graft cannot atomically roll back a whole file:
earlier SQL statements may remain committed when a later statement fails. Before
executing Up or Down, Graft commits a `dirty='up'` or `dirty='down'` history marker.
Success clears the marker (Up) or removes the history row (Down). SQL errors,
timeouts and crashes leave the marker. Status returns a nonzero exit code with
the dirty version/direction; migrate and rollback also stop without executing SQL.
This deliberately errs on the side of caution if the execution result is unknown.

Do not simply clear dirty and retry. Recovery must be performed by an operator:

1. Stop other migration runners and application writes; back up schema and data.
2. Inspect the failed SQL, actual schema/data and the history row. Restore the
   state **before the failed operation**, including any affected data. A failed
   Down must be restored to its fully applied state, not its pending state.
3. Only after verifying that restoration, repair the single affected history row
   using a SQL client on the same database. Hold Graft's lock throughout repair:

```sql
SET @graft_lock = CONCAT('graft:', LEFT(SHA2(LOWER(DATABASE()), 256), 56));
SELECT GET_LOCK(@graft_lock, 0); -- must return 1; otherwise STOP
SELECT * FROM graft_migrations WHERE dirty <> '';

-- Replace 202609140001 with the exact inspected version. Choose ONLY ONE:
-- Failed Up, after restoring the state before Up:
DELETE FROM graft_migrations WHERE version=202609140001 AND dirty='up';
-- Failed Down, after restoring the fully applied state:
UPDATE graft_migrations SET dirty='' WHERE version=202609140001 AND dirty='down';

SELECT RELEASE_LOCK(@graft_lock);
```

Confirm exactly one row changed. Fix the underlying cause, run status, then retry.
Never modify an applied migration's checksum to bypass validation. For an invalid
Down in an applied file, restore and seek a reviewed forward-fix migration instead.
There is intentionally no automatic force/repair command that could hide data loss.

## Supported SQL and application code

Use ordinary server-side DDL/DML. Multiple statements, backtick identifiers,
quoted strings, # and standard comments are supported. Do not change databases,
autocommit or session settings in migration SQL. Explicit transaction/session
control, mysql-client DELIMITER commands, executable /*! */ comments and nested
comments are rejected. Stored programs/dump scripts are outside this initial scope.
The session must use autocommit=1 without ANSI_QUOTES or NO_BACKSLASH_ESCAPES modes.
Migration files are trusted code, not an SQL sandbox; they must not manipulate
Graft's history or locks (directly or via stored routines).

The HTTP framework does not choose or open an application database. In application
code use database/sql and github.com/go-sql-driver/mysql, MySQL `?` placeholders,
and context-aware queries. Selecting DATABASE_DRIVER only configures CLI migrations;
build/publish and HTTP routing remain database-agnostic. No Go or native MySQL client
is needed on the target server for compiled apps using this pure-Go driver.

For library use, load with `migration.LoadDialect(fs, dir, "mysql")` and run with
`migration.Runner{Store: mysql.New(db)}` from migration/mysql. Configure that pool
with parseTime=true, loc=UTC and multiStatements=true; the caller owns the pool.

## Tests and references

Set GRAFT_TEST_MYSQL_DSN to an admin DSN and run `go test ./migration/mysql`.
Tests create/drop only uniquely named test databases, never migrate the DSN's
database. CI and release verification include a MySQL 8.4 service.

- [MySQL implicit commits](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html)
- [MySQL named locks](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)
- [Go MySQL driver configuration](https://github.com/go-sql-driver/mysql/tree/v1.10.1)
