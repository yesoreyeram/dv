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
    And the query statistics show the query time and the total request time
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

  @NFR-SEC-018
  Scenario: Download the result as CSV that is safe to open in a spreadsheet
    Given I open the app at "/explore?ds=samples&path=formulas.csv"
    And I press Run
    When I download the result as CSV
    Then the downloaded file is named "formulas.csv"
    And the downloaded CSV has 4 lines
    And the downloaded CSV contains the line "label,delta"
    And the downloaded CSV contains the line "'=1+1,-3"
    And the downloaded CSV contains the line "'@SUM(A1:A2),7"
    And the downloaded CSV contains the line "plain,0"

  @FR-012
  Scenario: Query a nested JSON file with a rows path and flattened fields
    Given I open the app at "/explore"
    When I enter the path "services.json"
    And I enter the rows path "data.services"
    And I check "Flatten nested objects"
    And I press Run
    Then the result table has 4 rows
    And the result table has the column "owner.team"
    And the page has no serious or critical accessibility violations

  @FR-012
  Scenario: NDJSON files offer only the flatten option
    Given I open the app at "/explore?ds=samples&path=events.ndjson"
    Then there is no rows path field
    When I press Run
    Then the result table has 5 rows

  @FR-012
  Scenario: A malformed rows path is reported inline and nothing is sent
    Given I open the app at "/explore?ds=samples&path=services.json"
    When I enter the rows path "data[0]"
    And I press Run
    Then the rows path field is marked invalid
    And no result is shown

  @NFR-A11Y-005
  Scenario: A wide result shows which edge hides columns and scrolls with the keyboard
    Given the viewport is 390 by 844 pixels
    And I open the app at "/explore?ds=samples&path=services.json&rowsPath=data.services&flatten=1"
    When I press Run
    Then the result table has 4 rows
    And the result region shows hidden columns at the end only
    And the page does not scroll sideways
    When I scroll the result region to the end with the keyboard
    Then the result region shows hidden columns at the start only
    And the page has no serious or critical accessibility violations

  @NFR-A11Y-005
  Scenario: A narrow result shows no edge indicator
    Given I open the app at "/explore?ds=samples&path=hosts.tsv"
    When I press Run
    Then the result region shows no hidden columns

  @FR-018 @NFR-A11Y-006
  Scenario: Resize the query editor with the keyboard and keep the width
    Given I open the app at "/explore"
    Then the query editor is 360 pixels wide
    When I focus the splitter
    And I press "ArrowRight"
    Then the query editor is 376 pixels wide
    When I press "End"
    Then the query editor is 640 pixels wide
    When I reload the page
    Then the query editor is 640 pixels wide
    When I focus the splitter
    And I press "Enter"
    Then the query editor is 360 pixels wide

  @FR-018
  Scenario: Drag the splitter
    Given I open the app at "/explore"
    When I drag the splitter 100 pixels to the right
    Then the query editor is 460 pixels wide

  @FR-018 @NFR-A11Y-005
  Scenario: On a phone the editor and the result stack without a splitter
    Given the viewport is 390 by 844 pixels
    And I open the app at "/explore"
    Then the splitter is not shown

  @FR-018 @NFR-SEC-019
  Scenario: Query history restores a query without running it
    Given I open the app at "/explore"
    When I enter the path "sales.csv"
    And I press Run
    Then the result table has 6 rows
    When I enter the path "hosts.tsv"
    And I press Run
    Then the result table has 3 rows
    When I open the query history
    Then the query history lists "hosts.tsv, sales.csv"
    When I choose the history entry "sales.csv"
    Then the path field contains "sales.csv"
    And no result is shown
    When I open the query history
    And I clear the query history
    Then the query history is empty

  @FR-018 @FR-008
  Scenario: A recent query is offered in the command palette
    Given I open the app at "/explore"
    When I enter the path "cpu.csv"
    And I press Run
    Then the result table has 24 rows
    When I click the navigation link "Home"
    And I press "Control+k"
    And I type "cpu.csv" in the command palette
    And I press "Enter"
    Then the page heading is "Explore"
    And the path field contains "cpu.csv"
    And no result is shown

