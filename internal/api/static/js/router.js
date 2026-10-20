// Hash routes: #/overview  #/incidents  #/incidents/<id>/<tab>  #/audit
// Malformed escapes (e.g. %E0) must not throw; keep the raw segment instead.
const decode = (s) => { try { return decodeURIComponent(s); } catch { return s; } };
export function parse(hash = location.hash) {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decode);
  const [page = "overview", id, tab] = parts;
  if (page === "incidents" && id) return { page: "incident", id, tab: tab || "evidence" };
  return { page, id: null, tab: null };
}
export const href = {
  overview: "#/overview",
  incidents: "#/incidents",
  audit: "#/audit",
  incident: (id, tab = "evidence") => `#/incidents/${encodeURIComponent(id)}/${tab}`,
};
export const go = (h) => { location.hash = h; };
