@FR-012 @NFR-PERF-003 @NFR-SEC-006
Feature: JSON and NDJSON files from a configured root
  As a dashboard editor
  I want to query JSON and NDJSON files that an operator has exposed to dv
  So that API exports and log files can be visualised like CSV files

  Background:
    Given a file root "samples"
    And a file source instance using root "samples"

  Scenario: Query a JSON array of objects and get typed fields
    Given the file "hosts.json" contains:
      """
      [
        {"time": "2026-11-01T00:00:00Z", "host": "a", "cpu": 0.5, "up": true, "tags": ["x"]},
        {"time": "2026-11-02T00:00:00Z", "host": "b", "cpu": 2, "up": false, "tags": []}
      ]
      """
    When I query the file "hosts.json"
    Then the query succeeds with 1 frame named "hosts.json"
    And the frame has 2 rows
    And the frame fields are:
      | name | type   |
      | time | time   |
      | host | string |
      | cpu  | number |
      | up   | bool   |
      | tags | json   |

  Scenario: Rows are read from a nested array named by rowsPath
    Given the file "api.json" contains:
      """
      {"meta": {"skip": [1, 2, {"deep": [3]}]}, "data": {"items": [{"id": 1}, {"id": 2}, {"id": 3}]}}
      """
    When I query the file "api.json" with options:
      """
      {"rowsPath": "data.items"}
      """
    Then the frame has 3 rows
    And the frame field "id" has values "1, 2, 3"

  Scenario: A missing rowsPath is a decode error that names the path
    Given the file "api.json" contains:
      """
      {"data": {"items": []}}
      """
    When I query the file "api.json" with options:
      """
      {"rowsPath": "data.rows"}
      """
    Then the query fails as "decode error"
    And the query fails with an error mentioning "data.rows"

  Scenario Outline: Malformed options are rejected
    Given the file "a.json" contains:
      """
      []
      """
    When I query the file "a.json" with options:
      """
      <options>
      """
    Then the query fails as "invalid options"

    Examples:
      | options                       |
      | {"rowsPath": "a..b"}          |
      | {"rowsPath": "a[0]"}          |
      | {"rowsPath": "$.a"}           |
      | {"unknown": true}             |
      | {"flatten": "yes"}            |

  Scenario: Keys missing from some rows are null, and fields keep first-seen order
    Given the file "sparse.json" contains:
      """
      [{"a": 1}, {"b": "x"}, {"a": 3, "b": "y"}]
      """
    When I query the file "sparse.json"
    Then the frame field "a" has values "1, null, 3"
    And the frame field "b" has values "null, x, y"

  Scenario: Nested objects are expanded into dotted fields with flatten
    Given the file "nested.json" contains:
      """
      [{"host": {"name": "a", "geo": {"region": "emea"}}, "cpu": 1}]
      """
    When I query the file "nested.json" with options:
      """
      {"flatten": true}
      """
    Then the frame fields are:
      | name             | type   |
      | host.name        | string |
      | host.geo.region  | string |
      | cpu              | number |

  Scenario: A literal dotted key and a flattened path stay separate fields
    Given the file "clash.json" contains:
      """
      [{"a.b": 1, "a": {"b": 2}}]
      """
    When I query the file "clash.json" with options:
      """
      {"flatten": true}
      """
    Then the frame field "a.b" has values "1"
    And the frame field "a.b (2)" has values "2"

  Scenario: An unsupported field type override is an options error
    Given the file "a.json" contains:
      """
      [{"a": 1}]
      """
    When I query the file "a.json" with field type "a" set to "integer"
    Then the query fails as "invalid options"

  Scenario: A single object over the byte limit is reported as truncated
    Given the file "one.json" contains:
      """
      {"message": "a value longer than the byte limit"}
      """
    And a byte limit of 8
    When I query the file "one.json"
    Then the frame has 0 rows
    And the frame is marked truncated
    And the frame has a warning notice mentioning "byte limit"

  Scenario: Fields that mix numbers and strings are strings
    Given the file "mixed.json" contains:
      """
      [{"v": 1}, {"v": "n/a"}, {"v": true}]
      """
    When I query the file "mixed.json"
    Then the frame field "v" has type "string"
    And the frame field "v" has values "1, n/a, true"

  Scenario: Large integers keep their precision
    Given the file "ids.json" contains:
      """
      [{"id": 9007199254740993}]
      """
    When I query the file "ids.json"
    Then the frame field "id" has values "9007199254740993"

  Scenario: Scalars become a single value field
    Given the file "values.json" contains:
      """
      [1, 2, 3]
      """
    When I query the file "values.json"
    Then the frame field "value" has values "1, 2, 3"

  Scenario: NDJSON files have one row per line
    Given the file "logs/app.ndjson" contains:
      """
      {"level": "info", "msg": "start"}

      {"level": "warn", "msg": "slow"}
      """
    When I query the file "logs/app.ndjson"
    Then the query succeeds with 1 frame named "app.ndjson"
    And the frame field "level" has values "info, warn"

  Scenario: The .jsonl extension selects NDJSON
    Given the file "events.JSONL" contains:
      """
      {"n": 1}
      {"n": 2}
      """
    When I query the file "events.JSONL"
    Then the frame has 2 rows

  Scenario: Rows beyond the row limit are not returned and later input is never parsed
    Given the file "big.json" contains:
      """
      [{"v": 1}, {"v": 2}, {"v": 3}, this is not json
      """
    And a row limit of 2
    When I query the file "big.json"
    Then the frame has 2 rows
    And the frame is marked truncated

  Scenario Outline: Broken input fails without echoing file content
    Given the file "<file>" contains:
      """
      <content>
      """
    When I query the file "<file>"
    Then the query fails as "decode error"
    And the query fails with an error mentioning "<mention>"
    And the error does not mention "s3cr3t"

    Examples:
      | file        | content                       | mention  |
      | bad.json    | [{"k": s3cr3t}]               | offset   |
      | trail.json  | [] {"k": "s3cr3t"}            | trailing |

  Scenario: A broken NDJSON line is reported by row number
    Given the file "bad.ndjson" contains:
      """
      {"ok": 1}
      {"k": s3cr3t}
      """
    When I query the file "bad.ndjson"
    Then the query fails as "decode error"
    And the query fails with an error mentioning "row 2"
    And the error does not mention "s3cr3t"

  Scenario: Nesting deeper than 64 levels is refused
    Given the file "deep.json" contains 70 nested arrays
    When I query the file "deep.json"
    Then the query fails as "decode error"
    And the query fails with an error mentioning "nesting"

  Scenario: More than 1024 fields are refused
    Given the file "wide.json" contains one object with 1025 keys
    When I query the file "wide.json"
    Then the query fails as "decode error"
    And the query fails with an error mentioning "1024"
