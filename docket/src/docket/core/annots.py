"""Comments and markup: highlight, underline, strikethrough, notes, text boxes,
plain text, pen, shapes, arrows, signatures. Plus listing/editing existing ones."""

from __future__ import annotations

from dataclasses import dataclass

import pymupdf

from .document import Document

SIGNATURE_SUBJECT = "Docket Signature"
PLAIN_TEXT_SUBJECT = "Docket Text"

HIGHLIGHT = pymupdf.PDF_ANNOT_HIGHLIGHT
UNDERLINE = pymupdf.PDF_ANNOT_UNDERLINE
STRIKEOUT = pymupdf.PDF_ANNOT_STRIKE_OUT
SQUIGGLY = pymupdf.PDF_ANNOT_SQUIGGLY
NOTE = pymupdf.PDF_ANNOT_TEXT
FREETEXT = pymupdf.PDF_ANNOT_FREE_TEXT
INK = pymupdf.PDF_ANNOT_INK
SQUARE = pymupdf.PDF_ANNOT_SQUARE
CIRCLE = pymupdf.PDF_ANNOT_CIRCLE
LINE = pymupdf.PDF_ANNOT_LINE
STAMP = pymupdf.PDF_ANNOT_STAMP
REDACT = pymupdf.PDF_ANNOT_REDACT
WIDGET = pymupdf.PDF_ANNOT_WIDGET
LINK = pymupdf.PDF_ANNOT_LINK
POPUP = pymupdf.PDF_ANNOT_POPUP

TYPE_NAMES = {
    HIGHLIGHT: "Highlight", UNDERLINE: "Underline", STRIKEOUT: "Strikethrough",
    SQUIGGLY: "Squiggly", NOTE: "Sticky Note", FREETEXT: "Text Box", INK: "Drawing",
    SQUARE: "Rectangle", CIRCLE: "Oval", LINE: "Line", STAMP: "Stamp", REDACT: "Redaction Mark",
    pymupdf.PDF_ANNOT_POLYGON: "Polygon", pymupdf.PDF_ANNOT_POLY_LINE: "Polyline",
    pymupdf.PDF_ANNOT_CARET: "Caret", pymupdf.PDF_ANNOT_FILE_ATTACHMENT: "Attachment",
}

MARKUP = {HIGHLIGHT, UNDERLINE, STRIKEOUT, SQUIGGLY}
RESIZABLE = {FREETEXT, SQUARE, CIRCLE, STAMP, REDACT}


@dataclass
class AnnotInfo:
    page: int
    xref: int
    type: int
    type_name: str
    rect: tuple
    content: str
    author: str
    modified: str
    color: tuple | None
    width: float
    fontsize: float
    is_signature: bool
    is_plain_text: bool
    vertices: list | None


def _rgb(c):
    return tuple(c) if c else None


def _fontsize_of(a: pymupdf.Annot) -> float:
    if a.type[0] != FREETEXT:
        return 0
    da = a.parent.parent.xref_get_key(a.xref, "DA")[1] or ""
    import re
    m = re.search(r"([\d.]+)\s+Tf", da)
    return float(m.group(1)) if m else 11


def _text_color(a: pymupdf.Annot):
    da = a.parent.parent.xref_get_key(a.xref, "DA")[1] or ""
    import re
    m = re.search(r"([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+rg", da)
    if m:
        return tuple(float(x) for x in m.groups())
    m = re.search(r"([\d.]+)\s+g", da)
    if m:
        v = float(m.group(1)); return (v, v, v)
    return (0, 0, 0)


