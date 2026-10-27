"""Suggest form fields on flat (non-fillable) PDFs.

Looks for blank underlines (drawn lines and ``____`` runs), empty table cells,
"Label:" cells with room after the label, and checkbox squares. Each suggestion is
named after the nearest label to its left (or above).
"""

from __future__ import annotations

import re

import pymupdf

from .document import Document

MIN_LINE = 36          # shortest underline worth a field (pt)
FIELD_H = 16           # default text field height
BOX_MIN, BOX_MAX = 6, 18
BOX_CHARS = "☐□❏❑▢"


def _words(page):
    return [(pymupdf.Rect(w[:4]), w[4]) for w in page.get_text("words")]


def _overlaps_text(rect, words, ignore=("_",)) -> bool:
    for r, t in words:
        if set(t) <= set(ignore):
            continue
        if r.intersects(rect) and (r & rect).get_area() > 0.25 * r.get_area():
            return True
    return False


def _label_for(rect, words) -> str:
    """Words on the same line to the left, closest first, up to the previous gap."""
    cy = (rect.y0 + rect.y1) / 2
    same = [(r, t) for r, t in words
            if r.x1 <= rect.x0 + 2 and r.y0 - 4 <= cy <= r.y1 + 6 and rect.x0 - r.x1 < 220
            and not set(t) <= {"_"}]
    same.sort(key=lambda w: -w[0].x1)
    picked = []
    edge = rect.x0
    for r, t in same:
        if edge - r.x1 > 18 and picked:
            break
        picked.append(t)
        edge = r.x0
        if len(picked) >= 5:
            break
    if picked:
        return " ".join(reversed(picked))
    above = [(r, t) for r, t in words
             if r.y1 <= rect.y0 + 2 and rect.y0 - r.y1 < 24 and r.x0 < rect.x1 and r.x1 > rect.x0
             and not set(t) <= {"_"}]
    if above:
        top = max(r.y1 for r, _ in above)
        return " ".join(t for r, t in sorted(above, key=lambda w: w[0].x0) if abs(r.y1 - top) < 3)
    return ""


def _name(label: str) -> str:
    label = label.strip().rstrip(":").strip()
    return re.sub(r"[^A-Za-z0-9]+", "_", label).strip("_")[:40] or "Field"


def _kind(label: str) -> str:
    return "date" if re.search(r"\bdate\b|\bdob\b", label, re.I) else "text"


def _horizontal_segments(page):
    segs = []
    for d in page.get_drawings():
        for item in d.get("items", []):
            if item[0] == "l":
                p1, p2 = item[1], item[2]
                if abs(p1.y - p2.y) < 1 and abs(p2.x - p1.x) >= MIN_LINE:
                    segs.append((min(p1.x, p2.x), max(p1.x, p2.x), (p1.y + p2.y) / 2))
            elif item[0] == "re":
                r = item[1]
                if r.height <= 2 and r.width >= MIN_LINE:
                    segs.append((r.x0, r.x1, (r.y0 + r.y1) / 2))
    return segs


def _squares(page):
    out = []
    for d in page.get_drawings():
        fill = d.get("fill")
        if fill and sum(fill) < 1.5:  # dark filled: not an empty box
            continue
        for item in d.get("items", []):
            if item[0] == "re":
                r = item[1]
            elif item[0] == "qu":
                r = item[1].rect
            else:
                continue
            if BOX_MIN <= r.width <= BOX_MAX and abs(r.width - r.height) < 1.5:
                out.append(pymupdf.Rect(r))
        # squares drawn as four lines
        items = d.get("items", [])
        if len(items) == 4 and all(i[0] == "l" for i in items):
            pts = [i[1] for i in items] + [i[2] for i in items]
            r = pymupdf.Rect(min(p.x for p in pts), min(p.y for p in pts),
                             max(p.x for p in pts), max(p.y for p in pts))
            if BOX_MIN <= r.width <= BOX_MAX and abs(r.width - r.height) < 1.5:
                out.append(r)
    return out


def _dedupe(cands):
    out = []
    for c in sorted(cands, key=lambda c: -c["rect"].get_area()):
        if any((c["rect"] & o["rect"]).get_area() > 0.3 * min(c["rect"].get_area(), o["rect"].get_area())
               for o in out):
            continue
        out.append(c)
    return out


