@FR-005 @NFR-A11Y-001 @NFR-A11Y-002
Feature: Application shell
  As a user
  I want a consistent, accessible application frame
  So that I can find my way around the platform

  Scenario: Primary navigation is available
    Given I open the app at "/"
    Then I see the primary navigation with links "Home, Explore, Dashboards, Data sources, API reference"
    And the navigation link "Home" is marked as the current page

  Scenario: Navigating between sections
    Given I open the app at "/"
    When I click the navigation link "Explore"
    Then the page heading is "Explore"
    And the navigation link "Explore" is marked as the current page
    And I see the text "Planned for milestone M2"

  Scenario: Skip link is the first focusable element
    Given I open the app at "/"
    When I press "Tab"
    Then the focused element is the link "Skip to main content"

  Scenario: Theme can be toggled
    Given I open the app at "/" with color scheme "light"
    When I click the button "Switch to dark theme"
    Then the document uses the "dark" theme

  Scenario: Unknown routes show a not-found page
    Given I open the app at "/no/such/page"
    Then the page heading is "Page not found"

  Scenario Outline: Pages have no serious accessibility violations
    Given I open the app at "<path>" with color scheme "<scheme>"
    Then the page has no serious or critical accessibility violations

    Examples:
      | path           | scheme |
      | /              | light  |
      | /              | dark   |
      | /data-sources  | light  |
      | /api-reference | dark   |
