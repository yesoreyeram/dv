@NFR-SEC-007 @NFR-SEC-008 @NFR-OBS-002
Feature: HTTP security baseline
  As a security engineer
  I want every response hardened and abusive clients throttled
  So that the platform is safe by default

  Scenario Outline: Security headers are present on every response
    Given the dv server is running
    When I send a GET request to "<path>"
    Then the response header "X-Content-Type-Options" should be "nosniff"
    And the response header "X-Frame-Options" should be "DENY"
    And the response header "Referrer-Policy" should be "no-referrer"
    And the response header "Cross-Origin-Opener-Policy" should be "same-origin"
    And the response header "Content-Security-Policy" should contain "default-src 'self'"
    And the response header "Content-Security-Policy" should contain "frame-ancestors 'none'"
    And the response header "Content-Security-Policy" should not contain "unsafe-inline' 'unsafe-eval"

    Examples:
      | path              |
      | /healthz          |
      | /api/v1/version   |
      | /api/v1/missing   |

  Scenario: A request ID is generated and echoed
    Given the dv server is running
    When I send a GET request to "/healthz"
    Then the response header "X-Request-ID" should be a valid request ID

  Scenario: A malicious request ID is replaced
    Given the dv server is running
    When I send a GET request to "/healthz" with header "X-Request-ID" set to "bad\u0000id<script>"
    Then the response header "X-Request-ID" should be a valid request ID
    And the response header "X-Request-ID" should not be "bad\u0000id<script>"

  Scenario: Clients exceeding the rate limit are throttled
    Given the dv server is running with a rate limit of 2 requests per second and burst 2
    When I send 5 GET requests to "/api/v1/version" in quick succession
    Then at least 1 response should have status 429
    And every 429 response should have a "Retry-After" header

  Scenario: Addresses in one IPv6 /64 share a rate-limit bucket
    Given the dv server is running with a rate limit of 1 requests per second and burst 1
    When I send a GET request to "/api/v1/version" from peer address "[2001:db8:1:2::1]:40000"
    And I send a GET request to "/api/v1/version" from peer address "[2001:db8:1:2:ffff::9]:40001"
    Then the response status should be 429
    And the response header "Retry-After" should be "1"

  Scenario: Clients in different IPv6 /64s and IPv4 addresses are limited independently
    Given the dv server is running with a rate limit of 1 requests per second and burst 1
    When I send a GET request to "/api/v1/version" from peer address "[2001:db8:1:2::1]:40000"
    And I send a GET request to "/api/v1/version" from peer address "[2001:db8:1:3::1]:40000"
    Then the response status should be 200
    When I send a GET request to "/api/v1/version" from peer address "192.0.2.10:5000"
    Then the response status should be 200

  @NFR-SEC-012
  Scenario: Panics are not leaked to clients
    Given the dv server is running with a route that panics
    When I send a GET request to "/api/v1/test/panic"
    Then the response status should be 500
    And the JSON field "error.code" should be "internal"
    And the response body should not contain "goroutine"
