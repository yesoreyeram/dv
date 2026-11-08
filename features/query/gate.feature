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
