Feature: CI workflows only run tools that are pinned and verified (NFR-SEC-015)
  As the repository owner
  I want every tool CI downloads or runs to be pinned and checked
  So that a replaced release asset, a moving tag or a new tool version cannot
  run in CI with the job's token unless a reviewed commit allowed it.

  Downloads go through scripts/fetch-verified.sh, which fetches, verifies and only then
  writes the output file. Checking shell scripts for a correct hand-written verification
  proved unsound (conditional blocks, heredocs, manifests, ignored failures), so any
  other downloader is refused outright.

  Scenario: A download through the verifying helper passes
    Given a workflow step that runs:
      """
      scripts/fetch-verified.sh https://example.com/tool_1.2.3.tar.gz a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb tool.tar.gz
      ./scripts/fetch-verified.sh "https://example.com/b.zip" 'b65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb' b.zip
      tar -xzf tool.tar.gz tool && unzip b.zip
      """
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy

  Scenario Outline: Any other download fails the policy
    Given a workflow step that runs:
      """
      <script>
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "<rule>"

    Examples:
      | script                                                                                                                                  | rule                |
      | curl -sSfLo tool.tar.gz https://example.com/tool.tar.gz                                                                                 | download-unverified |
      | curl -sSfLo t https://e.com/t; echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  t" \| sha256sum -c -; ./t         | download-unverified |
      | wget -q https://example.com/tool                                                                                                        | download-unverified |
      | /usr/bin/curl -fLo t https://example.com/t                                                                                              | download-unverified |
      | command wget https://example.com/t                                                                                                      | download-unverified |
      | aria2c https://example.com/t                                                                                                            | download-unverified |
      | gh release download v1 -R o/r -p t                                                                                                      | download-unverified |
      | curl -sSfL https://example.com/tool.tar.gz \| tar -xz tool                                                                              | download-piped      |
      | curl -sSfL https://example.com/install.sh \| bash                                                                                       | download-piped      |
      | wget -qO- https://example.com/install.sh \| sh                                                                                          | download-piped      |
      | go run golang.org/x/vuln/cmd/govulncheck@latest ./...                                                                                   | go-tool-unpinned    |
      | go install github.com/example/tool@main                                                                                                 | go-tool-unpinned    |
      | go run -tags netgo golang.org/x/vuln/cmd/govulncheck@latest ./...                                                                       | go-tool-unpinned    |
      | go run -ldflags "-s -w" -mod mod golang.org/x/vuln/cmd/govulncheck@latest ./...                                                         | go-tool-unpinned    |
      | go install -C /tmp github.com/example/a@v1.2.3 github.com/example/b@latest                                                              | go-tool-unpinned    |

  Scenario: A download inside a heredoc is refused too
    Given a workflow step that runs:
      """
      cat <<'END'
      curl -fLo tool https://example.com/tool
      END
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "download-unverified"

  Scenario: A commented-out download is not a download
    Given a workflow step that runs:
      """
      # curl -fsSL https://example.com/install.sh | bash
      echo curly
      """
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy

  Scenario Outline: A helper call must carry a committed SHA-256 literal
    Given a workflow step that runs:
      """
      <script>
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "download-unverified"

    Examples:
      | script                                                                                     |
      | scripts/fetch-verified.sh https://example.com/t "$TOOL_SHA256" t                           |
      | scripts/fetch-verified.sh https://example.com/t a65b5253807a68ac t                         |
      | scripts/fetch-verified.sh https://example.com/t                                            |
      | scripts/fetch-verified.sh http://example.com/t a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb t |

  Scenario Outline: The helper writes the output only when the SHA-256 matches
    Given an HTTPS server that serves "release bytes" at "/tool"
    When the helper fetches "/tool" with <hash> into "tool"
    Then the helper <outcome>

    Examples:
      | hash                       | outcome                                         |
      | the SHA-256 of the content | succeeds and "tool" holds the content           |
      | a different SHA-256        | fails and leaves no "tool" or temporary file    |
      | a malformed SHA-256        | fails and leaves no "tool" or temporary file    |

  Scenario: The helper refuses plain HTTP
    Given an HTTPS server that serves "release bytes" at "/tool"
    When the helper fetches the plain-HTTP URL of "/tool" with the SHA-256 of the content into "tool"
    Then the helper fails and leaves no "tool" or temporary file

  Scenario Outline: A run value that YAML folds into one line is checked as Actions runs it
    Given a workflow file:
      """
      jobs:
        a:
          steps:
            - run: <indicator>
                go run
                golang.org/x/vuln/cmd/govulncheck@latest ./...
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "go-tool-unpinned"

    Examples:
      | indicator |
      | >-        |
      | >         |
      | >+        |
      | go run    |

  Scenario: A multi-line quoted run value is checked as one line
    Given a workflow file:
      """
      jobs:
        a:
          steps:
            - run: "go install
                github.com/example/tool@main"
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "go-tool-unpinned"

  Scenario: A Go tool pinned to an exact version passes
    Given a workflow step that runs:
      """
      go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
      go run ./cmd/dv-automerge
      """
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy

  Scenario Outline: Action references must name a tag or a commit
    Given a workflow step that uses "<ref>"
    When the CI supply-chain policy checks the workflow
    Then the result for the action is "<result>"

    Examples:
      | ref                                                            | result        |
      | actions/checkout@v4                                            | pass          |
      | golangci/golangci-lint-action@v8.0.0                           | pass          |
      | actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683      | pass          |
      | ./.github/actions/local                                        | pass          |
      | actions/checkout@main                                          | action-unpinned |
      | actions/checkout                                               | action-unpinned |
      | docker://alpine:3.20                                           | action-unpinned |

  Scenario: The repository's own workflows pass the policy
    Given the workflows in ".github/workflows"
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy
