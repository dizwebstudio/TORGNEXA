import {useState} from "react";
import type {AuthSession, UserProfile} from "../auth/session-model";
import {useApi} from "../api/ApiProvider";
import {ProductImage} from "./ProductImage";

const localDemoAvatar = "/demo-images/demo-avatar.svg";

function initials(session: Pick<AuthSession, "displayName">, profile?: UserProfile): string {
  const names = [profile?.givenName, profile?.familyName].filter(Boolean) as string[];
  if (names.length > 1) return names.map((value) => value[0]).join("").slice(0, 2).toUpperCase();
  return session.displayName.trim().split(/\s+/).map((value) => value[0]).join("").slice(0, 2).toUpperCase() || "П";
}

export function UserAvatar({session, profile: override, className = ""}: {session: Pick<AuthSession, "displayName" | "profile">; profile?: UserProfile; className?: string}) {
  const api = useApi();
  const profile = override ?? session.profile;
  const [failed, setFailed] = useState(false);
  const picture = !failed ? profile?.picture || (profile?.username?.toLowerCase() === "demo" ? localDemoAvatar : undefined) : undefined;
  const internalPicture = picture?.match(/^\/api\/v1\/uploads\/upl_[0-9a-f]{32}\/content$/);
  return <span className={`avatar user-avatar ${className}`.trim()} aria-label={`Фото профиля: ${session.displayName}`}>
    {internalPicture ? <ProductImage api={api} src={internalPicture[0]} alt="" className="avatar-image"/> : picture ? <img src={picture} alt="" referrerPolicy="no-referrer" onError={() => setFailed(true)}/> : initials(session, profile)}
  </span>;
}