def info(page_no: int, a: pymupdf.Annot) -> AnnotInfo:
    t = a.type[0]
    i = a.info
    subj = i.get("subject", "")
    color = None
    if t == FREETEXT:
        color = _text_color(a)
    else:
        cols = a.colors or {}
        color = _rgb(cols.get("stroke")) or _rgb(cols.get("fill"))
    return AnnotInfo(
        page=page_no, xref=a.xref, type=t, type_name=TYPE_NAMES.get(t, a.type[1]),
        rect=tuple(a.rect), content=i.get("content", ""), author=i.get("title", ""),
        modified=i.get("modDate", ""), color=color,
        width=(a.border or {}).get("width", 1) or 1, fontsize=_fontsize_of(a),
        is_signature=t == STAMP and subj == SIGNATURE_SUBJECT,
        is_plain_text=t == FREETEXT and subj == PLAIN_TEXT_SUBJECT,
        vertices=a.vertices if t in (INK, LINE) else None,
    )


def list_page(doc: Document, page: int) -> list[AnnotInfo]:
    p = doc.pdf[page]
    return [info(page, a) for a in p.annots() if a.type[0] not in (POPUP, LINK, WIDGET)]


def list_all(doc: Document) -> list[AnnotInfo]:
    out = []
    for i in range(doc.page_count):
        out.extend(list_page(doc, i))
    return out


def _get(doc: Document, page: int, xref: int) -> tuple[pymupdf.Page, pymupdf.Annot]:
    """The page is returned too: an annotation stops working once its page is freed."""
    p = doc.pdf[page]
    a = p.load_annot(xref)
    if a is None:
        raise KeyError(xref)
    return p, a


def _stamp_author(a: pymupdf.Annot, author: str):
    if author:
        a.set_info(title=author)


# ----- creation ----------------------------------------------------------------------


def add_markup(doc: Document, page: int, kind: int, rects: list, color, author="") -> int:
    with doc.edit({HIGHLIGHT: "Highlight", UNDERLINE: "Underline",
                   STRIKEOUT: "Strikethrough"}.get(kind, "Markup"), pages=[page]):
        p = doc.pdf[page]
        quads = [pymupdf.Rect(r).quad for r in rects]
        fn = {HIGHLIGHT: p.add_highlight_annot, UNDERLINE: p.add_underline_annot,
              STRIKEOUT: p.add_strikeout_annot, SQUIGGLY: p.add_squiggly_annot}[kind]
        a = fn(quads)
        a.set_colors(stroke=color)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_note(doc: Document, page: int, point, text: str, color=(1, 0.85, 0.2), author="") -> int:
    with doc.edit("Sticky Note", pages=[page]):
        p = doc.pdf[page]
        a = p.add_text_annot(pymupdf.Point(point), text, icon="Comment")
        a.set_colors(stroke=color)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_textbox(doc: Document, page: int, rect, text: str, fontsize=12, color=(0, 0, 0),
                border=True, author="") -> int:
    label = "Text Box" if border else "Add Text"
    with doc.edit(label, pages=[page]):
        p = doc.pdf[page]
        a = p.add_freetext_annot(
            pymupdf.Rect(rect), text, fontsize=fontsize, fontname="helv",
            text_color=color, border_width=1 if border else 0,  # border uses the text colour
            fill_color=(1, 1, 1) if border else None,
        )
        if not border:
            a.set_info(subject=PLAIN_TEXT_SUBJECT)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_ink(doc: Document, page: int, strokes: list[list[tuple]], color, width=2, author="") -> int:
    with doc.edit("Draw", pages=[page]):
        p = doc.pdf[page]
        a = p.add_ink_annot(strokes)
        a.set_colors(stroke=color)
        a.set_border(width=width)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_shape(doc: Document, page: int, kind: int, rect, color, width=2, author="") -> int:
    with doc.edit("Rectangle" if kind == SQUARE else "Oval", pages=[page]):
        p = doc.pdf[page]
        r = pymupdf.Rect(rect)
        a = p.add_rect_annot(r) if kind == SQUARE else p.add_circle_annot(r)
        a.set_colors(stroke=color)
        a.set_border(width=width)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_arrow(doc: Document, page: int, p1, p2, color, width=2, arrow=True, author="") -> int:
    with doc.edit("Arrow" if arrow else "Line", pages=[page]):
        p = doc.pdf[page]
        a = p.add_line_annot(pymupdf.Point(p1), pymupdf.Point(p2))
        a.set_colors(stroke=color)
        a.set_border(width=width)
        if arrow:
            a.set_line_ends(pymupdf.PDF_ANNOT_LE_NONE, pymupdf.PDF_ANNOT_LE_OPEN_ARROW)
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_signature(doc: Document, page: int, rect, png: bytes, author="") -> int:
    with doc.edit("Sign", pages=[page]):
        p = doc.pdf[page]
        a = p.add_stamp_annot(pymupdf.Rect(rect), stamp=png)
        a.set_info(subject=SIGNATURE_SUBJECT, content="Signature")
        _stamp_author(a, author)
        a.update()
        return a.xref


