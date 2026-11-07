@FR-003 @FR-004
Feature: Build information and API description
  As an integrator
  I want to discover the server version and its API surface
  So that I can build compatible clients

  Scenario: Version endpoint returns build information
    Given the dv server is running with version "1.2.3" and commit "abc1234"
    When I send a GET request to "/api/v1/version"
    Then the response status should be 200
    And the JSON field "version" should be "1.2.3"
    And the JSON field "commit" should be "abc1234"
    And the response header "Cache-Control" should be "no-store"

  Scenario: OpenAPI document is published
    Given the dv server is running
    When I send a GET request to "/api/openapi.json"
    Then the response status should be 200
    And the JSON field "openapi" should start with "3.1"

  Scenario: Unknown API routes return a JSON 404
    Given the dv server is running
    When I send a GET request to "/api/v1/does-not-exist"
    Then the response status should be 404
    And the JSON field "error.code" should be "not_found"
