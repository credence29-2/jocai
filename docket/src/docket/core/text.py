"""Page text model for selection, copy and search.

Each page's characters are flattened into one string (lines joined with a space,
blocks with a newline) with a parallel list of character boxes. Selection and
search both work on string offsets, which makes matches that wrap across lines
and Shift+click extension straightforward.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

import pymupdf

Rect = tuple[float, float, float, float]


@dataclass
class PageText:
    text: str
    boxes: list[Rect | None]  # one per character in ``text``; None for joiners
    line_of: list[int]        # line number per character
    lines: list[Rect]         # bbox per line

    def rects_for(self, start: int, end: int) -> list[Rect]:
        """Merge the character boxes in [start, end) into one rect per line."""
        out: dict[int, list[float]] = {}
        for i in range(max(0, start), min(end, len(self.boxes))):
            b = self.boxes[i]
            if b is None:
                continue
            ln = self.line_of[i]
            r = out.get(ln)
            if r is None:
                out[ln] = [b[0], b[1], b[2], b[3]]
            else:
                r[0] = min(r[0], b[0]); r[1] = min(r[1], b[1])
                r[2] = max(r[2], b[2]); r[3] = max(r[3], b[3])
        return [tuple(v) for _, v in sorted(out.items())]

    def index_at(self, x: float, y: float) -> int | None:
        """Character offset nearest to a point (for starting a selection)."""
        best, best_d = None, None
        for i, b in enumerate(self.boxes):
            if b is None:
                continue
            if b[1] <= y <= b[3]:
                if b[0] <= x <= b[2]:
                    return i if x < (b[0] + b[2]) / 2 else i + 1
                dx = b[0] - x if x < b[0] else x - b[2]
                d = dx
            else:
                dy = b[1] - y if y < b[1] else y - b[3]
                dx = 0 if b[0] <= x <= b[2] else min(abs(b[0] - x), abs(b[2] - x))
                d = dy * 4 + dx
            if best_d is None or d < best_d:
                best_d = d
                cx = (b[0] + b[2]) / 2
                best = i if x < cx else i + 1
        return best

    def hit(self, x: float, y: float, slack: float = 2) -> bool:
        return any(r[0] - slack <= x <= r[2] + slack and r[1] - slack <= y <= r[3] + slack
                   for r in self.lines)

    def word_at(self, i: int) -> tuple[int, int]:
        n = len(self.text)
        if not n:
            return 0, 0
        i = min(max(i, 0), n - 1)
        s = e = i
        while s > 0 and not self.text[s - 1].isspace():
            s -= 1
        while e < n and not self.text[e].isspace():
            e += 1
        return s, e

    def line_at(self, i: int) -> tuple[int, int]:
        if not self.text:
            return 0, 0
        i = min(max(i, 0), len(self.text) - 1)
        ln = self.line_of[i]
        s = i
        while s > 0 and self.line_of[s - 1] == ln:
            s -= 1
        e = i
        while e < len(self.text) and self.line_of[e] == ln:
            e += 1
        return s, e


def extract(page: pymupdf.Page) -> PageText:
    raw = page.get_text("rawdict", flags=pymupdf.TEXTFLAGS_TEXT & ~pymupdf.TEXT_PRESERVE_IMAGES)
    text: list[str] = []
    boxes: list = []
    line_of: list[int] = []
    lines: list[Rect] = []
    ln = -1
    for block in raw.get("blocks", []):
        if block.get("type") != 0:
            continue
        if text:
            text.append("\n"); boxes.append(None); line_of.append(ln)
        first_line = True
        for line in block.get("lines", []):
            chars = [c for span in line.get("spans", []) for c in span.get("chars", [])]
            if not chars:
                continue
            ln += 1
            if not first_line:
                # previous line ended with a hyphen: keep it, still join with a space
                text.append(" "); boxes.append(None); line_of.append(ln - 1)
            first_line = False
            lines.append(tuple(line["bbox"]))
            for c in chars:
                text.append(c["c"]); boxes.append(tuple(c["bbox"])); line_of.append(ln)
    return PageText("".join(text), boxes, line_of, lines)


def selection_text(pt: PageText, start: int, end: int) -> str:
    return pt.text[start:end]


# ----- search ---------------------------------------------------------------------


def compile_query(query: str, match_case: bool = False, regex: bool = False) -> re.Pattern:
    if regex:
        pat = query
    else:
        # whitespace in the query matches any run of whitespace (line wraps)
        parts = [re.escape(p) for p in query.split()]
        pat = r"\s+".join(parts)
    return re.compile(pat, 0 if match_case else re.IGNORECASE)


def find(pt: PageText, pattern: re.Pattern) -> list[tuple[int, int]]:
    return [(m.start(), m.end()) for m in pattern.finditer(pt.text) if m.end() > m.start()]


EMAIL_RE = r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"
PHONE_RE = r"(?<!\d)(?:\+?1[\s.-]?)?(?:\(\d{3}\)\s?|\d{3}[\s.-])\d{3}[\s.-]\d{4}(?!\d)"
SSN_RE = r"(?<!\d)\d{3}-\d{2}-\d{4}(?!\d)"
