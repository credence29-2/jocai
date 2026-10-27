"""AcroForm fields: read, fill, create, rename, delete, flatten."""

from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import date

import pymupdf

from .document import Document

TEXT = pymupdf.PDF_WIDGET_TYPE_TEXT
CHECKBOX = pymupdf.PDF_WIDGET_TYPE_CHECKBOX
RADIO = pymupdf.PDF_WIDGET_TYPE_RADIOBUTTON
COMBO = pymupdf.PDF_WIDGET_TYPE_COMBOBOX
LIST = pymupdf.PDF_WIDGET_TYPE_LISTBOX
BUTTON = pymupdf.PDF_WIDGET_TYPE_BUTTON
SIGNATURE = pymupdf.PDF_WIDGET_TYPE_SIGNATURE

DATE_MARK = "DocketDate"  # private key on the field dict marking our date fields


@dataclass
class Field:
    page: int
    xref: int
    name: str
    kind: int
    rect: tuple[float, float, float, float]
    value: str
    choices: list
    multiline: bool
    readonly: bool
    is_date: bool
    fontsize: float
    on_state: str | None
    tab_order: int = 0


def _is_date(doc: pymupdf.Document, w: pymupdf.Widget) -> bool:
    try:
        if doc.xref_get_key(w.xref, DATE_MARK)[1] == "true":
            return True
    except Exception:
        pass
    js = (w.script_format or "") + (w.script_stroke or "")
    return "AFDate_" in js


def fields(doc: Document, page: int) -> list[Field]:
    out = []
    p = doc.pdf[page]
    for n, w in enumerate(p.widgets() or []):
        if w.field_type == BUTTON:
            continue
        flags = w.field_flags or 0
        val = w.field_value
        if isinstance(val, bool):
            val = "Yes" if val else "Off"
        out.append(Field(
            page=page,
            xref=w.xref,
            name=w.field_name or "",
            kind=w.field_type,
            rect=tuple(w.rect),
            value="" if val is None else str(val),
            choices=list(w.choice_values or []),
            multiline=bool(flags & pymupdf.PDF_TX_FIELD_IS_MULTILINE),
            readonly=bool(flags & pymupdf.PDF_FIELD_IS_READ_ONLY),
            is_date=w.field_type == TEXT and _is_date(doc.pdf, w),
            fontsize=w.text_fontsize or 0,
            on_state=_on_state(w),
            tab_order=n,
        ))
    return out


def _on_state(w: pymupdf.Widget) -> str | None:
    if w.field_type in (CHECKBOX, RADIO):
        try:
            return w.on_state()
        except Exception:
            return "Yes"
    return None


def all_fields(doc: Document) -> list[Field]:
    out = []
    for i in range(doc.page_count):
        out.extend(fields(doc, i))
    return out


def has_fields(doc: Document) -> bool:
    return any(doc.pdf[i].first_widget for i in range(doc.page_count))


def _widget(doc: Document, page: int, xref: int) -> tuple[pymupdf.Page, pymupdf.Widget]:
    """The page is returned too: a widget stops working once its page object is freed."""
    p = doc.pdf[page]
    for w in p.widgets():
        if w.xref == xref:
            return p, w
    raise KeyError(xref)


def set_value(doc: Document, page: int, xref: int, value) -> None:
    _p, w = _widget(doc, page, xref)
    if w.field_type == CHECKBOX:
        new = w.on_state() if value in (True, "Yes", "On", w.on_state()) else "Off"
    elif w.field_type == RADIO:
        new = True if value else False
    else:
        new = "" if value is None else str(value)
    current = w.field_value
    if current == new or (isinstance(current, bool) and w.field_type == RADIO and current == new):
        return
    with doc.edit("Fill Field", pages=[page]):
        p, w = _widget(doc, page, xref)
        w.field_value = new
        w.update()
        if w.field_type == RADIO and new:
            # turn the rest of the group off
            for other in p.widgets():
                if other.field_type == RADIO and other.field_name == w.field_name and other.xref != xref:
                    other.field_value = False
                    other.update()


def toggle(doc: Document, page: int, xref: int) -> None:
    _p, w = _widget(doc, page, xref)
    if w.field_type == CHECKBOX:
        on = w.field_value not in (False, "Off", "", None)
        set_value(doc, page, xref, "Off" if on else "Yes")
    elif w.field_type == RADIO:
        set_value(doc, page, xref, True)


# ----- dates ---------------------------------------------------------------------

