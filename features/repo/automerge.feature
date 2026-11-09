Feature: Merge a pull request automatically once CI is green (NFR-MAINT-006)
  As the repository owner
  I want green, trusted pull requests to merge without a manual click
  So that reviewed automation and dependency updates do not pile up,
  while nothing untested or untrusted is ever merged.

  Background:
    Given the repository "yesoreyeram/dv" with default branch "main"
    And the allowed authors are "yesoreyeram,dependabot[bot]"
    And an open pull request #42 by "dependabot[bot]" from "yesoreyeram/dv" into "main" at commit "abc123"
    And the CI run for commit "abc123" concluded "success" on a "pull_request" event
    And every check run on the commit succeeded

  Scenario: A green, trusted pull request is merged with its head commit pinned
    When the merge bot decides
    Then the pull request is merged
    And the merge is pinned to commit "abc123"

  Scenario Outline: A pull request that is not safe to merge is skipped with a reason
    Given <condition>
    When the merge bot decides
    Then the pull request is not merged
    And the reason mentions "<reason>"

    Examples:
      | condition                                                    | reason            |
      | the CI run concluded "failure"                               | CI concluded      |
      | the CI run was triggered by a "push" event                   | not a pull_request |
      | the pull request is a draft                                  | draft             |
      | the pull request is closed                                   | not open          |
      | the pull request comes from the fork "attacker/dv"           | fork              |
      | the pull request is authored by "mallory"                    | not allow-listed  |
      | the pull request targets "feat/parent"                       | default branch    |
      | the pull request has the label "hold"                        | label             |
      | the pull request has the label "do-not-merge"                | label             |
      | a newer commit "def456" was pushed to the pull request       | head moved        |
      | the pull request has merge conflicts                         | conflict          |
      | the check run "lint" concluded "failure"                     | check run         |
      | the check run "e2e" is still "in_progress"                   | check run         |
      | there are no check runs on the commit                        | no check runs     |

  Scenario: Neutral and skipped check runs do not block the merge
    Given the check run "optional" concluded "skipped"
    And the check run "advisory" concluded "neutral"
    When the merge bot decides
    Then the pull request is merged

  Scenario: Mergeability that GitHub has not computed yet does not block the merge
    Given GitHub has not computed mergeability yet
    When the merge bot decides
    Then the pull request is merged
