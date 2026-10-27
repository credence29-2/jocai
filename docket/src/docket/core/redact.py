"""True redaction: marks, find-and-mark, and apply (which removes content)."""

from __future__ import annotations

import re

import pymupdf

from . import text as textmod
from .document import Document

MARK_FILL = (0, 0, 0)


def add_marks(doc: Document, page: int, rects: list, label="Mark for Redaction") -> list[int]:
    with doc.edit(label, pages=[page]):
        return _add(doc, page, rects)


def _add(doc, page, rects) -> list[int]:
    p = doc.pdf[page]
    out = []
    for r in rects:
        a = p.add_redact_annot(pymupdf.Rect(r), fill=MARK_FILL, cross_out=True)
        a.set_colors(stroke=(0.85, 0, 0))
        a.update()
        out.append(a.xref)
    return out


def find_matches(doc: Document, query: str = "", match_case=False, whole_word=False,
                 emails=False, phones=False, ssns=False) -> dict[int, list[tuple]]:
    pats = []
    if query.strip():
        q = r"\s+".join(re.escape(p) for p in query.split())
        if whole_word:
            q = rf"(?<!\w){q}(?!\w)"
        pats.append(q)
    if emails:
        pats.append(textmod.EMAIL_RE)
    if phones:
        pats.append(textmod.PHONE_RE)
    if ssns:
        pats.append(textmod.SSN_RE)
    if not pats:
        return {}
    rx = re.compile("|".join(f"(?:{p})" for p in pats), 0 if match_case else re.IGNORECASE)
    out: dict[int, list[tuple]] = {}
    for i in range(doc.page_count):
        pt = textmod.extract(doc.pdf[i])
        rects = []
        for s, e in textmod.find(pt, rx):
            rects.extend(pt.rects_for(s, e))
        if rects:
            out[i] = rects
    return out


def mark_matches(doc: Document, matches: dict[int, list[tuple]]) -> int:
    if not matches:
        return 0
    n = 0
    with doc.edit("Find and Redact", pages=list(matches)):
        for page, rects in matches.items():
            n += len(_add(doc, page, rects))
    return n


def marks(doc: Document) -> dict[int, list[int]]:
    out = {}
    for i in range(doc.page_count):
        xs = [a.xref for a in doc.pdf[i].annots(types=[pymupdf.PDF_ANNOT_REDACT])]
        if xs:
            out[i] = xs
    return out


def count_marks(doc: Document) -> int:
    return sum(len(v) for v in marks(doc).values())


def clear_marks(doc: Document):
    m = marks(doc)
    if not m:
        return
    with doc.edit("Remove Redaction Marks", pages=list(m)):
        for page, xs in m.items():
            p = doc.pdf[page]
            for x in xs:
                p.delete_annot(p.load_annot(x))


def apply(doc: Document, remove_touching_graphics=True, clear_metadata=True) -> int:
    """Permanently remove everything under the marks. Returns marks applied."""
    m = marks(doc)
    if not m:
        return 0
    with doc.edit("Apply Redactions", pages=list(m)):
        for page in m:
            p = doc.pdf[page]
            areas = [pymupdf.Rect(a.rect) for a in p.annots(types=[pymupdf.PDF_ANNOT_REDACT])]
            # notes, form fields and links under the marks go too
            for a in list(p.annots()):
                if a.type[0] == pymupdf.PDF_ANNOT_REDACT:
                    continue
                if any(pymupdf.Rect(a.rect).intersects(r) for r in areas):
                    p.delete_annot(a)
            for w in list(p.widgets() or []):
                if any(pymupdf.Rect(w.rect).intersects(r) for r in areas):
                    p.delete_widget(w)
            for link in p.get_links():
                if any(pymupdf.Rect(link["from"]).intersects(r) for r in areas):
                    p.delete_link(link)
            p.apply_redactions(
                images=pymupdf.PDF_REDACT_IMAGE_PIXELS,
                graphics=(pymupdf.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED if remove_touching_graphics
                          else pymupdf.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED),
                text=pymupdf.PDF_REDACT_TEXT_REMOVE,
            )
        if clear_metadata:
            md = dict(doc.pdf.metadata or {})
            for k in ("title", "author", "subject", "keywords", "creator", "producer"):
                md[k] = ""
            doc.pdf.set_metadata(md)
            try:
                doc.pdf.del_xml_metadata()
            except Exception:
                pass
    return sum(len(v) for v in m.values())
