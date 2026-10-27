"""Document properties, bookmarks (outline) and links."""

from __future__ import annotations

from dataclasses import dataclass

import pymupdf

from .document import Document


@dataclass
class Bookmark:
    level: int
    title: str
    page: int  # 0-based, -1 if none
    y: float | None


def bookmarks(doc: Document) -> list[Bookmark]:
    out = []
    for item in doc.pdf.get_toc(simple=False):
        level, title, page = item[0], item[1], item[2]
        dest = item[3] if len(item) > 3 else {}
        y = None
        to = dest.get("to") if isinstance(dest, dict) else None
        if to is not None:
            y = to.y
        out.append(Bookmark(level, title, page - 1, y))
    return out


def set_bookmarks(doc: Document, items: list[Bookmark], label="Edit Bookmarks"):
    toc = []
    for b in items:
        entry = [b.level, b.title, b.page + 1]
        if b.y is not None:
            entry.append({"kind": pymupdf.LINK_GOTO, "to": pymupdf.Point(0, b.y), "page": b.page})
        toc.append(entry)
    # levels must start at 1 and never jump by more than one
    prev = 0
    for e in toc:
        e[0] = max(1, min(e[0], prev + 1))
        prev = e[0]
    with doc.edit(label, pages=[]):
        doc.pdf.set_toc(toc)


@dataclass
class Link:
    rect: tuple
    page: int | None   # internal target
    y: float | None
    uri: str | None


def links(doc: Document, page: int) -> list[Link]:
    out = []
    for lk in doc.pdf[page].get_links():
        kind = lk.get("kind")
        r = tuple(lk["from"])
        if kind == pymupdf.LINK_GOTO:
            to = lk.get("to")
            out.append(Link(r, lk.get("page"), to.y if to else None, None))
        elif kind == pymupdf.LINK_URI:
            out.append(Link(r, None, None, lk.get("uri")))
        elif kind == pymupdf.LINK_NAMED:
            target = lk.get("page")
            if target is not None and target >= 0:
                out.append(Link(r, target, None, None))
    return out


FIELDS = ["title", "author", "subject", "keywords", "creator", "producer"]


def properties(doc: Document) -> dict:
    md = dict(doc.pdf.metadata or {})
    md["pages"] = doc.page_count
    md["pdf_version"] = md.get("format", "")
    if doc.page_count:
        w, h = doc.page_size(0)
        md["page_size"] = f"{w / 72:.2f} x {h / 72:.2f} in"
    return md


def set_properties(doc: Document, values: dict):
    md = dict(doc.pdf.metadata or {})
    changed = False
    for k in FIELDS:
        if k in values and (md.get(k) or "") != values[k]:
            md[k] = values[k]
            changed = True
    if not changed:
        return
    with doc.edit("Document Properties", pages=[]):
        doc.pdf.set_metadata({k: md.get(k) or "" for k in FIELDS + ["creationDate", "modDate"]})
