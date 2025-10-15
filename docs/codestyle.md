# Style

- Do not make useless helper functions - inline functionality unless the function is reusable or composable.

## Unit Tests

Write unit tests exclusively for complex, critical, or fragile functions. This includes:
* Logic for parsing, calculations, or intricate data type conversions (e.g., `int64` to `[]byte`).
* Functions with non-standard implementations due to performance optimizations.

Do NOT test trivial code or functionalities fully handled by trusted libraries or the operating system. Prioritize high-impact tests for error-prone components over broad code coverage.

## Go

Wrap errors using `fmt.Errorf` with `%w` to add contextual information about data origin or execution failure.

Write modern Go code:
* Don't add imports upfront. Add them as needed.
* Use `any` instead of `interface{}`.
* Leverage Go's automatic loop variable capture; explicit copying is unnecessary.
* Don't ever use the deprecated `ioutil` package.
* For struct literals, prefer unkeyed fields. This ensures compile-time errors if new fields are added, preventing missed initializations.
* Write comments:
  - Only explain code that is not self-evident.
  - Separate large functions into multiple steps using comments as a step-title.
* For big functions, decide if you want to split it into smaller functions. If slitting it, would make it harder to read because of shared state between the functions, don't do it.
* I prefer keeping the scope of variables as small as possible. Use patterns like `if err := ...; err != nil { return err }` instead of `err := ...; if err != nil { return err }`. Don't forget the error wrapping!

Naming is used for accessibility in Go (public vs private). Think about private (camelCase) and public (PascalCase).

This code is mainly a web server, so you need to ensure that every block (like a mutex Lock) is properly unlocked. Avoid async code if possible unless it is needed for performance.
- Use a `sync.WaitGroup` to wait for all async operations to finish.
- Use a `sync.Mutex` or `sync.RWMutex` when repeated access to shared data is needed.
- Use a `chan` when you periodically need to send data to another goroutine. This pattern is the most dangerous (because it can panic), avoid it.

The database is PostgreSQL with queries generated using sqlc.
Database schema is defined using migrations in `./db/migrations/*.sql` and queries are in `./db/query/*.sql`.
Use `@named` parameters if otherwise it would not be clear what the generated Go arguments are. Otherwise use `$1` positional parameters.
The `db` package contains all that is related to the database. Use `db.Q` to execute queries.
Use `tx, err := db.Q.Begin(context.Context)` to start a transaction. Always use `defer tx.Close()` to ensure that the transaction is properly closed (rolled back if it wasn't committed). And commit using `err = tx.Commit(context.Context)`. A transaction has access to the same query methods as `db.Q` does.
The interface `db.Querier` implements all the queries. `db.Q` is of type `*db.Queries` which implements `db.Querier`. The transaction is of type `*db.TxQueries` which also implements `db.Querier` but additionally can be committed.

gRPC is used for comunicatin between server and Clint. Both server and client are in the same binary but will most likely run on different machines.

The `env` package is used for working with env variables on the server. It ensures that it's values are valid. Never do any checks! Always assume that the values in the `env` package are correct and usable! Only use this package on the server, never on the client.

### Auth

Make sure the same encoding functions are used whenever a personal access token is encoded or decoded.