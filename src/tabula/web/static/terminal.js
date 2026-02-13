import { normalizeTerminalOutput } from './terminal-text.js';

const MAX_BUFFER_SIZE = 200_000;
const STICK_THRESHOLD = 24;

let container = null;
let pre = null;
let probe = null;
let inputCapture = null;
let resizeObserver = null;
let buffer = "";
let cols = 120;
let rows = 40;
let cellMetrics = { width: 8.4, height: 18 };
let stickToBottom = true;
let dataCallback = null;
let resizeCallback = null;
const decoder = new TextDecoder();

function keyEventToTerminalData(event) {
  if (event.metaKey) {
    return null;
  }
  if (event.ctrlKey && event.key.length === 1) {
    const lower = event.key.toLowerCase();
    if (lower >= "a" && lower <= "z") {
      return String.fromCharCode(lower.charCodeAt(0) - 96);
    }
    if (lower === " ") {
      return "\u0000";
    }
  }

  switch (event.key) {
    case "Enter": return "\r";
    case "Backspace": return "\u007f";
    case "Tab": return "\t";
    case "Escape": return "\u001b";
    case "ArrowUp": return "\u001b[A";
    case "ArrowDown": return "\u001b[B";
    case "ArrowRight": return "\u001b[C";
    case "ArrowLeft": return "\u001b[D";
    case "Home": return "\u001b[H";
    case "End": return "\u001b[F";
    case "Delete": return "\u001b[3~";
    case "PageUp": return "\u001b[5~";
    case "PageDown": return "\u001b[6~";
    default: break;
  }

  if (event.altKey && event.key.length === 1) {
    return `\u001b${event.key}`;
  }

  return null;
}

function measureTerminalSize() {
  if (!container || !probe) {
    return;
  }
  const probeRect = probe.getBoundingClientRect();
  if (probeRect.width > 2 && probeRect.height > 4) {
    cellMetrics = { width: probeRect.width, height: probeRect.height };
  }
  const rect = container.getBoundingClientRect();
  const styles = window.getComputedStyle(container);
  const paddingX = parseFloat(styles.paddingLeft || "0") + parseFloat(styles.paddingRight || "0");
  const paddingY = parseFloat(styles.paddingTop || "0") + parseFloat(styles.paddingBottom || "0");
  const contentWidth = Math.max(80, rect.width - paddingX);
  const contentHeight = Math.max(80, rect.height - paddingY);
  const glyphWidth = Math.max(4, cellMetrics.width);
  const glyphHeight = Math.max(10, cellMetrics.height);
  const newCols = Math.max(40, Math.floor(contentWidth / glyphWidth));
  const newRows = Math.max(10, Math.floor(contentHeight / glyphHeight));
  const changed = newCols !== cols || newRows !== rows;
  cols = newCols;
  rows = newRows;
  if (changed && resizeCallback) {
    resizeCallback({ cols, rows });
  }
}

function isNearBottom() {
  if (!container) return true;
  return container.scrollHeight - container.scrollTop - container.clientHeight <= STICK_THRESHOLD;
}

function scrollToBottom() {
  requestAnimationFrame(() => {
    if (container) {
      container.scrollTop = container.scrollHeight;
    }
  });
}

function render() {
  if (!pre) return;
  const text = normalizeTerminalOutput(buffer, { cols });
  pre.textContent = text;
  if (stickToBottom) {
    scrollToBottom();
  }
}

function onContainerScroll() {
  stickToBottom = isNearBottom();
}

function onKeyDown(event) {
  const encoded = keyEventToTerminalData(event);
  if (encoded) {
    event.preventDefault();
    if (dataCallback) dataCallback(encoded);
    return;
  }
  if (!event.ctrlKey && !event.altKey && !event.metaKey && event.key.length === 1) {
    event.preventDefault();
    if (dataCallback) dataCallback(event.key);
  }
}

function onPaste(event) {
  const pasted = event.clipboardData.getData("text");
  if (!pasted) return;
  event.preventDefault();
  if (dataCallback) dataCallback(pasted);
}

export function initTerminal(containerEl) {
  container = containerEl;
  container.innerHTML = "";
  buffer = "";
  stickToBottom = true;
  dataCallback = null;
  resizeCallback = null;

  pre = document.createElement("pre");
  pre.className = "terminal-text";
  container.appendChild(pre);

  probe = document.createElement("span");
  probe.className = "terminal-measure-probe";
  probe.setAttribute("aria-hidden", "true");
  probe.textContent = "M";
  container.appendChild(probe);

  inputCapture = document.createElement("textarea");
  inputCapture.className = "terminal-input-capture";
  inputCapture.setAttribute("tabindex", "0");
  inputCapture.setAttribute("role", "textbox");
  inputCapture.setAttribute("aria-label", "Terminal input capture");
  inputCapture.setAttribute("autocomplete", "off");
  inputCapture.setAttribute("autocapitalize", "none");
  inputCapture.setAttribute("autocorrect", "off");
  inputCapture.setAttribute("spellcheck", "false");
  inputCapture.setAttribute("data-gramm", "false");
  inputCapture.setAttribute("data-lpignore", "true");
  inputCapture.setAttribute("data-1p-ignore", "true");
  inputCapture.setAttribute("data-form-type", "other");
  container.appendChild(inputCapture);

  container.setAttribute("tabindex", "0");
  container.addEventListener("scroll", onContainerScroll);
  container.addEventListener("keydown", onKeyDown);
  container.addEventListener("paste", onPaste);
  container.addEventListener("click", () => {
    if (window.getSelection().isCollapsed) {
      inputCapture.focus();
      inputCapture.value = "";
    }
  });

  inputCapture.addEventListener("input", () => {
    const text = inputCapture.value;
    if (text && dataCallback) {
      dataCallback(text);
    }
    inputCapture.value = "";
  });
  inputCapture.addEventListener("blur", () => { inputCapture.value = ""; });

  measureTerminalSize();

  resizeObserver = new ResizeObserver(() => {
    measureTerminalSize();
    render();
  });
  resizeObserver.observe(container);

  window._tabulaTerminal = {
    cols,
    rows,
    onData(cb) { dataCallback = cb; },
    onBinary() {},
    onResize(cb) { resizeCallback = cb; },
  };

  return window._tabulaTerminal;
}

export function destroyTerminal() {
  if (resizeObserver) {
    resizeObserver.disconnect();
    resizeObserver = null;
  }
  if (container) {
    container.removeEventListener("scroll", onContainerScroll);
    container.removeEventListener("keydown", onKeyDown);
    container.removeEventListener("paste", onPaste);
    container.innerHTML = "";
  }
  container = null;
  pre = null;
  probe = null;
  inputCapture = null;
  buffer = "";
  dataCallback = null;
  resizeCallback = null;
  window._tabulaTerminal = null;
}

export function writeToTerminal(data) {
  let text;
  if (data instanceof ArrayBuffer || data instanceof Uint8Array) {
    text = decoder.decode(data instanceof ArrayBuffer ? data : data.buffer, { stream: true });
  } else {
    text = data;
  }
  buffer += text;
  if (buffer.length > MAX_BUFFER_SIZE) {
    buffer = buffer.slice(buffer.length - MAX_BUFFER_SIZE);
  }
  render();
}
