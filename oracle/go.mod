module github.com/sqlc-dev/oliphant/oracle

go 1.24

require github.com/pganalyze/pg_query_go/v6 v6.2.2

require google.golang.org/protobuf v1.33.0 // indirect

// No pg_query_go release ships libpg_query 18 yet; the oracle builds against
// a locally generated pg_query_go + libpg_query 18.0.0 tree. Run
// ./update-pg-query-go.sh to populate it before building.
replace github.com/pganalyze/pg_query_go/v6 => ./pg_query_go
