@FR-014 @NFR-A11Y-004
Feature: Time-series panel in Explore
  As a dashboard editor
  I want to see a query result as lines over time
  So that I can spot trends and gaps before building a dashboard panel

  Background:
    Given I open the app at "/explore?ds=samples&path=cpu.csv"
    And I press Run

  Scenario: Switch the result to a time-series chart
    When I choose the visualization "Time series"
    Then the URL has the query parameter "view" set to "timeseries"
    And a time-series chart is shown with 3 series
    And the chart's accessible name mentions "web-1, web-2, db-1"
    And the legend lists "web-1", "web-2" and "db-1"
    And the page has no serious or critical accessibility violations

  Scenario: A null value is drawn as a gap, not as zero
    When I choose the visualization "Time series"
    Then the series "db-1" is drawn in 2 segments

  Scenario: Read values with the keyboard
    When I choose the visualization "Time series"
    And I focus the chart plot
    And I press "Home" in the chart plot
    Then the chart reading mentions "web-1" and "16.4"
    When I press "End" in the chart plot
    Then the chart reading mentions "db-1" and "51.8"

  Scenario: Hide a series from the legend
    When I choose the visualization "Time series"
    And I toggle the legend entry "web-2"
    Then the legend entry "web-2" is not pressed
    And a time-series chart is shown with 2 series

  Scenario: The view is restored from the URL without running the query
    Given I open the app at "/explore?ds=samples&path=cpu.csv&view=timeseries"
    Then the visualization "Time series" is selected
    And no result is shown

  Scenario: A frame without a time field explains why it cannot be charted
    Given I open the app at "/explore?ds=samples&path=hosts.tsv&view=timeseries"
    And I press Run
    Then the chart panel explains that it needs a time field and a number field
    When I choose the visualization "Table"
    Then the result table has 3 rows

  @NFR-A11Y-001
  Scenario: The chart works on a phone-width screen
    Given the viewport is 390 by 844 pixels
    When I choose the visualization "Time series"
    Then a time-series chart is shown with 3 series
    And the page does not scroll sideways
