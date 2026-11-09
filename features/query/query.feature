@FR-016 @FR-017 @NFR-SEC-016 @NFR-PERF-003 @NFR-SEC-009
Feature: Query API over provisioned data sources
  As a dashboard editor
  I want to run refId-tagged queries against data sources the operator configured
  So that panels can render frames, including partial results when some queries fail

  Background:
    Given a file root "samples" containing "sales.csv":
      """
      time,region,amount
      2026-11-01T00:00:00Z,emea,120.5
      2026-11-02T00:00:00Z,apac,98
      2026-11-03T00:00:00Z,amer,77
      """
    And a provisioned data source "files" of type "file" named "Sample files" with root "samples"

  Scenario: A query returns frames keyed by refId
    Given the query API is enabled
    When I query "files" with refId "A" and path "sales.csv"
    Then the response status should be 200
    And result "A" has status 200 and 1 frame with 3 rows
    And result "A" frame 0 meta "executedQuery" is "samples:sales.csv"

  Scenario: Several queries run in one request
    Given the query API is enabled
    When I send the queries:
      | refId | uid   | path      |
      | A     | files | sales.csv |
      | B     | files | sales.csv |
    Then the response status should be 200
    And result "A" has status 200 and 1 frame with 3 rows
    And result "B" has status 200 and 1 frame with 3 rows

  Scenario: Every result reports how long its query took
    Given the query API is enabled
    When I send the queries:
      | refId | uid   | path        |
      | A     | files | sales.csv   |
      | B     | files | missing.csv |
    Then result "A" reports a durationMs of at least 0
    And result "B" reports a durationMs of at least 0

  Scenario: Partial failure answers 207 with a per-query error
    Given the query API is enabled
    When I send the queries:
      | refId | uid   | path        |
      | A     | files | sales.csv   |
      | B     | files | missing.csv |
    Then the response status should be 207
    And result "A" has status 200 and 1 frame with 3 rows
    And result "B" has status 404 and error code "not_found"

  Scenario Outline: Query errors map to stable statuses and codes
    Given the query API is enabled
    And the file "<file>" in root "samples" contains "<content>"
    When I query "<uid>" with refId "A" and path "<path>"
    Then the response status should be <status>
    And result "A" has status <status> and error code "<code>"
    And the response body does not contain the root directory

    Examples:
      | uid     | path          | file       | content | status | code                 |
      | nope    | sales.csv     | x.csv      | a       | 404    | datasource_not_found |
      | files   | ../etc/passwd | x.csv      | a       | 400    | invalid_query        |
      | files   | .env          | x.csv      | a       | 400    | invalid_query        |
      | files   | missing.csv   | x.csv      | a       | 404    | not_found            |
      | files   | notes.txt     | notes.txt  | hello   | 415    | unsupported_format   |
      | files   | dir.csv       | dir.csv/a.csv | a    | 400    | invalid_query        |
      | files   | bad.csv       | bad.csv    | a,b\n1   | 422    | decode_failed        |
      | files   | empty.csv     | empty.csv  |         | 422    | decode_failed        |

  Scenario: Invalid format options are a client error
    Given the query API is enabled
    When I query "files" with refId "A" and the query body {"path":"sales.csv","options":{"delimiter":";;"}}
    Then the response status should be 400
    And result "A" has status 400 and error code "invalid_query"

  Scenario: A file over the size limit is 413
    Given the query API is enabled
    And the file size limit is 16 bytes
    When I query "files" with refId "A" and path "sales.csv"
    Then the response status should be 413
    And result "A" has status 413 and error code "too_large"

  Scenario: The host row limit applies and a query can only lower it
    Given the query API is enabled
    And the host query row limit is 2
    When I query "files" with refId "A" and path "sales.csv" and maxRows 50
    Then result "A" has status 200 and 1 frame with 2 rows
    And result "A" frame 0 is marked truncated
    When I query "files" with refId "A" and path "sales.csv" and maxRows 1
    Then result "A" has status 200 and 1 frame with 1 rows

  Scenario Outline: Invalid requests are rejected before any query runs
    Given the query API is enabled
    When I POST to "/api/v1/query" with content type "<type>" and body:
      """
      <body>
      """
    Then the response status should be <status>
    And the JSON field "error.code" should be "<code>"

    Examples:
      | type             | body                                                                                                 | status | code                   |
      | text/plain       | {"queries":[{"refId":"A","datasource":{"uid":"files"},"query":{"path":"sales.csv"}}]}                | 415    | unsupported_media_type |
      | application/json | {"queries":[]}                                                                                       | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{"uid":"files"}}],"extra":1}                                   | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{"uid":"files"}},{"refId":"A","datasource":{"uid":"files"}}]}  | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"bad id","datasource":{"uid":"files"}}]}                                        | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{}}]}                                                          | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{"uid":"files"}}]} {}                                          | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{"uid":"files"}}],"from":"2026-11-02T00:00:00Z"}               | 400    | invalid_request        |
      | application/json | {"queries":[{"refId":"A","datasource":{"uid":"files"}}],"from":"2026-11-03T00:00:00Z","to":"2026-11-01T00:00:00Z"} | 400 | invalid_request |

  Scenario: More than 20 queries are rejected
    Given the query API is enabled
    When I send 21 queries to "files" for path "sales.csv"
    Then the response status should be 400
    And the JSON field "error.code" should be "invalid_request"

  Scenario: The data-source listing returns identity only, never settings
    Given the query API is enabled
    When I send a GET request to "/api/v1/datasources"
    Then the response status should be 200
    And the JSON field "datasources.0.uid" should be "files"
    And the JSON field "datasources.0.type" should be "file"
    And the JSON field "datasources.0.name" should be "Sample files"
    And the response body does not contain "jsonData"
    And the response body does not contain the root directory

  Scenario Outline: The endpoints do not exist while the query API is disabled
    When I send a <method> request to "<path>"
    Then the response status should be 404
    And the JSON field "error.code" should be "not_found"

    Examples:
      | method | path                |
      | GET    | /api/v1/datasources |
      | POST   | /api/v1/query       |

  # B-062: one request can never retain more than queryMaxBytes of frames (FR-016 AC9).
  Scenario: The byte limit is a budget shared by all queries of one request
    Given the query API is enabled
    And a file "big.csv" in root "samples" with 200 rows
    And the host query byte limit is 16000 bytes
    When I query "files" with refId "A" and path "big.csv"
    Then result "A" has status 200 and 1 frame with 200 rows
    When I send the queries:
      | refId | uid   | path    |
      | A     | files | big.csv |
      | B     | files | big.csv |
    Then the response status should be 200
    And result "A" frame 0 is marked truncated
    And result "B" frame 0 is marked truncated
    And result "A" frame 0 has a notice mentioning "request byte budget"
    And the frames of the response hold at most 16000 bytes

  Scenario: A query that starts after the budget is used up does not run
    Given the query API is enabled
    And a file "big.csv" in root "samples" with 200 rows
    And the host query byte limit is 16000 bytes
    When I send 6 queries to "files" for path "big.csv"
    Then the frames of the response hold at most 16000 bytes
