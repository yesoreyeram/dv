@FR-016 @FR-017 @NFR-SEC-016 @NFR-SEC-009
Feature: Pre-authentication safety gate and provisioning validation
  As an operator
  I want the unauthenticated query API to be off by default and loopback-only
  So that enabling it by mistake does not expose my files to the network

  Scenario: The query API is disabled by default
    Given the environment is empty
    When the configuration is loaded
    Then the configuration is valid
    And the query API is off

  Scenario Outline: Enabling the query API requires a loopback bind
    Given the environment variable "DV_QUERY_API_ENABLED" is "true"
    And the environment variable "DV_HTTP_ADDR" is "<addr>"
    When the configuration is loaded
    Then the configuration is <outcome>

    Examples:
      | addr            | outcome                                  |
      | 127.0.0.1:8080  | valid                                    |
      | [::1]:8080      | valid                                    |
      | localhost:8080  | valid                                    |
      | :8080           | rejected mentioning "queryApiAllowRemote" |
      | 0.0.0.0:8080    | rejected mentioning "queryApiAllowRemote" |
      | 10.0.0.5:8080   | rejected mentioning "queryApiAllowRemote" |
      | example.com:80  | rejected mentioning "queryApiAllowRemote" |

  Scenario: An explicit remote opt-in is accepted
    Given the environment variable "DV_QUERY_API_ENABLED" is "true"
    And the environment variable "DV_QUERY_API_ALLOW_REMOTE" is "true"
    And the environment variable "DV_HTTP_ADDR" is ":8080"
    When the configuration is loaded
    Then the configuration is valid

  Scenario Outline: Invalid provisioned data sources fail start-up
    Given the config file contains:
      """
      {"datasources": [<entries>]}
      """
    When the configuration is loaded
    Then the configuration is rejected mentioning "<message>"

    Examples:
      | entries                                                                                                     | message        |
      | {"uid":"bad uid","type":"file","name":"x"}                                                                  | uid            |
      | {"uid":"a","type":"file","name":"x"},{"uid":"a","type":"file","name":"y"}                                   | duplicate      |
      | {"uid":"a","type":"file","name":""}                                                                         | name           |
      | {"uid":"a","type":"","name":"x"}                                                                            | type           |
      | {"uid":"a","type":"file","name":"x","secureJsonData":{"password":"hunter2"}}                                | secureJsonData |

  Scenario: Host query limits must be positive
    Given the environment variable "DV_QUERY_MAX_ROWS" is "0"
    When the configuration is loaded
    Then the configuration is rejected mentioning "queryMaxRows"

  @NFR-SEC-017
  Scenario Outline: The loopback query API only answers loopback host names
    Given a file root "samples" containing "sales.csv":
      """
      time,amount
      2026-11-01T00:00:00Z,1
      """
    And a provisioned data source "files" of type "file" named "Sample files" with root "samples"
    And the query API is enabled
    And the request header "Host" is "<host>"
    When I query "files" with refId "A" and path "sales.csv"
    Then the response status should be <status>
    And the response body does not contain "attacker"

    Examples:
      | host                   | status |
      | 127.0.0.1:8080         | 200    |
      | localhost:8080         | 200    |
      | LOCALHOST.             | 200    |
      | app.localhost:8080     | 200    |
      | [::1]:8080             | 200    |
      | 127.8.9.10             | 200    |
      | attacker.example:8080  | 403    |
      | 10.0.0.5:8080          | 403    |
      | localhost.attacker.io  | 403    |

  @NFR-SEC-017
  Scenario: The data-source list refuses a rebound host name
    Given a file root "samples" containing "sales.csv":
      """
      time,amount
      2026-11-01T00:00:00Z,1
      """
    And a provisioned data source "files" of type "file" named "Sample files" with root "samples"
    And the query API is enabled
    And the request header "Host" is "attacker.example"
    When I send a GET request to "/api/v1/datasources"
    Then the response status should be 403
    And the JSON field "error.code" should be "host_not_allowed"

  @NFR-SEC-017
  Scenario Outline: Cross-site browser requests cannot run queries
    Given a file root "samples" containing "sales.csv":
      """
      time,amount
      2026-11-01T00:00:00Z,1
      """
    And a provisioned data source "files" of type "file" named "Sample files" with root "samples"
    And the query API is enabled
    And the request header "<header>" is "<value>"
    When I query "files" with refId "A" and path "sales.csv"
    Then the response status should be <status>

    Examples:
      | header         | value                   | status |
      | Sec-Fetch-Site | same-origin             | 200    |
      | Sec-Fetch-Site | none                    | 200    |
      | Sec-Fetch-Site | cross-site              | 403    |
      | Sec-Fetch-Site | same-site               | 403    |
      | Origin         | http://127.0.0.1:8080   | 200    |
      | Origin         | http://attacker.example | 403    |
      | Origin         | null                    | 403    |

  @NFR-SEC-017
  Scenario: With the remote opt-in other host names are served but cross-site requests are still refused
    Given a file root "samples" containing "sales.csv":
      """
      time,amount
      2026-11-01T00:00:00Z,1
      """
    And a provisioned data source "files" of type "file" named "Sample files" with root "samples"
    And the query API is enabled with the remote opt-in
    And the request header "Host" is "dv.internal.example"
    And the request header "Origin" is "http://attacker.example"
    When I query "files" with refId "A" and path "sales.csv"
    Then the response status should be 403
    And the JSON field "error.code" should be "cross_site_request"
