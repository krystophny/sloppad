function esc(str) {
  const d = document.createElement("span");
  d.textContent = str;
  return d.innerHTML;
}
export {
  esc
};

//# sourceMappingURL=utils.js.map
