@FR-040 @FR-072 @FR-073 @FR-018
Feature: Query an HTTP API connection in Explore
  As a dashboard editor
  I want to query a JSON API connection with a jq selector and pagination
  So that SaaS and observability data shows up as a table or a chart

  Background:
    Given I open the app at "/explore"

  Scenario: Page through a GitHub-style API and shape the rows with jq
    When I choose the data source "GitHub (mock)"
    Then the file format fields are hidden
    When I enter the path "/repos/acme/web/issues"
    And I enter the rows selector ".[] | {number, title, state, author: .user.login}"
    And I enter the connection options:
      """
      {"params": [{"name": "per_page", "value": "2"}], "pagination": {"type": "linkHeader"}}
      """
    And I press Run
    Then the result table has 5 rows
    And the result table has the column "author"
    And the query statistics mention "GET /api/github/repos/acme/web/issues"
    And the query statistics mention "3 upstream pages"
    And the page has no serious or critical accessibility violations

  Scenario: Offset pagination with API-key headers
    When I choose the data source "Datadog (mock)"
    And I enter the path "/api/v2/logs"
    And I enter the rows selector ".data[] | .attributes"
    And I enter the connection options:
      """
      {"pagination": {"type": "offset", "limit": 3}}
      """
    And I press Run
    Then the result table has 7 rows
    And the result table has the column "service"

  Scenario: Zip a metrics API into a time-series chart
    When I choose the data source "Metrics API (mock)"
    And I enter the path "/cpu"
    And I enter the rows selector:
      """
      [.series[] | .points] | transpose[] | {time: (.[0][0] / 1000 | todate), "web-1": .[0][1], "web-2": .[1][1], "db-1": .[2][1]}
      """
    And I press Run
    And I choose the visualization "Time series"
    Then a time-series chart is shown with 3 series

  Scenario: Invalid connection options are caught before anything runs
    When I choose the data source "GitHub (mock)"
    And I enter the path "/repos/acme/web/issues"
    And I enter the connection options:
      """
      {"path": "/somewhere-else"}
      """
    And I press Run
    Then the connection options field is marked invalid
    And no result is shown

  Scenario: An upstream error is shown without upstream details
    When I choose the data source "GitHub (mock)"
    And I enter the path "/repos/acme/missing"
    And I press Run
    Then I see an error alert mentioning "404"
