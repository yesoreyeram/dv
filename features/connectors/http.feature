@FR-040 @FR-071 @FR-072 @FR-073 @FR-079 @NFR-SEC-005 @NFR-SEC-020 @NFR-SEC-021
Feature: Generic HTTP connector
  As a dashboard editor
  I want to query any JSON HTTP API through a connection the operator configured
  So that SaaS, cloud and observability data become tables I can visualise and combine

  Background:
    Given a fake upstream API
    And outbound connections to the fake upstream are allowed

  Scenario: Bearer authentication, Link-header pagination and a jq row selector
    Given an http connection "gh" to the fake upstream with bearer token from env "DV_TEST_GH_TOKEN" set to "s3cr3t-token"
    When I run the http query on "gh":
      """
      {"path": "/github/repos/acme/web/issues", "params": [{"name": "per_page", "value": "2"}],
       "pagination": {"type": "linkHeader", "maxPages": 10},
       "rows": ".[] | {number, title, state, author: .user.login}"}
      """
    Then the query result status is 200
    And the result frame has 5 rows
    And the result frame fields are "number, title, state, author"
    And the result frame field "author" has values "ana, ben, ana, cy, ben"
    And the fake upstream received 3 requests with "Authorization" set to "Bearer s3cr3t-token"
    And the response body does not contain "s3cr3t-token"

  Scenario: Cursor pagination stops when the cursor is empty
    Given an http connection "vercel" to the fake upstream with bearer token from env "DV_TEST_VERCEL_TOKEN" set to "v-token"
    When I run the http query on "vercel":
      """
      {"path": "/vercel/v6/deployments",
       "pagination": {"type": "cursor", "cursorSelector": ".pagination.next", "cursorParam": "until"},
       "rows": ".deployments[]",
       "columns": [{"name": "name", "selector": ".name"}, {"name": "created", "selector": ".created / 1000 | todate", "type": "time"}]}
      """
    Then the query result status is 200
    And the result frame has 4 rows
    And the result frame fields are "name, created"
    And the result frame field "created" has type "time"

  Scenario: Offset pagination with two API-key headers
    Given an http connection "dd" to the fake upstream with API-key headers "DD-API-KEY" and "DD-APPLICATION-KEY"
    When I run the http query on "dd":
      """
      {"path": "/datadog/api/v2/logs",
       "pagination": {"type": "offset", "offsetParam": "offset", "limitParam": "limit", "limit": 3},
       "rows": ".data[] | .attributes"}
      """
    Then the query result status is 200
    And the result frame has 7 rows

  Scenario: Next-URL pagination from the response body
    Given an http connection "te" to the fake upstream with no authentication
    When I run the http query on "te":
      """
      {"path": "/thousandeyes/v7/tests",
       "pagination": {"type": "nextUrl", "nextUrlSelector": "._links.next.href"},
       "rows": ".tests[]"}
      """
    Then the query result status is 200
    And the result frame has 5 rows

  Scenario: Page-number pagination stops on a short page
    Given an http connection "cf" to the fake upstream with no authentication
    When I run the http query on "cf":
      """
      {"path": "/cloudflare/client/v4/zones",
       "pagination": {"type": "page", "pageParam": "page", "sizeParam": "per_page", "size": 2},
       "rows": ".result[]"}
      """
    Then the query result status is 200
    And the result frame has 5 rows

  Scenario: A pagination budget marks the frame truncated
    Given an http connection "gh" to the fake upstream with bearer token from env "DV_TEST_GH_TOKEN" set to "s3cr3t-token"
    When I run the http query on "gh":
      """
      {"path": "/github/repos/acme/web/issues", "params": [{"name": "per_page", "value": "2"}],
       "pagination": {"type": "linkHeader", "maxPages": 2}}
      """
    Then the query result status is 200
    And the result frame has 4 rows
    And the result frame is truncated with a notice mentioning "maxPages"

  Scenario: Columns zip parallel arrays like CloudWatch or Dynatrace
    Given an http connection "dt" to the fake upstream with no authentication
    When I run the http query on "dt":
      """
      {"path": "/dynatrace/api/v2/metrics/query",
       "rows": ".result[0].data[0] | [.timestamps, .values] | transpose[] | {time: (.[0] / 1000 | todate), value: .[1]}",
       "fieldTypes": {"time": "time"}}
      """
    Then the query result status is 200
    And the result frame has 3 rows
    And the result frame field "value" has values "1.5, 2, 2.5"

  Scenario: The SSRF guard refuses a loopback destination that is not exempted
    Given outbound connections to the fake upstream are not allowed
    And an http connection "gh" to the fake upstream with no authentication
    When I run the http query on "gh":
      """
      {"path": "/github/repos/acme/web/issues"}
      """
    Then the query result status is 403
    And the query error code is "destination_not_allowed"
    And the query error mentions "loopback"
    And the fake upstream received 0 requests

  Scenario: A next-page link to another origin is refused and credentials are not sent there
    Given an http connection "evil" to the fake upstream with bearer token from env "DV_TEST_EVIL_TOKEN" set to "keep-me"
    When I run the http query on "evil":
      """
      {"path": "/evil/redirect-pagination", "pagination": {"type": "linkHeader"}}
      """
    Then the query result status is 502
    And the query error mentions "leaves the connection's base URL"

  Scenario Outline: Queries cannot escape the base URL or inject headers
    Given an http connection "gh" to the fake upstream with bearer token from env "DV_TEST_GH_TOKEN" set to "s3cr3t-token"
    When I run the http query on "gh":
      """
      <query>
      """
    Then the query result status is 400
    And the query error code is "invalid_query"
    And the query error mentions "<message>"
    And the fake upstream received 0 requests

    Examples:
      | query                                                                      | message                   |
      | {"path": "http://169.254.169.254/latest/meta-data"}                        | relative path             |
      | {"path": "//example.org/x"}                                                | relative path             |
      | {"path": "/github/../../admin"}                                            | outside the base path     |
      | {"path": "/github", "headers": [{"name": "Authorization", "value": "x"}]}  | may not be set            |
      | {"path": "/github", "headers": [{"name": "X-A", "value": "a\u000d\u000aHost: b"}]} | control character         |
      | {"path": "/github", "rows": "env.HOME"}                                    | not supported             |

  Scenario: A selector that exceeds its evaluation budget is refused, not run unbounded
    Given an http connection "te" to the fake upstream with no authentication
    When I run the http query on "te":
      """
      {"path": "/echo", "rows": "[range(100000000)]"}
      """
    Then the query result status is 400
    And the query error code is "invalid_query"
    And the query error mentions "evaluation budget"

  Scenario: Upstream authentication failures are reported without upstream details
    Given an http connection "gh" to the fake upstream with bearer token from env "DV_TEST_GH_TOKEN" set to "wrong"
    When I run the http query on "gh":
      """
      {"path": "/github/repos/acme/web/issues"}
      """
    Then the query result status is 502
    And the query error code is "upstream_auth_failed"
    And the response body does not contain "Bad credentials"

  Scenario: Time-range macros fill query parameters
    Given an http connection "te" to the fake upstream with no authentication
    When I run the http query on "te" from "2026-11-01T00:00:00Z" to "2026-11-02T00:00:00Z":
      """
      {"path": "/echo", "params": [{"name": "from", "value": "${__from}"}, {"name": "to", "value": "${__to:iso}"}]}
      """
    Then the query result status is 200
    And the result frame field "from" has values "1793491200000"
    And the result frame field "to" has values "2026-11-02T00:00:00Z"

  Scenario: Plain-text secrets in the provisioning file fail start-up
    When the server starts with an http connection whose secureJsonData token is the plain string "abc"
    Then start-up fails with an error mentioning "{env} or {file} references"
    And the start-up error does not contain "abc"