def detect_page(doc: Document, page_no: int) -> list[dict]:
    page = doc.pdf[page_no]
    words = _words(page)
    existing = [pymupdf.Rect(w.rect) for w in (page.widgets() or [])]
    cands = []

    # table cells first: they explain many of the lines on a page
    cell_rects = []
    try:
        tables = page.find_tables().tables
    except Exception:
        tables = []
    for t in tables:
        for c in t.cells:
            if not c:
                continue
            r = pymupdf.Rect(c)
            cell_rects.append(r)
            if r.height < 10 or r.height > 80 or r.width < 30:
                continue
            inner = [(wr, wt) for wr, wt in words if r.contains((wr.tl + wr.br) / 2)]
            pad = 2
            if not inner:
                label = _label_for(r, words) or _cell_header(t, c, words)
                field = pymupdf.Rect(r.x0 + pad, r.y0 + pad, r.x1 - pad, r.y1 - pad)
                cands.append({"page": page_no, "rect": _cap_height(field), "kind": _kind(label),
                              "name": _name(label)})
            else:
                last = max(inner, key=lambda w: w[0].x1)
                label_text = " ".join(wt for _, wt in sorted(inner, key=lambda w: (round(w[0].y0), w[0].x0)))
                if label_text.rstrip().endswith(":") and r.x1 - last[0].x1 > 60 and len(inner) <= 6:
                    field = pymupdf.Rect(last[0].x1 + 4, r.y0 + pad, r.x1 - pad, r.y1 - pad)
                    cands.append({"page": page_no, "rect": _cap_height(field), "kind": _kind(label_text),
                                  "name": _name(label_text)})

    # drawn underlines
    for x0, x1, y in _horizontal_segments(page):
        line = pymupdf.Rect(x0, y - FIELD_H, x1, y)
        if any(abs(cr.y1 - y) < 2 and cr.x0 - 2 <= x0 and x1 <= cr.x1 + 2 for cr in cell_rects):
            continue  # a table border
        top = max([r.y1 for r, t in words if r.y1 <= y - 2 and r.x0 < x1 and r.x1 > x0 and y - r.y1 < FIELD_H + 2]
                  or [y - FIELD_H])
        field = pymupdf.Rect(x0, max(top + 1, y - FIELD_H), x1, y - 0.5)
        if field.height < 8 or _overlaps_text(field, words):
            continue
        label = _label_for(field, words)
        cands.append({"page": page_no, "rect": field, "kind": _kind(label), "name": _name(label)})

    # ____ runs in the text
    raw = page.get_text("rawdict")
    for block in raw.get("blocks", []):
        for line in block.get("lines", []):
            chars = [c for s in line.get("spans", []) for c in s.get("chars", [])]
            i = 0
            while i < len(chars):
                if chars[i]["c"] == "_":
                    j = i
                    while j < len(chars) and chars[j]["c"] == "_":
                        j += 1
                    if j - i >= 3:
                        b0, b1 = chars[i]["bbox"], chars[j - 1]["bbox"]
                        base = max(b0[3], b1[3])
                        field = pymupdf.Rect(b0[0], base - FIELD_H, b1[2], base - 1)
                        if field.width >= 24:
                            label = _label_for(field, words)
                            cands.append({"page": page_no, "rect": field, "kind": _kind(label),
                                          "name": _name(label)})
                    i = j
                else:
                    if chars[i]["c"] in BOX_CHARS:
                        b = pymupdf.Rect(chars[i]["bbox"])
                        s = min(b.width, b.height)
                        box = pymupdf.Rect(b.x0, b.y1 - s, b.x0 + s, b.y1)
                        cands.append({"page": page_no, "rect": box, "kind": "checkbox",
                                      "name": _name(_right_label(box, words) or "Check")})
                    i += 1

    # checkbox squares
    for sq in _squares(page):
        if _overlaps_text(sq, words):
            continue
        cands.append({"page": page_no, "rect": sq, "kind": "checkbox",
                      "name": _name(_right_label(sq, words) or _label_for(sq, words) or "Check")})

    cands = [c for c in cands if not any(c["rect"].intersects(e) for e in existing)]
    out = _dedupe(cands)
    for c in out:
        c["rect"] = tuple(c["rect"])
    out.sort(key=lambda c: (round(c["rect"][1]), c["rect"][0]))
    return out


def _cap_height(r: pymupdf.Rect) -> pymupdf.Rect:
    if r.height > 30:  # tall empty cell: use a single line at the top unless it's a box
        return r
    return r


def _cell_header(table, cell, words) -> str:
    try:
        hdr = table.header
        if hdr and hdr.names:
            col = None
            for k, c in enumerate(table.rows[0].cells):
                if c and abs(c[0] - cell[0]) < 2:
                    col = k
                    break
            if col is not None and col < len(hdr.names) and hdr.names[col]:
                return str(hdr.names[col])
    except Exception:
        pass
    return ""


def _right_label(box, words) -> str:
    cy = (box.y0 + box.y1) / 2
    right = [(r, t) for r, t in words if r.x0 >= box.x1 - 1 and r.y0 - 3 <= cy <= r.y1 + 3 and r.x0 - box.x1 < 120]
    right.sort(key=lambda w: w[0].x0)
    picked, edge = [], box.x1
    for r, t in right:
        if r.x0 - edge > 12:
            break
        picked.append(t)
        edge = r.x1
        if len(picked) >= 4:
            break
    return " ".join(picked)


def detect(doc: Document, progress=None) -> list[dict]:
    out = []
    for i in range(doc.page_count):
        out.extend(detect_page(doc, i))
        if progress and progress(i + 1, doc.page_count) is False:
            break
    return out
