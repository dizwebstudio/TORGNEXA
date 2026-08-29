import {useState} from "react";
import type {AuthSession, UserProfile} from "../auth/session-model";

const localDemoAvatar = "/demo-images/demo-avatar.svg";

function initials(session: Pick<AuthSession, "displayName">, profile?: UserProfile): string {
  const names = [profile?.givenName, profile?.familyName].filter(Boolean) as string[];
  if (names.length > 1) return names.map((value) => value[0]).join("").slice(0, 2).toUpperCase();
  return session.displayName.trim().split(/\s+/).map((value) => value[0]).join("").slice(0, 2).toUpperCase() || "П";
}

export function UserAvatar({session, profile: override, className = ""}: {session: Pick<AuthSession, "displayName" | "profile">; profile?: UserProfile; className?: string}) {
  const profile = override ?? session.profile;
  const [failed, setFailed] = useState(false);
  const picture = !failed ? profile?.picture || (profile?.username?.toLowerCase() === "demo" ? localDemoAvatar : undefined) : undefined;
  return <span className={`avatar user-avatar ${className}`.trim()} aria-label={`Фото профиля: ${session.displayName}`}>
    {picture ? <img src={picture} alt="" referrerPolicy="no-referrer" onError={() => setFailed(true)}/> : initials(session, profile)}
  </span>;
}
