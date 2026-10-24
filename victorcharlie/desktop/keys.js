// Maps browser key names (KeyboardEvent.code, as the web app stores them)
// to uiohook key codes, so push-to-talk can be read system-wide.
const { UiohookKey } = require("uiohook-napi");

const special = {
  Backquote: "Backquote", Minus: "Minus", Equal: "Equal", BracketLeft: "BracketLeft", BracketRight: "BracketRight",
  Backslash: "Backslash", Semicolon: "Semicolon", Quote: "Quote", Comma: "Comma", Period: "Period", Slash: "Slash",
  Space: "Space", Tab: "Tab", Enter: "Enter", Escape: "Escape", Backspace: "Backspace", CapsLock: "CapsLock",
  Insert: "Insert", Delete: "Delete", Home: "Home", End: "End", PageUp: "PageUp", PageDown: "PageDown",
  ArrowUp: "ArrowUp", ArrowDown: "ArrowDown", ArrowLeft: "ArrowLeft", ArrowRight: "ArrowRight",
  ShiftLeft: "Shift", ShiftRight: "ShiftRight", ControlLeft: "Ctrl", ControlRight: "CtrlRight",
  AltLeft: "Alt", AltRight: "AltRight", MetaLeft: "Meta", MetaRight: "MetaRight",
  NumLock: "NumLock", ScrollLock: "ScrollLock", PrintScreen: "PrintScreen",
  NumpadMultiply: "NumpadMultiply", NumpadAdd: "NumpadAdd", NumpadSubtract: "NumpadSubtract",
  NumpadDecimal: "NumpadDecimal", NumpadDivide: "NumpadDivide", NumpadEnter: "NumpadEnter",
};

// Returns { key } for keyboard codes or { mouse } for "MouseN" (browser
// button numbers: 3 = back, 4 = forward), or null.
function parse(code) {
  if (!code) return null;
  const m = /^Mouse(\d)$/.exec(code);
  if (m) return { mouse: Number(m[1]) + 1 }; // uiohook counts from 1
  let name = special[code];
  if (!name) {
    let k = /^Key([A-Z])$/.exec(code) || /^Digit(\d)$/.exec(code) || /^(F\d{1,2})$/.exec(code) || /^(Numpad\d)$/.exec(code);
    if (k) name = k[1];
  }
  if (!name || UiohookKey[name] === undefined) return null;
  return { key: UiohookKey[name] };
}

// "Ctrl+Shift+KeyM" -> { key, ctrl, shift, alt, meta }
function parseCombo(combo) {
  if (!combo) return null;
  const parts = combo.split("+");
  const main = parse(parts.pop());
  if (!main || main.mouse) return null;
  return { key: main.key, ctrl: parts.includes("Ctrl"), alt: parts.includes("Alt"), shift: parts.includes("Shift"), meta: parts.includes("Meta") };
}

module.exports = { parse, parseCombo };
