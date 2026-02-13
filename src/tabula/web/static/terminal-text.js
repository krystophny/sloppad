const ANSI_OSC_REGEX = /\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g;
const ANSI_DCS_PM_APC_REGEX = /\x1b(?:P|_|\^)[\s\S]*?\x1b\\/g;

const DEFAULT_COLS = 120;
const DEFAULT_TAB_SIZE = 8;
const MAX_SCROLLBACK_LINES = 5_000;

function clampCols(cols) {
  const parsed = Number.isFinite(cols) ? Number(cols) : DEFAULT_COLS;
  return Math.max(4, Math.min(500, Math.floor(parsed)));
}

function clampTabSize(tabSize) {
  const parsed = Number.isFinite(tabSize) ? Number(tabSize) : DEFAULT_TAB_SIZE;
  return Math.max(1, Math.min(16, Math.floor(parsed)));
}

function trimScrollback(state) {
  if (state.lines.length <= MAX_SCROLLBACK_LINES) {
    return;
  }
  const dropCount = state.lines.length - MAX_SCROLLBACK_LINES;
  state.lines.splice(0, dropCount);
  state.row = Math.max(0, state.row - dropCount);
  state.savedRow = Math.max(0, state.savedRow - dropCount);
}

function ensureRow(state, row) {
  while (state.lines.length <= row) {
    state.lines.push([]);
  }
}

function clampCursor(state) {
  if (state.row < 0) {
    state.row = 0;
  }
  ensureRow(state, state.row);
  if (state.col < 0) {
    state.col = 0;
  }
  if (state.col > state.cols) {
    state.col = state.cols;
  }
}

function ensureCol(state, targetCol) {
  if (targetCol === undefined) {
    targetCol = state.col;
  }
  const line = state.lines[state.row];
  while (line.length < targetCol) {
    line.push(" ");
  }
}

function lineFeed(state, carriageReturn) {
  state.row += 1;
  if (carriageReturn) {
    state.col = 0;
  }
  ensureRow(state, state.row);
  trimScrollback(state);
  clampCursor(state);
}

function writeChar(state, value) {
  if (state.col >= state.cols) {
    lineFeed(state, true);
  }
  ensureCol(state, state.col);
  const line = state.lines[state.row];
  if (state.col < line.length) {
    line[state.col] = value;
  } else {
    line.push(value);
  }
  state.col += 1;
  if (state.col >= state.cols) {
    lineFeed(state, true);
  }
}

function writeTab(state) {
  const spaces = state.tabSize - (state.col % state.tabSize || 0);
  for (let i = 0; i < spaces; i += 1) {
    writeChar(state, " ");
  }
}

function parseNumericParam(raw, fallback) {
  if (!raw || raw.length === 0) {
    return fallback;
  }
  const cleaned = raw.replace(/^[?>!]/, "");
  const parsed = Number.parseInt(cleaned, 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return fallback;
  }
  return parsed;
}

function parseCsiParams(paramsRaw) {
  if (!paramsRaw) {
    return [];
  }
  return paramsRaw.split(";");
}

function clearCurrentLine(state) {
  state.lines[state.row] = [];
  state.col = 0;
}

function clearLineFromCursor(state) {
  const line = state.lines[state.row];
  state.lines[state.row] = line.slice(0, Math.max(0, state.col));
}

function clearLineToCursor(state) {
  ensureCol(state, state.col);
  const line = state.lines[state.row];
  for (let i = 0; i < state.col; i += 1) {
    line[i] = " ";
  }
}

function clearDisplay(state, mode) {
  if (mode === 3) {
    state.lines = [[]];
    state.row = 0;
    state.col = 0;
    return;
  }

  if (mode === 2) {
    if (state.preserveScrollbackOnClear) {
      state.row = state.lines.length;
      state.col = 0;
      state.lines.push([]);
      trimScrollback(state);
      return;
    }
    state.lines = [[]];
    state.row = 0;
    state.col = 0;
    return;
  }

  if (mode === 0) {
    clearLineFromCursor(state);
    state.lines = state.lines.slice(0, state.row + 1);
    return;
  }

  if (mode === 1) {
    for (let i = 0; i < state.row; i += 1) {
      state.lines[i] = [];
    }
    clearLineToCursor(state);
  }
}

function eraseChars(state, count) {
  const size = Math.max(1, count);
  ensureCol(state, Math.min(state.cols, state.col + size));
  const line = state.lines[state.row];
  for (let i = 0; i < size && state.col + i < line.length; i += 1) {
    line[state.col + i] = " ";
  }
}

function insertChars(state, count) {
  const size = Math.max(1, count);
  ensureCol(state, state.col);
  const line = state.lines[state.row];
  line.splice(state.col, 0, ...new Array(size).fill(" "));
  if (line.length > state.cols) {
    line.length = state.cols;
  }
}

function deleteChars(state, count) {
  const size = Math.max(1, count);
  const line = state.lines[state.row];
  if (state.col < line.length) {
    line.splice(state.col, size);
  }
}

function insertLines(state, count) {
  const size = Math.max(1, count);
  const blanks = Array.from({ length: size }, () => []);
  state.lines.splice(state.row, 0, ...blanks);
  trimScrollback(state);
}

function deleteLines(state, count) {
  const size = Math.max(1, count);
  state.lines.splice(state.row, size);
  ensureRow(state, state.row);
}