def add_stamp(doc: Document, page: int, rect, stamp_index: int, author="") -> int:
    with doc.edit("Stamp", pages=[page]):
        a = doc.pdf[page].add_stamp_annot(pymupdf.Rect(rect), stamp=stamp_index)
        _stamp_author(a, author)
        a.update()
        return a.xref


STAMPS = ["Approved", "As Is", "Confidential", "Departmental", "Experimental", "Expired",
          "Final", "For Comment", "For Public Release", "Not Approved",
          "Not For Public Release", "Sold", "Top Secret", "Draft"]


# ----- editing -------------------------------------------------------------------------


def move(doc: Document, page: int, xref: int, dx: float, dy: float):
    with doc.edit("Move", pages=[page]):
        _p, a = _get(doc, page, xref)
        t = a.type[0]
        if t in (INK, LINE) or t in MARKUP or t == pymupdf.PDF_ANNOT_POLYGON:
            _translate_vertices(doc, a, dx, dy)
        else:
            r = a.rect
            a.set_rect(pymupdf.Rect(r.x0 + dx, r.y0 + dy, r.x1 + dx, r.y1 + dy))
            if t == FREETEXT:
                a.update(**_freetext_update_args(a))
            elif t not in (STAMP,):
                a.update()


def _freetext_update_args(a: pymupdf.Annot) -> dict:
    return dict(fontsize=_fontsize_of(a), text_color=_text_color(a))


def _translate_vertices(doc: Document, a: pymupdf.Annot, dx: float, dy: float):
    """Move vertex-based annotations by rewriting their geometry keys."""
    pdf = doc.pdf
    x = a.xref
    t = a.type[0]
    page = a.parent
    # PDF space has y up; account for page height and rotation via the page matrix
    m = page.transformation_matrix
    inv = ~m
    p0 = pymupdf.Point(0, 0) * inv
    p1 = pymupdf.Point(dx, dy) * inv
    pdx, pdy = p1.x - p0.x, p1.y - p0.y

    def shift(arr: str, step=2):
        nums = [float(v) for v in arr.strip("[] ").split()]
        for k in range(0, len(nums), 2):
            nums[k] += pdx
            nums[k + 1] += pdy
        return "[" + " ".join(f"{v:g}" for v in nums) + "]"

    if t == INK:
        kind, val = pdf.xref_get_key(x, "InkList")
        import re
        parts = re.findall(r"\[[^\[\]]*\]", val)
        pdf.xref_set_key(x, "InkList", "[" + " ".join(shift(p) for p in parts) + "]")
    elif t == LINE:
        pdf.xref_set_key(x, "L", shift(pdf.xref_get_key(x, "L")[1]))
    elif t in MARKUP:
        pdf.xref_set_key(x, "QuadPoints", shift(pdf.xref_get_key(x, "QuadPoints")[1]))
    elif t == pymupdf.PDF_ANNOT_POLYGON:
        pdf.xref_set_key(x, "Vertices", shift(pdf.xref_get_key(x, "Vertices")[1]))
    r = a.rect
    a.set_rect(pymupdf.Rect(r.x0 + dx, r.y0 + dy, r.x1 + dx, r.y1 + dy))
    a = page.load_annot(x)
    a.update()


