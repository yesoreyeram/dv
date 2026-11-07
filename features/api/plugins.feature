@FR-015 @NFR-SEC-002
Feature: Plugin catalogue
  As a dashboard editor
  I want to see which data-source and format plugins the server offers
  So that I can choose how to connect to my data

  Scenario: The catalogue lists registered plugins sorted by ID
    Given the plugin registry contains the source "zeta" and the source "alpha"
    And the plugin registry contains the format "csv"
    And the plugin registry is frozen
    When I send a GET request to "/api/v1/plugins"
    Then the response status should be 200
    And the JSON field "sources.0.id" should be "alpha"
    And the JSON field "sources.1.id" should be "zeta"
    And the JSON field "formats.0.id" should be "csv"
    And the response header "Cache-Control" should be "no-store"

  Scenario: An empty catalogue is returned as empty lists
    Given the plugin registry is frozen
    When I send a GET request to "/api/v1/plugins"
    Then the response status should be 200
    And the JSON field "sources" should be an empty list
    And the JSON field "formats" should be an empty list

  Scenario: Readiness fails until plugin registration has completed
    Given the plugin registry is not frozen
    When I send a GET request to "/readyz"
    Then the response status should be 503
    And the JSON field "checks.plugins" should be "fail"

  Scenario: Plugins cannot be registered after start-up
    Given the plugin registry is frozen
    When a source "late" is registered
    Then the registration fails

  Scenario: Invalid or duplicate plugin IDs are rejected
    Given the plugin registry contains the source "alpha"
    When a source "alpha" is registered
    Then the registration fails
    When a source "Bad_ID" is registered
    Then the registration fails
