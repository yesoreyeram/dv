@FR-011 @NFR-SEC-006 @NFR-PERF-003
Feature: CSV and TSV files from a configured root
  As a dashboard editor
  I want to query CSV and TSV files that an operator has exposed to dv
  So that I can visualise file data without dv reading anything else on the server

  Background:
    Given a file root "samples"
    And a file source instance using root "samples"

  Scenario: Query a CSV file and get typed fields
    Given the file "sales.csv" contains:
      """
      time,region,amount,paid
      2026-11-01T00:00:00Z,emea,120.5,true
      2026-11-02T00:00:00Z,apac,98,false
      """
    When I query the file "sales.csv"
    Then the query succeeds with 1 frame named "sales.csv"
    And the frame has 2 rows
    And the frame fields are:
      | name   | type   |
      | time   | time   |
      | region | string |
      | amount | number |
      | paid   | bool   |
    And the frame meta "executedQuery" is "samples:sales.csv"

  Scenario: The format is chosen from the file extension
    Given the file "data/hosts.TSV" contains the tab-separated rows:
      | host | cpu |
      | a    | 1   |
      | b    | 2   |
    When I query the file "data/hosts.TSV"
    Then the query succeeds with 1 frame named "hosts.TSV"
    And the frame field "cpu" has type "number"

  Scenario: A type override turns unparsable values into nulls with a warning
    Given the file "mixed.csv" contains:
      """
      code
      10
      n/a
      30
      """
    When I query the file "mixed.csv" with field type "code" set to "number"
    Then the frame field "code" has type "number"
    And the frame field "code" has values "10, null, 30"
    And the frame has a warning notice mentioning "1 value"

  Scenario: Rows beyond the row limit are not returned
    Given the file "big.csv" contains 50 numeric rows
    And a row limit of 10
    When I query the file "big.csv"
    Then the frame has 10 rows
    And the frame is marked truncated

  Scenario: Rows dropped by the byte limit do not change inferred types
    Given the file "codes.csv" contains:
      """
      v
      1
      2
      x
      """
    And a byte limit of 60
    When I query the file "codes.csv"
    Then the frame field "v" has type "number"
    And the frame field "v" has values "1, 2"
    And the frame is marked truncated
    And the frame has a warning notice mentioning "byte limit"

  Scenario: A query that sets no limits is still bounded by the default limits
    Given the file "big.csv" contains 50 numeric rows
    And the default row limit for queries is 20
    When I query the file "big.csv"
    Then the frame has 20 rows
    And the frame is marked truncated

  Scenario: An explicit row limit takes precedence over the default
    Given the file "big.csv" contains 50 numeric rows
    And the default row limit for queries is 20
    And a row limit of 30
    When I query the file "big.csv"
    Then the frame has 30 rows

  Scenario Outline: Paths that could leave the root are rejected before any file access
    When I query the file "<path>"
    Then the query fails as "invalid path"

    Examples:
      | path              |
      | ../secret.csv     |
      | /etc/passwd       |
      | a/../../x.csv     |
      | dir\..\x.csv      |
      |                   |

  Scenario Outline: Hidden files and directories are never read, even when they exist
    Given the file "<path>" contains:
      """
      KEY,value
      token,s3cr3t
      """
    When I query the file "<path>" as CSV
    Then the query fails as "invalid path"
    And the error does not reveal the root directory

    Examples:
      | path                 |
      | .env                 |
      | .git/config          |
      | data/.secret.csv     |
      | .aws/credentials.csv |

  Scenario: A leading "./" is not a hidden entry
    Given the file "sales.csv" contains:
      """
      amount
      1
      """
    When I query the file "./sales.csv"
    Then the query succeeds with 1 frame named "sales.csv"

  Scenario: Dots inside a name are allowed
    Given the file "v1.2/report.final.csv" contains:
      """
      amount
      1
      """
    When I query the file "v1.2/report.final.csv"
    Then the query succeeds with 1 frame named "report.final.csv"

  Scenario Outline: Paths with bidirectional control characters are rejected
    When I query the file with the escaped path "<path>"
    Then the query fails as "invalid path"

    Examples:
      | path                 |
      | invoice\u202Ecsv.exe |
      | a\u2066b.csv         |
      | x\u200F.csv          |

  Scenario: A symlink that points outside the root is refused
    Given a file "secret.csv" outside the root
    And the file "link.csv" is a symlink to that outside file
    When I query the file "link.csv"
    Then the query fails as "invalid path"
    And the error does not reveal the root directory

  Scenario: Missing files are reported without revealing the server path
    When I query the file "nope.csv"
    Then the query fails as "not found"
    And the error does not reveal the root directory

  Scenario: Files larger than the size cap are refused
    Given a file size cap of 64 bytes
    And the file "large.csv" contains 100 numeric rows
    When I query the file "large.csv"
    Then the query fails as "too large"

  Scenario: Directories are not files
    Given the directory "folder.csv" exists
    When I query the file "folder.csv"
    Then the query fails as "not a regular file"

  Scenario: Unknown extensions are rejected
    Given the file "notes.txt" contains:
      """
      hello
      """
    When I query the file "notes.txt"
    Then the query fails as "unsupported format"

  Scenario: A source instance must reference a configured root
    When I validate a file source instance using root "elsewhere"
    Then validation fails with an error mentioning "elsewhere"

  Scenario: Ragged rows are reported with their line number
    Given the file "ragged.csv" contains:
      """
      a,b
      1,2
      3
      """
    When I query the file "ragged.csv"
    Then the query fails with an error mentioning "line 3"
