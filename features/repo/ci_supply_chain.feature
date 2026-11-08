Feature: CI workflows only run tools that are pinned and verified (NFR-SEC-015)
  As the repository owner
  I want every tool CI downloads or runs to be pinned and checked
  So that a replaced release asset, a moving tag or a new tool version cannot
  run in CI with the job's token unless a reviewed commit allowed it.

  Scenario: A download verified against a committed SHA-256 passes
    Given a workflow step that runs:
      """
      curl -sSfLo tool.tar.gz https://example.com/tool_1.2.3_linux_x64.tar.gz
      echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  tool.tar.gz" | sha256sum -c -
      tar -xzf tool.tar.gz tool
      """
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy

  Scenario Outline: An unverified or unpinned tool fails the policy
    Given a workflow step that runs:
      """
      <script>
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "<rule>"

    Examples:
      | script                                                                  | rule                 |
      | curl -sSfLo tool.tar.gz https://example.com/tool.tar.gz                 | download-unverified  |
      | curl -sSfL https://example.com/tool.tar.gz \| tar -xz tool              | download-piped       |
      | curl -sSfL https://example.com/install.sh \| bash                       | download-piped       |
      | wget -qO- https://example.com/install.sh \| sh                          | download-piped       |
      | go run golang.org/x/vuln/cmd/govulncheck@latest ./...                   | go-tool-unpinned     |
      | go install github.com/example/tool@main                                 | go-tool-unpinned     |

  Scenario Outline: Each downloaded file is verified on its own, before it is used
    Given a workflow step that runs:
      """
      <script>
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "download-unverified"
    And the finding mentions "<file>"

    Examples:
      | script | file |
      | curl -sSfLo a.tgz https://e.com/a.tgz; curl -sSfLo b.tgz https://e.com/b.tgz; echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  a.tgz" \| sha256sum -c -; tar -xzf b.tgz | b.tgz |
      | curl -sSfLo a.tgz https://e.com/a.tgz; tar -xzf a.tgz; echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  a.tgz" \| sha256sum -c - | a.tgz |
      | curl -sSfL https://e.com/tool > tool; echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  other" \| sha256sum -c - | tool |
      | curl -sSf https://e.com/tool; echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  tool" \| sha256sum -c - | output file |

  Scenario: A commented-out checksum is not a verification
    Given a workflow step that runs:
      """
      curl -fLo tool https://example.com/tool
      # echo "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  tool" | sha256sum -c -
      ./tool
      """
    When the CI supply-chain policy checks the workflow
    Then the policy reports "download-unverified"
    And the finding mentions "tool"

  Scenario: Two downloads, each verified before use, pass
    Given a workflow step that runs:
      """
      curl -sSfLo a.tgz https://example.com/a.tgz
      wget -q https://example.com/dl/b.zip
      printf '%s  %s\n' a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb a.tgz > sums
      echo "b65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb  b.zip" >> sums
      sha256sum -c sums
      tar -xzf a.tgz && unzip b.zip
      """
    When the CI supply-chain policy checks the workflow
    Then the workflow passes the policy

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
