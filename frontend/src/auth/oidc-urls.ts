export function accountConsoleURL(rawIssuer: string): string {
  const issuer = new URL(rawIssuer);
  const loopback = issuer.hostname === "127.0.0.1" || issuer.hostname === "localhost";
  if ((issuer.protocol !== "https:" && !(issuer.protocol === "http:" && loopback)) || issuer.username || issuer.password || issuer.search || issuer.hash) {
    throw new Error("OIDC issuer is not safe for account management");
  }
  issuer.pathname = `${issuer.pathname.replace(/\/$/, "")}/account/`;
  return issuer.toString();
}
