@FR-005 @FR-008 @NFR-A11Y-006
Feature: Workspace frame, command palette and keyboard shortcuts
  As a daily user
  I want a dense frame I can drive from the keyboard
  So that I reach any page or action without hunting through menus

  Scenario: The top bar names the current page
    Given I open the app at "/data-sources"
    Then the breadcrumb shows the current page "Data sources"

  Scenario: The sidebar foot shows that the server is ready
    Given I open the app at "/"
    Then the sidebar shows the server state "Server ready"

  Scenario: Open the palette with the keyboard and go to a page
    Given I open the app at "/"
    When I press "Control+k"
    Then the command palette is open
    When I type "data sources" in the command palette
    And I press "Enter"
    Then the page heading is "Data sources"
    And the command palette is closed

  Scenario: Closing the palette returns focus to its opener
    Given I open the app at "/"
    When I click the button "Search or jump to"
    Then the command palette is open
    When I press "Escape"
    Then the command palette is closed
    And the focused element is the button "Search or jump to"

  Scenario: Arrow keys move through the commands and wrap
    Given I open the app at "/"
    When I press "Control+k"
    Then the active command is "Go to Home"
    When I press "ArrowDown"
    Then the active command is "Go to Explore"
    When I press "ArrowUp"
    And I press "ArrowUp"
    Then the active command is "Keyboard shortcuts"
    When I press "Tab"
    Then the command palette search field has focus

  Scenario: A search without matches says so
    Given I open the app at "/"
    When I press "Control+k"
    And I type "zzzz" in the command palette
    Then the command palette shows no results for "zzzz"

  Scenario: Switch the theme from the palette
    Given I open the app at "/" with color scheme "light"
    When I press "Control+k"
    And I type "dark theme" in the command palette
    And I press "Enter"
    Then the document uses the "dark" theme

  @FR-018
  Scenario: A data source entry fills Explore without running a query
    Given I open the app at "/"
    When I press "Control+k"
    And I type "sample files" in the command palette
    And I press "Enter"
    Then the page heading is "Explore"
    And no result is shown

  Scenario Outline: The open palette has no serious accessibility violations
    Given I open the app at "/" with color scheme "<scheme>"
    When I press "Control+k"
    Then the command palette is open
    And the page has no serious or critical accessibility violations

    Examples:
      | scheme |
      | light  |
      | dark   |

  Scenario: Collapse the sidebar to an icon rail and keep the choice
    Given I open the app at "/"
    When I click the button "Collapse sidebar"
    Then the sidebar is collapsed
    And I see the primary navigation with links "Home, Explore, Dashboards, Data sources, API reference"
    And the page has no serious or critical accessibility violations
    When I reload the page
    Then the sidebar is collapsed
    When I press "Control+b"
    Then the sidebar is expanded

  Scenario: Go to a page with a key sequence
    Given I open the app at "/"
    When I press "g"
    And I press "e"
    Then the page heading is "Explore"

  @FR-018
  Scenario: Single-key shortcuts do not fire while typing in a field
    Given I open the app at "/explore"
    When I type "gd/?" in the path field
    Then the path field contains "gd/?"
    And the page heading is "Explore"
    And the command palette is closed

  Scenario: Single-key shortcuts can be turned off
    Given I open the app at "/"
    When I press "?"
    Then the dialog "Keyboard shortcuts" is open
    And the page has no serious or critical accessibility violations
    When I switch off "Single-key shortcuts"
    And I press "Escape"
    And I press "g"
    And I press "e"
    Then the page heading is "Home"
    When I press "Control+k"
    Then the command palette is open

  @NFR-A11Y-005
  Scenario Outline: On a phone the frame does not scroll sideways
    Given the viewport is 390 by 844 pixels
    And I open the app at "<path>"
    Then the page does not scroll sideways
    And the page has no serious or critical accessibility violations

    Examples:
      | path     |
      | /        |
      | /explore |
