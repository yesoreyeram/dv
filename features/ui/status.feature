@FR-006 @FR-007
Feature: System status and API reference
  As an operator
  I want to see the health of the server and its API from the UI
  So that I can confirm the platform is working

  Scenario: Home page shows the server is operational
    Given I open the app at "/"
    Then I see the status "Operational"
    And I see the server version

  Scenario: API reference lists the published endpoints
    Given I open the app at "/api-reference"
    Then I see the endpoint "GET" "/healthz"
    And I see the endpoint "GET" "/readyz"
    And I see the endpoint "GET" "/api/v1/version"
