Feature: One toolchain, declared once and on the Active LTS line (NFR-MAINT-007)
  As the repository owner
  I want mise.toml, go.mod, package.json, CI and the Dockerfile to agree on tool versions
  So that what CI tests is what the image ships, and Node stays on a supported LTS line.

  Each scenario copies the toolchain declarations of this repository into a scratch
  directory, changes one of them, and runs scripts/check-toolchain.sh there (AC5).

  Scenario: The repository's own toolchain pins are consistent
    Given a copy of the repository's toolchain declarations
    When the toolchain check runs
    Then the toolchain check passes
    And the toolchain check output mentions "node 26."

  Scenario: Node is pinned to the Active LTS line in every place it is declared
    Given a copy of the repository's toolchain declarations
    Then "mise.toml" pins Node major 26
    And every "node-version" in ".github/workflows/ci.yml" is 26
    And "deploy/Dockerfile" builds the web assets with "node:26-alpine"

  Scenario Outline: A drifting declaration fails the check
    Given a copy of the repository's toolchain declarations
    And in "<file>" the text '<from>' is replaced with '<to>'
    When the toolchain check runs
    Then the toolchain check fails
    And the toolchain check output mentions "<message>"

    Examples:
      | file                     | from                    | to                      | message                                |
      | .github/workflows/ci.yml | node-version: 26        | node-version: 24        | node major (ci.yml)                    |
      | deploy/Dockerfile        | FROM node:26-alpine     | FROM node:24-alpine     | must be an LTS (even) major >= 26      |
      | deploy/Dockerfile        | FROM node:26-alpine     | FROM node:27-alpine     | must be an LTS (even) major >= 26      |
      | mise.toml                | node = "26.             | node = "27.             | is not an LTS (even) major             |
      | go.mod                   | go 1.24.7               | go 1.24.6               | go (go.mod)                            |
      | deploy/Dockerfile        | FROM golang:1.24-alpine | FROM golang:1.23-alpine | is older than go                       |
      | package.json             | "pnpm@10.28.0"          | "pnpm@10.27.0"          | pnpm (package.json)                    |
      | .github/workflows/ci.yml | version: v2.5.0         | version: v2.4.0         | golangci-lint (ci.yml)                 |
      | deploy/Dockerfile        | pnpm@10.28.0            | pnpm@10.27.0            | pnpm (Dockerfile)                      |
      | deploy/Dockerfile        | npm install -g pnpm@10.28.0 | corepack enable     | pnpm (Dockerfile)                      |

  Scenario: The image may build with a newer Node LTS than the pin
    Given a copy of the repository's toolchain declarations
    And in "deploy/Dockerfile" the text 'FROM node:26-alpine' is replaced with 'FROM node:28-alpine'
    When the toolchain check runs
    Then the toolchain check passes
