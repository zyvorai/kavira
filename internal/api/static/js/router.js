// Hash routes: #/overview  #/incidents  #/incidents/<id>/<tab>  #/audit
export function parse(hash = location.hash) {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
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
