export interface UserProfile {
  readonly username?: string;
  readonly email?: string;
  readonly givenName?: string;
  readonly familyName?: string;
  readonly picture?: string;
  readonly birthdate?: string;
  readonly jobTitle?: string;
  readonly department?: string;
  readonly phoneNumber?: string;
  readonly locale?: string;
}

export interface AuthSession {
  readonly subject: string;
  readonly displayName: string;
  readonly accessToken: string;
  readonly capabilities: readonly string[];
  readonly roles?: readonly string[];
  readonly expiresAt?: string;
  readonly profile?: UserProfile;
}

export interface PublicSession {
  readonly subject: string;
  readonly displayName: string;
  readonly capabilities: readonly string[];
  readonly roles: readonly string[];
  readonly expiresAt?: string;
  readonly profile?: UserProfile;
}

const capabilityPattern = /^[a-z][a-z0-9._:-]{1,127}$/;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const roleDisplayNames: Readonly<Record<string, string>> = {
  admin: "TORGNEXA Administrator",
  manager: "Менеджер TORGNEXA",
  operator: "Оператор TORGNEXA",
  viewer: "Пользователь TORGNEXA",
};

export function safeDisplayName(value: string, subject: string, roles: readonly string[] = []): string {
  const candidate = value.trim();
  if (candidate.length > 200) throw new Error("invalid display name");

  const exposesOpaqueIdentity = !candidate
    || candidate.toLocaleLowerCase("en-US") === subject.toLocaleLowerCase("en-US")
    || uuidPattern.test(candidate);
  if (!exposesOpaqueIdentity) return candidate;

  for (const role of ["admin", "manager", "operator", "viewer"]) {
    if (roles.includes(role)) return roleDisplayNames[role];
  }
  return "Пользователь TORGNEXA";
}

function boundedProfileValue(value: string | undefined, maximum: number): string | undefined {
  if (value === undefined) return undefined;
  const candidate = value.trim();
  if (candidate.length > maximum) throw new Error("invalid profile value");
  return candidate || undefined;
}

function normalizeProfile(input?: UserProfile): UserProfile | undefined {
  if (!input) return undefined;
  const picture = boundedProfileValue(input.picture, 1024);
  const profile: UserProfile = {
    username: boundedProfileValue(input.username, 128),
    email: boundedProfileValue(input.email, 254),
    givenName: boundedProfileValue(input.givenName, 160),
    familyName: boundedProfileValue(input.familyName, 160),
    picture: picture && /^(?:https?:\/\/|\/)[^\s]{1,1024}$/.test(picture) ? picture : undefined,
    birthdate: boundedProfileValue(input.birthdate, 32),
    jobTitle: boundedProfileValue(input.jobTitle, 160),
    department: boundedProfileValue(input.department, 160),
    phoneNumber: boundedProfileValue(input.phoneNumber, 64),
    locale: boundedProfileValue(input.locale, 32),
  };
  return Object.values(profile).some(Boolean) ? profile : undefined;
}

export function normalizeSession(input: AuthSession): AuthSession {
  const subject = input.subject.trim();
  const accessToken = input.accessToken.trim();
  if (!subject || subject.length > 256) throw new Error("invalid auth subject");
  if (!accessToken || accessToken.length > 16_384) throw new Error("invalid access token");

  const capabilities = [...new Set(input.capabilities.map((value) => value.trim()))]
    .filter(Boolean)
    .sort();
  if (capabilities.length > 512 || capabilities.some((value) => !capabilityPattern.test(value))) {
    throw new Error("invalid capability set");
  }
  const roles = [...new Set((input.roles ?? []).map((value) => value.trim()))].filter(Boolean).sort();
  if (roles.length > 128 || roles.some((value) => !capabilityPattern.test(value))) {
    throw new Error("invalid role set");
  }
  const displayName = safeDisplayName(input.displayName, subject, roles);

  let expiresAt: string | undefined;
  if (input.expiresAt) {
    const parsed = new Date(input.expiresAt);
    if (!Number.isFinite(parsed.getTime())) throw new Error("invalid auth expiry");
    expiresAt = parsed.toISOString();
  }

  return {subject, displayName, accessToken, capabilities, roles, expiresAt, profile: normalizeProfile(input.profile)};
}

export function publicSession(session: AuthSession): PublicSession {
  return {
    subject: session.subject,
    displayName: session.displayName,
    capabilities: [...session.capabilities],
    roles: [...(session.roles ?? [])],
    expiresAt: session.expiresAt,
    profile: session.profile ? {...session.profile} : undefined,
  };
}

export function sessionExpired(session: AuthSession, now = Date.now()): boolean {
  if (!session.expiresAt) return false;
  const expires = Date.parse(session.expiresAt);
  return !Number.isFinite(expires) || expires <= now;
}

export function sessionNeedsRefresh(session: AuthSession, now = Date.now(), minimumValidityMs = 60_000): boolean {
  if (!session.expiresAt) return false;
  const expires = Date.parse(session.expiresAt);
  return !Number.isFinite(expires) || expires <= now + Math.max(0, minimumValidityMs);
}
