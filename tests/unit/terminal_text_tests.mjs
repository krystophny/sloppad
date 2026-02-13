import { normalizeTerminalOutput } from "../../src/tabula/web/static/terminal-text.js";

let passed = 0;
let failed = 0;

function assert(actual, expected, label) {
  if (actual === expected) {
    passed++;
  } else {
    failed++;
    console.error(`FAIL: ${label}`);
    console.error(`  expected: ${JSON.stringify(expected)}`);
    console.error(`  actual:   ${JSON.stringify(actual)}`);
  }
}

assert(
  normalizeTerminalOutput("line1\r\nline2\r\n"),
  "line1\nline2\n",
  "normalizes CRLF to LF"
);

assert(
  normalizeTerminalOutput("abcdef", { cols: 4 }),
  "abcd\nef",
  "wraps output to configured columns"
);

assert(
  normalizeTerminalOutput("10%\r20%\r100%\n"),
  "100%\n",
  "applies carriage return overwrite semantics"
);

assert(
  normalizeTerminalOutput("booting\r\x1b[2Kready\r\nnext\r\x1b[2Kdone\r\n"),
  "ready\ndone\n",
  "handles mixed ANSI clear-line and CRLF redraw sequences"
);

assert(
  normalizeTerminalOutput("abc\rX\n"),
  "Xbc\n",
  "keeps residual characters when shorter overwrite occurs"
);

assert(
  normalizeTerminalOutput("\x1b]0;title\x07\x1b[31mred\x1b[0m\r\nok\n"),
  "red\nok\n",
  "strips ANSI and OSC control sequences"
);

assert(
  normalizeTerminalOutput("processing file 123\r\x1b[2Kdone\n"),
  "done\n",
  "applies clear-line CSI sequence"
);

assert(
  normalizeTerminalOutput("hello world\r\x1b[6C\x1b[5X\n"),
  "hello      \n",
  "applies erase-chars CSI sequence"
);

assert(
  normalizeTerminalOutput("abcd\r\x1b[2C\x1b[@X\n"),
  "abXcd\n",
  "applies insert-char CSI sequence"
);

assert(
  normalizeTerminalOutput("abcdef\r\x1b[2C\x1b[2P\n"),
  "abef\n",
  "applies delete-char CSI sequence"
);

assert(
  normalizeTerminalOutput("ab\bX\n"),
  "aX\n",
  "applies backspace overwrite semantics"
);

assert(
  normalizeTerminalOutput("\x1b(0lqk\x1b(B\n"),
  "lqk\n",
  "ignores charset designation escapes"
);

assert(
  normalizeTerminalOutput("line1\nline2\n\x1b[1A\r\x1b[2KlineX\n"),
  "line1\nlineX\n",
  "applies cursor-up redraw semantics"
);

assert(
  normalizeTerminalOutput("a\nb\nc\x1b[1A\r\x1b[0JX\n"),
  "a\nX\n",
  "keeps later lines when clear-to-end-of-screen is used"
);

assert(
  normalizeTerminalOutput("one\ntwo\n\x1b[2Jthree\n"),
  "one\ntwo\n\nthree\n",
  "treats CSI 2J as clear-screen while preserving scrollback"
);

assert(
  normalizeTerminalOutput("one\ntwo\n\x1b[2Jthree\n", { preserveScrollbackOnClear: false }),
  "three\n",
  "resets screen on CSI 2J when scrollback preservation is disabled"
);

assert(
  normalizeTerminalOutput("aaaa\nbbbb\ncccc\x1b[2;2HZZ\n"),
  "aaaa\nbZZb\ncccc",
  "applies CUP absolute cursor positioning"
);

assert(
  normalizeTerminalOutput("line1\nline2\nline3\x1b[2;1H\x1b[Lnew\n"),
  "line1\nnew\nline2\nline3",
  "applies insert-line CSI sequence"
);

assert(
  normalizeTerminalOutput("line1\nline2\nline3\x1b[2;1H\x1b[M"),
  "line1\nline3",
  "applies delete-line CSI sequence"
);

assert(
  normalizeTerminalOutput("abc\x1b[s123\x1b[uZ\n"),
  "abcZ23\n",
  "applies save and restore cursor position"
);

assert(
  normalizeTerminalOutput("a\tb\n", { tabSize: 4 }),
  "a   b\n",
  "uses tab stops with configurable tab size"
);

assert(
  normalizeTerminalOutput("abcde\r\x1b[3C\x1b[1K\n"),
  "   de\n",
  "applies clear-line to-cursor CSI sequence"
);

assert(
  normalizeTerminalOutput("abc\ndef\x1b[2;2H\x1b[0JX\n"),
  "abc\ndX\n",
  "applies clear-display from cursor CSI sequence"
);

assert(
  normalizeTerminalOutput("abc\ndef\x1b[2;2H\x1b[1JX\n"),
  "\n Xf\n",
  "applies clear-display to cursor CSI sequence"
);

assert(
  normalizeTerminalOutput("one\ntwo\n\x1b[3Jthree\n"),
  "three\n",
  "applies clear-display with scrollback clear mode"
);

assert(
  normalizeTerminalOutput("abc\x1b[1GZ\n"),
  "Zbc\n",
  "applies cursor horizontal absolute CSI sequence"
);

assert(
  normalizeTerminalOutput("abc\r\x1b[5C\x1b[2DZ\n"),
  "abcZ\n",
  "applies cursor forward/backward CSI sequences"
);

assert(
  normalizeTerminalOutput("top\x1b[2B\x1b[1Ebottom\n"),
  "top\n\n\nbottom\n",
  "applies cursor down and next-line CSI sequences"
);

assert(
  normalizeTerminalOutput("line1\nline2\n\x1b[1FZ\n"),
  "line1\nZine2\n",
  "applies cursor previous-line CSI sequence"
);

assert(
  normalizeTerminalOutput("abc\x1b[?;fZ\n"),
  "Zbc\n",
  "supports CUP alias with fallback numeric params"
);

assert(
  normalizeTerminalOutput("abc\x1b7XX\x1b8Z\x1bDa\x1bEb\n"),
  "abcZX\n    a\nb\n",
  "handles DEC save/restore cursor and escape new-line commands"
);

assert(
  normalizeTerminalOutput("ab\ncd\x1b[2;1H\x1bMX\x1bcnew\n"),
  "new\n",
  "handles ESC reverse index and full reset"
);

assert(
  normalizeTerminalOutput("ab\x1b#cd\x1b("),
  "abcd",
  "skips unknown or incomplete escape sequences safely"
);

assert(
  normalizeTerminalOutput("abc\x1b[31"),
  "abc",
  "stops parsing an incomplete CSI sequence at end of input"
);

assert(
  normalizeTerminalOutput("a\x01\x7fb\n"),
  "ab\n",
  "ignores additional non-printable C0 and DEL control characters"
);

assert(
  normalizeTerminalOutput("abcd\r\x1b[2C\x1b[4@Z", { cols: 6 }),
  "abZ   ",
  "trims inserted characters at column limit"
);

assert(
  normalizeTerminalOutput("a\r\x1b[-2CZ\n"),
  "aZ\n",
  "uses fallback movement for invalid numeric CSI params"
);

const tabOutput = normalizeTerminalOutput("a\tb\n", { tabSize: 0 });
const colsOutput = normalizeTerminalOutput("abcde", { cols: Number.NaN });
assert(tabOutput, "a b\n", "clamps invalid tab size");
assert(colsOutput, "abcde", "ignores non-finite column values");

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
