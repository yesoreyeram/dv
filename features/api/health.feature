@FR-001 @FR-002 @NFR-AVAIL-002
Feature: Health probes
  As a platform operator
  I want separate liveness and readiness probes
  So that the orchestrator restarts dead instances and only routes traffic to ready ones

  Scenario: Liveness reports ok
    Given the dv server is running
    When I send a GET request to "/healthz"
    Then the response status should be 200
    And the JSON field "status" should be "ok"

  Scenario: Readiness reports ready when all checks pass
    Given the dv server is running
    And the readiness check "plugins" passes
    When I send a GET request to "/readyz"
    Then the response status should be 200
    And the JSON field "status" should be "ready"
    And the JSON field "checks.plugins" should be "pass"

  Scenario: Readiness reports not ready without leaking error details
    Given the dv server is running
    And the readiness check "database" fails with "dial tcp 10.0.0.5:5432: connection refused"
    When I send a GET request to "/readyz"
    Then the response status should be 503
    And the JSON field "status" should be "not_ready"
    And the JSON field "checks.database" should be "fail"
    And the response body should not contain "10.0.0.5"

  @NFR-AVAIL-001
  Scenario: Readiness flips to not ready while draining
    Given the dv server is running
    And the server has started shutting down
    When I send a GET request to "/readyz"
    Then the response status should be 503
