@FR-010 @NFR-PERF-003
Feature: Common data-frame model
  As a panel author
  I want every data source to produce the same columnar frame
  So that any panel can render data from any source

  Scenario: Build a frame and encode it to the wire format
    Given a frame "cpu" with fields:
      | name  | type   |
      | time  | time   |
      | value | number |
      | host  | string |
    When I append the rows:
      | time                 | value | host |
      | 2026-11-07T10:00:00Z | 1.5   | a    |
      | 2026-11-07T10:01:00Z | null  | b    |
    Then the frame has 2 rows
    And the wire JSON field "schema.fields.1.type" is "number"
    And the wire JSON field "data.values.0.0" is the number 1794045600000
    And the wire JSON field "data.values.1.1" is null

  Scenario: A value of the wrong type is rejected
    Given a frame "cpu" with fields:
      | name  | type   |
      | value | number |
    When I append the value "not-a-number" to field "value"
    Then the append fails with an error mentioning "value"
    And the frame has 0 rows

  Scenario: Rows beyond the row limit are not added
    Given a frame "big" with fields:
      | name  | type   |
      | value | number |
    And a row limit of 3
    When I append 5 numeric rows
    Then the frame has 3 rows
    And the frame is marked truncated
    And the frame has a warning notice mentioning "row limit"

  Scenario: Rows beyond the byte limit are not added
    Given a frame "big" with fields:
      | name | type   |
      | text | string |
    And a byte limit of 100
    When I append 10 rows of 20-character text
    Then the frame has fewer than 10 rows
    And the frame is marked truncated
    And the frame has a warning notice mentioning "byte limit"

  Scenario: Malformed wire payloads are rejected
    When I decode the wire payload '{"schema":{"name":"x","fields":[{"name":"a","type":"number"},{"name":"b","type":"string"}]},"data":{"values":[[1,2],["x"]]}}'
    Then decoding fails with an error mentioning "length"

  Scenario: Wire payloads round-trip losslessly
    Given a frame "cpu" with fields:
      | name  | type   |
      | time  | time   |
      | value | number |
      | ok    | bool   |
    When I append the rows:
      | time                 | value | ok    |
      | 2026-11-07T10:00:00Z | 42    | true  |
      | 2026-11-07T10:01:00Z | -0.25 | false |
    Then decoding the frame's wire JSON yields an equal frame
