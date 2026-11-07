import { describe, expect, it } from "vitest";
import { en, type MessageKey } from "./messages";

/**
 * NFR-AUD-004: product copy must not claim a security or compliance property that the
 * running build does not deliver. A message that mentions one must either name it as
 * planned with its milestone, or be allow-listed here against the shipped requirement.
 */
const SECURITY_CLAIM =
  /\b(encrypt\w*|single sign-on|SSO|SAML|OIDC|role-based|RBAC|audit(?:ed)? log\w*|SOC ?2|ISO ?27001|HIPAA|GDPR|FedRAMP|end-to-end)\b/i;
const PLANNED_MARKER = /\bplanned for milestone M\d+\b/i;

/** Keys whose security wording describes shipped behaviour, with the requirement that ships it. */
const SHIPPED_CLAIMS: Partial<Record<MessageKey, string>> = {};

function unbackedClaims(catalog: Record<string, string>): string[] {
  return Object.entries(catalog)
    .filter(([, msg]) => SECURITY_CLAIM.test(msg) && !PLANNED_MARKER.test(msg))
    .filter(([key]) => !(key in SHIPPED_CLAIMS))
    .map(([key]) => key);
}

describe("message catalog claims (NFR-AUD-004)", () => {
  it("makes no security or compliance claim without a planned-milestone marker", () => {
    expect(unbackedClaims(en)).toEqual([]);
  });

  it("flags the wording that B-049 removed", () => {
    expect(unbackedClaims({ k: "Credentials are encrypted at rest and never sent back." })).toEqual(
      ["k"],
    );
  });

  it.each([
    "Sign in with SSO.",
    "Access is controlled by RBAC.",
    "Every change is written to the audit log.",
    "dv is SOC 2 compliant.",
    "End-to-end encrypted.",
  ])("flags %j", (msg) => {
    expect(unbackedClaims({ k: msg })).toEqual(["k"]);
  });

  it("accepts a claim named as planned with its milestone", () => {
    expect(unbackedClaims({ k: "Encrypted secret storage is planned for milestone M3." })).toEqual(
      [],
    );
  });

  it("ignores ordinary copy", () => {
    expect(unbackedClaims({ k: "Stored credentials are not supported yet." })).toEqual([]);
  });

  it("cites a requirement ID for every allow-listed key", () => {
    for (const [key, req] of Object.entries(SHIPPED_CLAIMS)) {
      expect(key in en).toBe(true);
      expect(req).toMatch(/^(FR|NFR)-[A-Z0-9-]+$/);
    }
  });
});