function applyCsiSequence(state, command, paramsRaw) {
  const params = parseCsiParams(paramsRaw);

  if (command === "K") {
    const mode = parseNumericParam(params[0], 0);
    if (mode === 0) {
      clearLineFromCursor(state);
      return;
    }
    if (mode === 1) {
      clearLineToCursor(state);
      return;
    }
    if (mode === 2) {
      clearCurrentLine(state);
    }
    return;
  }

  if (command === "J") {
    clearDisplay(state, parseNumericParam(params[0], 0));
    return;
  }

  if (command === "X") {
    eraseChars(state, parseNumericParam(params[0], 1));
    return;
  }

  if (command === "@") {
    insertChars(state, parseNumericParam(params[0], 1));
    return;
  }

  if (command === "P") {
    deleteChars(state, parseNumericParam(params[0], 1));
    return;
  }

  if (command === "L") {
    insertLines(state, parseNumericParam(params[0], 1));
    return;
  }

  if (command === "M") {
    deleteLines(state, parseNumericParam(params[0], 1));
    return;
  }

  if (command === "G") {
    const col = Math.max(1, parseNumericParam(params[0], 1));
    state.col = Math.min(state.cols, col - 1);
    return;
  }

  if (command === "C") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.col = Math.min(state.cols, state.col + move);
    return;
  }

  if (command === "D") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.col = Math.max(0, state.col - move);
    return;
  }

  if (command === "A") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.row = Math.max(0, state.row - move);
    ensureRow(state, state.row);
    return;
  }

  if (command === "B") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.row += move;
    ensureRow(state, state.row);
    trimScrollback(state);
    return;
  }

  if (command === "E") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.row += move;
    state.col = 0;
    ensureRow(state, state.row);
    trimScrollback(state);
    return;
  }

  if (command === "F") {
    const move = Math.max(1, parseNumericParam(params[0], 1));
    state.row = Math.max(0, state.row - move);
    state.col = 0;
    ensureRow(state, state.row);
    return;
  }

  if (command === "H" || command === "f") {
    const row = Math.max(1, parseNumericParam(params[0], 1));
    const col = Math.max(1, parseNumericParam(params[1], 1));
    state.row = row - 1;
    state.col = Math.min(state.cols, col - 1);
    ensureRow(state, state.row);
    trimScrollback(state);
    return;
  }

  if (command === "s") {
    state.savedRow = state.row;
    state.savedCol = state.col;
    return;
  }

  if (command === "u") {
    state.row = Math.max(0, state.savedRow);
    state.col = Math.max(0, Math.min(state.cols, state.savedCol));
    ensureRow(state, state.row);
    return;
  }

  if (command === "m" || command === "h" || command === "l" || command === "n") {
    return;
  }
}

export function normalizeTerminalOutput(raw, options) {
  const input = raw.replace(ANSI_OSC_REGEX, "").replace(ANSI_DCS_PM_APC_REGEX, "");

  const state = {
    lines: [[]],
    row: 0,
    col: 0,
    savedRow: 0,
    savedCol: 0,
    cols: clampCols(options?.cols),
    tabSize: clampTabSize(options?.tabSize),
    preserveScrollbackOnClear: options?.preserveScrollbackOnClear !== false,
  };

  for (let i = 0; i < input.length; i += 1) {
    const char = input[i];

    if (char === "\x1b") {
      const next = input[i + 1];
      if (next === "[") {
        let end = i + 2;
        while (end < input.length) {
          const value = input[end];
          if (value >= "@" && value <= "~") {
            break;
          }
          end += 1;
        }
        if (end >= input.length) {
          break;
        }
        const command = input[end];
        const paramsRaw = input.slice(i + 2, end);
        applyCsiSequence(state, command, paramsRaw);
        i = end;
        continue;
      }
      if (next === "7") {
        state.savedRow = state.row;
        state.savedCol = state.col;
        i += 1;
        continue;
      }
      if (next === "8") {
        state.row = Math.max(0, state.savedRow);
        state.col = Math.max(0, Math.min(state.cols, state.savedCol));
        ensureRow(state, state.row);
        i += 1;
        continue;
      }
      if (next === "D") {
        lineFeed(state, false);
        i += 1;
        continue;
      }
      if (next === "E") {
        lineFeed(state, true);
        i += 1;
        continue;
      }
      if (next === "M") {
        state.row = Math.max(0, state.row - 1);
        i += 1;
        continue;
      }
      if (next === "c") {
        state.lines = [[]];
        state.row = 0;
        state.col = 0;
        i += 1;
        continue;
      }
      if (next === "(" || next === ")" || next === "*" || next === "+" || next === "-" || next === ".") {
        if (i + 2 < input.length) {
          i += 2;
        } else {
          i += 1;
        }
        continue;
      }
      if (i + 1 < input.length) {
        i += 1;
      }
      continue;
    }

    if (char === "\r") {
      state.col = 0;
      continue;
    }
    if (char === "\n") {
      lineFeed(state, true);
      continue;
    }
    if (char === "\b") {
      state.col = Math.max(0, state.col - 1);
      continue;
    }
    if (char === "\t") {
      writeTab(state);
      continue;
    }

    const code = char.charCodeAt(0);
    if ((code >= 0 && code <= 8) || (code >= 11 && code <= 12) || (code >= 14 && code <= 31) || code === 127) {
      continue;
    }

    writeChar(state, char);
  }

  return state.lines.map((line) => line.join("")).join("\n");
}
