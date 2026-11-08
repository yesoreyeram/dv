@FR-018 @FR-013
Feature: Explore data from a provisioned source
  As a dashboard editor
  I want to run an ad-hoc query and inspect the result as a table
  So that I can check what a data source returns before building panels

  Scenario: Run a query and see the result as a table
    Given I open the app at "/explore"
    Then the page heading is "Explore"
    When I choose the data source "Sample files"
    And I enter the path "sales.csv"
    And I press Run
    Then the result table has 6 rows
    And the result table has the column "revenue"
    And the query statistics mention "samples:sales.csv"
    And the page has no serious or critical accessibility violations

  Scenario: Run a query with the keyboard
    Given I open the app at "/explore"
    When I enter the path "hosts.tsv"
    And I press Control+Enter in the path field
    Then the result table has 3 rows

  Scenario: Sort a column
    Given I open the app at "/explore?ds=samples&path=sales.csv"
    And I press Run
    When I sort by the column "orders"
    Then the column "orders" is sorted "ascending"
    And the first cell of the column "orders" is "94"
    When I sort by the column "orders"
    Then the column "orders" is sorted "descending"
    And the first cell of the column "orders" is "141"

  Scenario: The query is kept in the URL but only runs on request
    Given I open the app at "/explore?ds=samples&path=hosts.tsv&maxRows=2"
    Then the path field contains "hosts.tsv"
    And no result is shown
    When I press Run
    Then the result table has 2 rows

  Scenario: A missing file shows the error from the server
    Given I open the app at "/explore"
    When I enter the path "missing.csv"
    And I press Run
    Then I see an error alert mentioning "not_found"

  Scenario: An empty path is reported inline and nothing is sent
    Given I open the app at "/explore"
    When I press Run
    Then the path field is marked invalid
    And no result is shown
