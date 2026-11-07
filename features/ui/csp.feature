@NFR-SEC-007
Feature: Content Security Policy in the browser
  As a security engineer
  I want every page to run under the strict CSP without violations
  So that the policy can never be loosened to make a page work, and injected script is blocked

  Scenario Outline: Pages render with no CSP violations
    Given I am recording Content Security Policy violations
    And I open the app at "<path>" with color scheme "<scheme>"
    Then the page heading is "<heading>"
    And the document was served with a Content Security Policy that forbids inline script
    And no Content Security Policy violations were reported

    Examples:
      | path           | scheme | heading           |
      | /              | light  | Home              |
      | /              | dark   | Home              |
      | /data-sources  | light  | Data sources      |
      | /api-reference | light  | API reference     |
      | /no/such/page  | dark   | Page not found    |

  Scenario: Interacting with the shell causes no CSP violations
    Given I am recording Content Security Policy violations
    And I open the app at "/" with color scheme "light"
    When I click the button "Switch to dark theme"
    And I click the navigation link "Data sources"
    And I click the navigation link "API reference"
    Then no Content Security Policy violations were reported

  Scenario: Injected inline script is blocked and reported
    Given I am recording Content Security Policy violations
    And I open the app at "/"
    When a page script injects an inline script element
    Then the injected inline script did not run
    And a Content Security Policy violation for "script-src" was reported