def resize(doc: Document, page: int, xref: int, rect):
    with doc.edit("Resize", pages=[page]):
        _p, a = _get(doc, page, xref)
        a.set_rect(pymupdf.Rect(rect))
        if a.type[0] == FREETEXT:
            a.update(**_freetext_update_args(a))
        elif a.type[0] != STAMP:
            a.update()


def set_text(doc: Document, page: int, xref: int, text: str, rect=None):
    with doc.edit("Edit Text", pages=[page]):
        _p, a = _get(doc, page, xref)
        a.set_info(content=text)
        if rect is not None:
            a.set_rect(pymupdf.Rect(rect))
        if a.type[0] == FREETEXT:
            a.update(**_freetext_update_args(a))
        else:
            a.update()


def fit_freetext(doc: Document, page: int, xref: int, rect):
    """Grow/shrink a plain-text box to its text (no undo step of its own)."""
    _p, a = _get(doc, page, xref)
    a.set_rect(pymupdf.Rect(rect))
    a.update(**_freetext_update_args(a))


def set_style(doc: Document, page: int, xref: int, color=None, width=None, fontsize=None):
    with doc.edit("Change Style", pages=[page]):
        _p, a = _get(doc, page, xref)
        t = a.type[0]
        if t == FREETEXT:
            args = _freetext_update_args(a)
            if fontsize:
                args["fontsize"] = fontsize
            if color is not None:
                args["text_color"] = color
            a.update(**args)
            return
        if color is not None:
            if t in (NOTE,) or t in MARKUP or t in (INK, SQUARE, CIRCLE, LINE):
                a.set_colors(stroke=color)
        if width is not None and t in (INK, SQUARE, CIRCLE, LINE):
            a.set_border(width=width)
        a.update()


def delete(doc: Document, page: int, xrefs: list[int]):
    with doc.edit("Delete", pages=[page]):
        p = doc.pdf[page]
        for x in xrefs:
            a = p.load_annot(x)
            if a is not None:
                p.delete_annot(a)


def hit_test(doc: Document, page: int, x: float, y: float, tol: float = 3) -> AnnotInfo | None:
    """Top-most annotation under a point. Markup annotations hit on their quads."""
    p = doc.pdf[page]
    pt = pymupdf.Point(x, y)
    found = None
    for a in p.annots():
        t = a.type[0]
        if t in (POPUP, LINK, WIDGET):
            continue
        r = pymupdf.Rect(a.rect)
        r.x0 -= tol; r.y0 -= tol; r.x1 += tol; r.y1 += tol
        if not r.contains(pt):
            continue
        if t in MARKUP and a.vertices:
            v = a.vertices
            hit = False
            for k in range(0, len(v) - 3, 4):
                q = pymupdf.Quad(v[k], v[k + 1], v[k + 2], v[k + 3]).rect
                if pymupdf.Rect(q.x0 - 1, q.y0 - 1, q.x1 + 1, q.y1 + 1).contains(pt):
                    hit = True
                    break
            if not hit:
                continue
        if t == INK and a.vertices:
            if not _near_polyline(a.vertices, pt, max(tol, 4)):
                continue
        if t == LINE and a.vertices:
            if not _near_polyline([a.vertices], pt, max(tol, 4)):
                continue
        found = info(page, a)  # later annots draw on top
    return found


def _near_polyline(strokes, pt, tol) -> bool:
    for s in strokes:
        pts = [pymupdf.Point(p) for p in s]
        for a, b in zip(pts, pts[1:]):
            if _dist_seg(pt, a, b) <= tol:
                return True
        if len(pts) == 1 and abs(pts[0] - pt) <= tol:
            return True
    return False


def _dist_seg(p, a, b) -> float:
    dx, dy = b.x - a.x, b.y - a.y
    if dx == dy == 0:
        return abs(p - a)
    t = max(0, min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / (dx * dx + dy * dy)))
    return abs(p - pymupdf.Point(a.x + t * dx, a.y + t * dy))
