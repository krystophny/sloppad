function trimLeadingSlashes(value) {
  return String(value || "").replace(/^\/+/, "");
}
function joinRelative(prefix, value) {
  const clean = trimLeadingSlashes(value);
  if (!clean) return `./${prefix}`;
  return `./${prefix}/${clean}`;
}
function appURL(path) {
  return new URL(String(path || "./"), document.baseURI || window.location.href).toString();
}
function apiURL(path) {
  return appURL(joinRelative("api", path));
}
function wsURL(path) {
  const url = new URL(joinRelative("ws", path), document.baseURI || window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
function staticURL(path) {
  return appURL(joinRelative("static", path));
}
export {
  apiURL,
  appURL,
  staticURL,
  wsURL
};

//# sourceMappingURL=paths.js.map
