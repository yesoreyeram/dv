Feature: One toolchain, declared once and on supported lines (NFR-MAINT-007, NFR-SEC-015 AC6)
  As the repository owner
  I want mise.toml, go.mod, package.json, CI and the Dockerfile to agree on tool versions
  So that what CI tests is what the image ships, and Node stays on a supported LTS line.

  Each scenario copies the toolchain declarations of this repository into a scratch
  directory, changes one of them, and runs scripts/check-toolchain.sh there (AC5).

  Values in braces are read from the copied files: {go}, {pnpm}, {golangci-lint} and
  {node.major} (with +1 / -2 offsets) from mise.toml, {docker.go} and {docker.node} (with
  a +2 offset) from the Dockerfile's FROM lines. So neither a coordinated toolchain bump
  nor a Dependabot builder-image bump needs this file to change.

  Scenario: The repository's own toolchain pins are consistent
    Given a copy of the repository's toolchain declarations
    When the toolchain check runs
    Then the toolchain check passes
    And the toolchain check output mentions "node {node.major}."

  Scenario: Node is pinned to the Active LTS line in every place it is declared
    # Literal on purpose: moving to a new Node major is a planned migration (B-055).
    Given a copy of the repository's toolchain declarations
    Then "mise.toml" pins Node major 26
    And every "node-version" in ".github/workflows/ci.yml" is 26
    And "deploy/Dockerfile" builds the web assets with an LTS Node image not older than the pin

  Scenario Outline: A drifting declaration fails the check
    Given a copy of the repository's toolchain declarations
    And in "<file>" the text '<from>' is replaced with '<to>'
    When the toolchain check runs
    Then the toolchain check fails
    And the toolchain check output mentions "<message>"

    Examples:
      | file                     | from                                          | to                               | message                                         |
      | .github/workflows/ci.yml | node-version: {node.major}                    | node-version: {node.major-2}     | node major (ci.yml)                             |
      | deploy/Dockerfile        | FROM node:{docker.node}-alpine                | FROM node:{node.major-2}-alpine  | must be an LTS (even) major >= {node.major}     |
      | deploy/Dockerfile        | FROM node:{docker.node}-alpine                | FROM node:{node.major+1}-alpine  | must be an LTS (even) major >= {node.major}     |
      | mise.toml                | node = "{node.major}.                         | node = "{node.major+1}.          | is not an LTS (even) major                      |
      | go.mod                   | go {go}                                       | go 1.0.0                         | go (go.mod)                                     |
      | deploy/Dockerfile        | FROM golang:{docker.go}-alpine                | FROM golang:1.0-alpine           | is older than go                                |
      | package.json             | "pnpm@{pnpm}"                                 | "pnpm@0.0.1"                     | pnpm (package.json)                             |
      | .github/workflows/ci.yml | version: v{golangci-lint}                     | version: v0.0.1                  | golangci-lint (ci.yml)                          |
      | deploy/Dockerfile        | pnpm@{pnpm}                                   | pnpm@0.0.1                       | pnpm (Dockerfile)                               |
      | deploy/Dockerfile        | npm install -g --ignore-scripts pnpm@{pnpm}   | corepack enable                  | pnpm (Dockerfile)                               |
      | .github/workflows/ci.yml | GOTOOLCHAIN=local go run golang.org/x/vuln/   | go run golang.org/x/vuln/        | govulncheck must run with GOTOOLCHAIN=local     |
      | .github/workflows/ci.yml | govulncheck@v1.9.0 -show version              | govulncheck@v1.9.0               | govulncheck must print the scanned Go version   |

  Scenario: A coordinated patch bump of every pin passes without editing this file
    Given a copy of the repository's toolchain declarations
    And every Go, pnpm and golangci-lint pin is bumped by one patch version
    When the toolchain check runs
    Then the toolchain check passes

  Scenario: Dependabot moving the builder images to newer allowed versions passes without editing this file
    Given a copy of the repository's toolchain declarations
    And in "deploy/Dockerfile" the text 'FROM golang:{docker.go}-alpine' is replaced with 'FROM golang:9.99-alpine'
    And in "deploy/Dockerfile" the text 'FROM node:{docker.node}-alpine' is replaced with 'FROM node:{docker.node+2}-alpine'
    When the toolchain check runs
    Then the toolchain check passes

  Scenario: The image may build with a newer Node LTS than the pin
    Given a copy of the repository's toolchain declarations
    And in "deploy/Dockerfile" the text 'FROM node:{docker.node}-alpine' is replaced with 'FROM node:{docker.node+2}-alpine'
    When the toolchain check runs
    Then the toolchain check passes
