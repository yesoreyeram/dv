@FR-015
Feature: Data source catalogue in the UI
  As a dashboard editor
  I want to see which kinds of data sources I can add
  So that I know what the platform can connect to

  Scenario: Data sources page shows the plugin catalogue
    Given I open the app at "/data-sources"
    Then the page heading is "Data sources"
    And I see the plugin catalogue
    And the page has no serious or critical accessibility violations

  @NFR-AUD-004
  Scenario: Data sources page does not claim protections that have not shipped
    Given I open the app at "/data-sources"
    Then I do not see the text "encrypted at rest"
    And I see the text "Encrypted secret storage is planned for milestone M3."