_DATE_PATTERNS = [
    (r"(\d{1,2})[/.-](\d{1,2})[/.-](\d{4})$", "mdy"),
    (r"(\d{1,2})[/.-](\d{1,2})[/.-](\d{2})$", "mdy2"),
    (r"(\d{4})[/.-](\d{1,2})[/.-](\d{1,2})$", "ymd"),
]


def parse_date(text: str) -> date | None:
    text = text.strip()
    for pat, kind in _DATE_PATTERNS:
        m = re.match(pat, text)
        if not m:
            continue
        a, b, c = (int(g) for g in m.groups())
        try:
            if kind == "mdy":
                return date(c, a, b)
            if kind == "mdy2":
                return date(2000 + c if c < 70 else 1900 + c, a, b)
            return date(a, b, c)
        except ValueError:
            return None
    return None


def format_date(d: date) -> str:
    return d.strftime("%m/%d/%Y")


# ----- creating fields -------------------------------------------------------------


def _unique_name(doc: Document, base: str) -> str:
    base = re.sub(r"[^A-Za-z0-9_]+", "_", base).strip("_") or "Field"
    names = {f.name for f in all_fields(doc)}
    if base not in names:
        return base
    n = 2
    while f"{base}_{n}" in names:
        n += 1
    return f"{base}_{n}"


def add_field(doc: Document, page: int, rect, kind: str, name: str, fontsize: float = 0,
              multiline: bool = False, label: str = "Add Field") -> int:
    """kind: 'text', 'checkbox', 'date'. fontsize 0 = auto. Returns the widget xref."""
    with doc.edit(label, pages=[page]):
        xref = _add_widget(doc, page, rect, kind, name, fontsize, multiline)
    return xref


def _add_widget(doc, page, rect, kind, name, fontsize=0, multiline=False) -> int:
    p = doc.pdf[page]
    w = pymupdf.Widget()
    w.rect = pymupdf.Rect(rect)
    w.field_name = _unique_name(doc, name)
    w.border_width = 0
    w.fill_color = None
    w.text_font = "Helv"
    w.text_fontsize = fontsize
    if kind == "checkbox":
        w.field_type = CHECKBOX
        w.field_value = "Off"
        w.border_color = (0, 0, 0)
        w.border_width = 1
    else:
        w.field_type = TEXT
        w.field_value = ""
        if multiline:
            w.field_flags = pymupdf.PDF_TX_FIELD_IS_MULTILINE
        if kind == "date":
            w.script_format = 'AFDate_FormatEx("mm/dd/yyyy");'
            w.script_stroke = 'AFDate_KeystrokeEx("mm/dd/yyyy");'
    annot = p.add_widget(w)
    if kind == "date":
        doc.pdf.xref_set_key(annot.xref, DATE_MARK, "true")
    return annot.xref


def add_fields_bulk(doc: Document, specs: list[dict]) -> int:
    """specs: {page, rect, kind, name}. One undo step."""
    if not specs:
        return 0
    pages = sorted({s["page"] for s in specs})
    with doc.edit(f"Create {len(specs)} Fields", pages=pages):
        for s in specs:
            _add_widget(doc, s["page"], s["rect"], s["kind"], s["name"], s.get("fontsize", 0))
    return len(specs)


def update_field(doc: Document, page: int, xref: int, name: str | None = None,
                 fontsize: float | None = None):
    with doc.edit("Field Properties", pages=[page]):
        _p, w = _widget(doc, page, xref)
        pdf = doc.pdf
        # the name and default appearance live on the field dict, which may be the
        # widget's parent; Widget.update() doesn't write them there
        field = xref
        parent = pdf.xref_get_key(xref, "Parent")
        if parent[0] == "xref" and pdf.xref_get_key(xref, "T")[0] == "null":
            field = int(parent[1].split()[0])
        if name and name != w.field_name:
            pdf.xref_set_key(field, "T", pymupdf.get_pdf_str(_unique_name(doc, name)))
        if fontsize is not None:
            for target in {field, xref}:
                kind, da = pdf.xref_get_key(target, "DA")
                if kind == "string":
                    da = re.sub(r"[\d.]+\s+Tf", f"{fontsize:g} Tf", da)
                    pdf.xref_set_key(target, "DA", pymupdf.get_pdf_str(da))
        _p, w = _widget(doc, page, xref)
        w.update()


def delete_field(doc: Document, page: int, xref: int):
    with doc.edit("Delete Field", pages=[page]):
        p, w = _widget(doc, page, xref)
        p.delete_widget(w)


def flatten(doc: Document):
    with doc.edit("Flatten Form"):
        doc.pdf.bake(annots=False, widgets=True)
