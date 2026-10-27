"""One open document: the page view plus everything that edits the document."""

from __future__ import annotations

import logging
import os
import webbrowser

from PySide6.QtCore import QDate, QObject, QPointF, Qt, QTimer, Signal
from PySide6.QtGui import QColor, QFont, QFontMetricsF, QGuiApplication, QKeySequence
from PySide6.QtWidgets import (QCalendarWidget, QComboBox, QHBoxLayout, QLineEdit, QMessageBox,
                               QPlainTextEdit, QToolButton, QVBoxLayout, QWidget)

from ..core import annots as A
from ..core import forms as F
from ..core import redact as R
from ..core.document import Document
from . import icons, settings
from .pageview import PageView

log = logging.getLogger(__name__)


class ToolStyles(QObject):
    """Colour, line width and font size per tool, shared by all tabs."""

    changed = Signal()

    DEFAULTS = {
        "highlight": dict(color="#ffd400", width=1, size=12),
        "underline": dict(color="#1a8f2e", width=1, size=12),
        "strike": dict(color="#d32f2f", width=1, size=12),
        "note": dict(color="#ffc400", width=1, size=12),
        "textbox": dict(color="#d32f2f", width=1, size=12),
        "addtext": dict(color="#000000", width=1, size=11),
        "pen": dict(color="#1565c0", width=2, size=12),
        "rect": dict(color="#d32f2f", width=2, size=12),
        "oval": dict(color="#d32f2f", width=2, size=12),
        "arrow": dict(color="#d32f2f", width=2, size=12),
        "line": dict(color="#d32f2f", width=2, size=12),
    }

    def __init__(self):
        super().__init__()
        self.values = {k: dict(v) for k, v in self.DEFAULTS.items()}
        saved = settings.get("tool_styles", None)
        if isinstance(saved, dict):
            for k, v in saved.items():
                if k in self.values and isinstance(v, dict):
                    self.values[k].update({kk: vv for kk, vv in v.items() if kk in ("color", "width", "size")})

    def get(self, tool, key):
        return self.values.get(tool, {}).get(key, self.DEFAULTS.get("pen")[key])

    def set(self, tool, key, value):
        if tool not in self.values:
            return
        self.values[tool][key] = value
        settings.put("tool_styles", self.values)
        self.changed.emit()


def rgb(color: str | QColor) -> tuple:
    c = QColor(color)
    return (c.redF(), c.greenF(), c.blueF())


def hexcolor(t) -> str:
    if not t:
        return "#000000"
    return QColor.fromRgbF(*t[:3]).name()


ANNOT_TOOL = {A.HIGHLIGHT: "highlight", A.UNDERLINE: "underline", A.STRIKEOUT: "strike", A.NOTE: "note",
              A.FREETEXT: "textbox", A.INK: "pen", A.SQUARE: "rect", A.CIRCLE: "oval", A.LINE: "arrow"}


class InlineText(QPlainTextEdit):
    committed = Signal(str)
    cancelled = Signal()

    def __init__(self, text="", single_line=False, grow=None, parent=None):
        super().__init__(parent)
        self.single_line = single_line
        self.grow = grow
        self._done = False
        self.setPlainText(text)
        self.setFrameShape(QPlainTextEdit.Shape.NoFrame)
        self.setVerticalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff)
        self.setHorizontalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff)
        self.document().setDocumentMargin(1)
        if grow:
            self.setLineWrapMode(QPlainTextEdit.LineWrapMode.NoWrap)
            self.textChanged.connect(lambda: grow(self.toPlainText()))

    def keyPressEvent(self, ev):
        if ev.key() == Qt.Key.Key_Escape:
            self.cancel()
            return
        if ev.key() in (Qt.Key.Key_Return, Qt.Key.Key_Enter):
            if ev.modifiers() & Qt.KeyboardModifier.ControlModifier or self.single_line:
                self.commit()
                return
        super().keyPressEvent(ev)

    def focusOutEvent(self, ev):
        super().focusOutEvent(ev)
        if ev.reason() != Qt.FocusReason.PopupFocusReason:
            QTimer.singleShot(0, self.commit)

    def commit(self):
        if self._done:
            return
        self._done = True
        self.committed.emit(self.toPlainText())

    def cancel(self):
        if self._done:
            return
        self._done = True
        self.cancelled.emit()


class FieldLine(QLineEdit):
    committed = Signal(str, int)   # value, move (+1 next, -1 previous, 0 stay)
    cancelled = Signal()

    def __init__(self, text, parent=None):
        super().__init__(text, parent)
        self._done = False
        self.setFrame(False)

    def event(self, ev):
        if ev.type() == ev.Type.KeyPress and ev.key() in (Qt.Key.Key_Tab, Qt.Key.Key_Backtab):
            self.finish(-1 if ev.key() == Qt.Key.Key_Backtab or
                        ev.modifiers() & Qt.KeyboardModifier.ShiftModifier else 1)
            return True
        return super().event(ev)

    def keyPressEvent(self, ev):
        if ev.key() == Qt.Key.Key_Escape:
            self._done = True
            self.cancelled.emit()
            return
        if ev.key() in (Qt.Key.Key_Return, Qt.Key.Key_Enter):
            self.finish(0)
            return
        super().keyPressEvent(ev)

    def focusOutEvent(self, ev):
        super().focusOutEvent(ev)
        if ev.reason() not in (Qt.FocusReason.PopupFocusReason, Qt.FocusReason.ActiveWindowFocusReason):
            QTimer.singleShot(0, lambda: self.finish(0))

    def finish(self, move):
        if self._done:
            return
        self._done = True
        self.committed.emit(self.text(), move)


class FieldMulti(InlineText):
    def event(self, ev):
        if ev.type() == ev.Type.KeyPress and ev.key() in (Qt.Key.Key_Tab, Qt.Key.Key_Backtab):
            self.move = -1 if ev.key() == Qt.Key.Key_Backtab or \
                ev.modifiers() & Qt.KeyboardModifier.ShiftModifier else 1
            self.commit()
            return True
        return super().event(ev)


class DocumentTab(QWidget):
    changed = Signal()                # dirty / undo state / content
    title_changed = Signal()
    message = Signal(str)
    tool_changed = Signal(str)
    selection_changed = Signal()
    annot_selected = Signal(object)
    search_update = Signal()

    def __init__(self, doc: Document, renderer, styles: ToolStyles, window, parent=None):
        super().__init__(parent)
        self.doc = doc
        self.window_ = window
        self.styles = styles
        self.renderer = renderer
        renderer.set_document(id(self), doc.version, doc.tobytes)
        self.view = PageView(self, renderer, self)
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.setSpacing(0)
        lay.addWidget(self.view)
        self.view.tool_changed.connect(self.tool_changed)
        self.view.selection_changed.connect(self.selection_changed)
        self.view.annot_selected.connect(self.annot_selected)
        doc.listeners.append(self._doc_changed)
        self._editor = None
        self._editor_field = None
        # search
        self.search_query = ""
        self.search_case = False
        self.search_req = None
        self.search_done = True
        self.search_progress = (0, 0)
        self._hit_order: list[tuple[int, int]] = []
        self.view.highlight_fields = bool(settings.get("highlight_fields", True, bool))

    # ----- plumbing --------------------------------------------------------------------

    def _doc_changed(self, _doc):
        self.view.document_changed()
        if self.search_query:
            # positions may have moved: re-run the search quietly
            QTimer.singleShot(0, lambda: self.find(self.search_query, self.search_case, keep_index=True))
        self.changed.emit()

    def close_document(self):
        self.finish_editing()
        self.renderer.side.cancel_search()
        self.renderer.drop_document(id(self))
        self.doc.close()

    def run(self, fn, *args, **kw):
        """Run an editing call, reporting errors instead of crashing."""
        try:
            return fn(*args, **kw)
        except Exception as exc:
            log.exception("edit failed: %s", getattr(fn, "__name__", fn))
            QMessageBox.warning(self, "Docket", f"That didn't work:\n\n{exc}")
            return None

    def author(self) -> str:
        return settings.author()

    def tool_color(self, tool=None):
        return self.styles.get(tool or self.view.tool, "color")

    def tool_width(self, tool=None):
        return float(self.styles.get(tool or self.view.tool, "width"))

    def tool_size(self, tool=None):
        return float(self.styles.get(tool or self.view.tool, "size"))

    def set_tool(self, tool):
        self.finish_editing()
        self.view.set_tool(tool)

    # ----- creation (called by the page view) --------------------------------------------

    def create_markup(self, page, kind, rects):
        tool = {A.HIGHLIGHT: "highlight", A.UNDERLINE: "underline", A.STRIKEOUT: "strike"}[kind]
        self.run(A.add_markup, self.doc, page, kind, rects, rgb(self.tool_color(tool)), self.author())

    def markup_selection(self, kind) -> bool:
        i, rects = self.view.selection_rects()
        if not rects:
            return False
        self.create_markup(i, kind, rects)
        self.view.clear_selection()
        return True

    def redact_selection(self) -> bool:
        i, rects = self.view.selection_rects()
        if not rects:
            return False
        self.create_redaction(i, rects)
        self.view.clear_selection()
        return True

    def create_redaction(self, page, rects):
        self.run(R.add_marks, self.doc, page, rects)

    def create_ink(self, page, points):
        self.run(A.add_ink, self.doc, page, [points], rgb(self.tool_color("pen")), self.tool_width("pen"),
                 self.author())

    def create_shape(self, page, kind, rect):
        tool = "rect" if kind == A.SQUARE else "oval"
        x = self.run(A.add_shape, self.doc, page, kind, rect, rgb(self.tool_color(tool)), self.tool_width(tool),
                     self.author())
        self._select_xref(page, x)
        self.set_tool("select")

    def create_arrow(self, page, p1, p2, arrow):
        tool = "arrow" if arrow else "line"
        x = self.run(A.add_arrow, self.doc, page, p1, p2, rgb(self.tool_color(tool)), self.tool_width(tool),
                     arrow, self.author())
        self._select_xref(page, x)
        self.set_tool("select")

    def create_signature(self, page, rect):
        png = self.view.pending_signature
        if not png:
            return
        x = self.run(A.add_signature, self.doc, page, rect, png, self.author())
        self.set_tool("select")
        self._select_xref(page, x)

    def create_stamp(self, page, rect):
        x = self.run(A.add_stamp, self.doc, page, rect, self.view.pending_stamp, self.author())
        self.set_tool("select")
        self._select_xref(page, x)

    def _select_xref(self, page, xref):
        if not xref:
            return
        for a in self.view.annots(page):
            if a.xref == xref:
                self.view.select_annot(a)
                return

    def create_note(self, page, pt):
        x, y = pt
        rect = (x + 22, y, x + 222, y + 110)
        ed = InlineText("")
        ed.setStyleSheet("background:#fff8c4;color:#000;border:1px solid #c9a800;")

        def done(text):
            self._close_editor()
            if text.strip():
                xr = self.run(A.add_note, self.doc, page, (x, y), text, rgb(self.tool_color("note")), self.author())
                self.set_tool("select")
                self._select_xref(page, xr)

        ed.committed.connect(done)
        ed.cancelled.connect(self._close_editor)
        self._open_editor(ed, page, rect, None)
        self.message.emit("Type your note. Ctrl+Enter or click elsewhere to finish, Esc to cancel.")

    def create_textbox(self, page, rect):
        size = self.tool_size("textbox")
        ed = InlineText("")
        col = self.tool_color("textbox")
        ed.setStyleSheet(f"background:white;color:black;border:1px solid {col};")

        def done(text):
            self._close_editor()
            if text.strip():
                xr = self.run(A.add_textbox, self.doc, page, rect, text, size, rgb(col), True, self.author())
                self._select_xref(page, xr)
            self.set_tool("select")

        ed.committed.connect(done)
        ed.cancelled.connect(lambda: (self._close_editor(), self.set_tool("select")))
        self._open_editor(ed, page, rect, size)
        self.message.emit("Type the text. Ctrl+Enter or click elsewhere to finish.")

    def _text_extent(self, text, size) -> tuple[float, float]:
        f = QFont("Helvetica")
        f.setPixelSize(100)
        fm = QFontMetricsF(f)
        lines = text.split("\n") or [""]
        w = max(fm.horizontalAdvance(l) for l in lines) * size / 100
        h = fm.height() * size / 100 * len(lines)
        return w + size * 0.6 + 4, h + size * 0.35 + 3

    def create_plain_text(self, page, pt, existing=None):
        size = self.tool_size("addtext") if existing is None else (existing.fontsize or 11)
        col = self.tool_color("addtext") if existing is None else hexcolor(existing.color)
        x, y = pt
        start_text = existing.content if existing else ""
        w, h = self._text_extent(start_text or "M", size)
        rect = [x, y - size * 0.25, x + max(w, size * 2), y - size * 0.25 + h]
        ed = None

        def grow(text):
            ww, hh = self._text_extent(text or "M", size)
            rect[2] = rect[0] + max(ww, size * 2) + size
            rect[3] = rect[1] + hh
            self.view.update_anchor_rect(ed, tuple(rect))

        ed = InlineText(start_text, grow=grow)
        ed.setStyleSheet(f"background:rgba(255,255,255,200);color:{col};border:1px dashed #1473e6;")

        def done(text):
            self._close_editor()
            if existing is not None:
                if text != existing.content:
                    if text.strip():
                        ww, hh = self._text_extent(text, size)
                        r = existing.rect
                        self.run(A.set_text, self.doc, page, existing.xref, text,
                                 rect=(r[0], r[1], r[0] + ww, r[1] + hh))
                    else:
                        self.run(A.delete, self.doc, page, [existing.xref])
                return
            if text.strip():
                ww, hh = self._text_extent(text, size)
                r = (rect[0], rect[1], rect[0] + ww, rect[1] + hh)
                self.run(A.add_textbox, self.doc, page, r, text, size, rgb(col), False, self.author())

        ed.committed.connect(done)
        ed.cancelled.connect(self._close_editor)
        self._open_editor(ed, page, tuple(rect), size)
        grow(start_text)
        self.message.emit("Type the text. Ctrl+Enter or click elsewhere to finish. The Add Text tool stays on until Esc.")

    def edit_annot_text(self, a):
        if a.type == A.FREETEXT:
            if a.is_plain_text:
                self.create_plain_text(a.page, (a.rect[0], a.rect[1] + (a.fontsize or 11) * 0.25), existing=a)
                return
            ed = InlineText(a.content)
            ed.setStyleSheet("background:white;color:black;border:1px solid #1473e6;")

            def done(text):
                self._close_editor()
                if text != a.content:
                    self.run(A.set_text, self.doc, a.page, a.xref, text)

            ed.committed.connect(done)
            ed.cancelled.connect(self._close_editor)
            self._open_editor(ed, a.page, a.rect, a.fontsize or 12)
        elif a.type in (A.NOTE,) or a.type in A.MARKUP or a.type in (A.SQUARE, A.CIRCLE, A.INK, A.LINE):
            x, y = a.rect[2], a.rect[1]
            ed = InlineText(a.content)
            ed.setStyleSheet("background:#fff8c4;color:#000;border:1px solid #c9a800;")
            ed.setPlaceholderText("Add a comment")

            def done(text):
                self._close_editor()
                if text != a.content:
                    self.run(A.set_text, self.doc, a.page, a.xref, text)

            ed.committed.connect(done)
            ed.cancelled.connect(self._close_editor)
            self._open_editor(ed, a.page, (x + 4, y, x + 204, y + 110), None)

    def _open_editor(self, ed, page, rect, font_pt):
        self.finish_editing()
        self._editor = ed
        self.view.anchor_widget(ed, page, rect, font_pt)
        ed.setFocus()

    def _close_editor(self):
        ed = self._editor
        self._editor = None
        self._editor_field = None
        if ed is not None:
            self.view.unanchor(ed)
            ed.hide()
            ed.deleteLater()
            self.view.setFocus()

    def finish_editing(self):
        ed = self._editor
        if ed is None:
            return
        if isinstance(ed, InlineText):
            ed.commit()
        elif isinstance(ed, FieldLine):
            ed.finish(0)
        elif isinstance(ed, QWidget) and hasattr(ed, "finish"):
            ed.finish(0)
        else:
            self._close_editor()

    # ----- annotations editing ---------------------------------------------------------------

    def move_annot(self, a, dx, dy):
        self.run(A.move, self.doc, a.page, a.xref, dx, dy)

    def resize_annot(self, a, rect):
        self.run(A.resize, self.doc, a.page, a.xref, rect)

    def delete_annot(self, a):
        self.view.select_annot(None)
        self.run(A.delete, self.doc, a.page, [a.xref])

    def style_annot(self, a, color=None, width=None, fontsize=None):
        self.run(A.set_style, self.doc, a.page, a.xref,
                 color=rgb(color) if color is not None else None, width=width, fontsize=fontsize)

    # ----- forms ------------------------------------------------------------------------------------

    def field_clicked(self, f):
        self.finish_editing()
        if f.kind in (F.CHECKBOX, F.RADIO):
            self.run(F.toggle, self.doc, f.page, f.xref)
            return
        if f.kind in (F.COMBO, F.LIST):
            self._choice_editor(f)
            return
        if f.kind == F.TEXT:
            self._text_field_editor(f)
            return
        if f.kind == F.SIGNATURE:
            self.message.emit("This is a digital signature field. Use Fill & Sign to place a signature.")

    def _font_for(self, f) -> float:
        if f.fontsize:
            return f.fontsize
        h = f.rect[3] - f.rect[1]
        return max(6, min(12, h * 0.65)) if not f.multiline else 10

    def _text_field_editor(self, f):
        size = self._font_for(f)
        if f.multiline:
            ed = FieldMulti(f.value)
            ed.move = 0
            ed.setStyleSheet("background:#eef3ff;color:black;border:1px solid #1473e6;")
            ed.committed.connect(lambda text: self._commit_field(f, text, getattr(ed, "move", 0)))
            ed.cancelled.connect(self._close_editor)
            self._open_editor(ed, f.page, f.rect, size)
            self._editor_field = f
            return
        if f.is_date:
            box = DateFieldEditor(f.value)
            box.committed.connect(lambda text, move: self._commit_field(f, text, move))
            box.cancelled.connect(self._close_editor)
            rect = (f.rect[0], f.rect[1], max(f.rect[2], f.rect[0] + 90), f.rect[3])
            self._open_editor(box, f.page, rect, size)
            box.line.setFocus()
            box.line.selectAll()
            self._editor_field = f
            self.message.emit("Type a date like 3/5/26 or use the calendar. Saved as MM/DD/YYYY.")
            return
        ed = FieldLine(f.value)
        ed.setStyleSheet("background:#eef3ff;color:black;border:1px solid #1473e6;")
        ed.committed.connect(lambda text, move: self._commit_field(f, text, move))
        ed.cancelled.connect(self._close_editor)
        self._open_editor(ed, f.page, f.rect, size)
        ed.selectAll()
        self._editor_field = f

    def _commit_field(self, f, text, move=0):
        if f.is_date and text.strip():
            d = F.parse_date(text)
            if d is None:
                self.message.emit(f"“{text}” isn't a date. Use MM/DD/YYYY.")
                ed = self._editor
                if isinstance(ed, DateFieldEditor):
                    ed.reopen()
                elif isinstance(ed, FieldLine):
                    ed._done = False
                return
            text = F.format_date(d)
        self._close_editor()
        if text != f.value:
            self.run(F.set_value, self.doc, f.page, f.xref, text)
        if move:
            self._tab_to(f, move)

    def _tab_to(self, f, move):
        order = []
        for i in range(self.doc.page_count):
            for g in self.view.fields(i):
                if not g.readonly and g.kind in (F.TEXT, F.COMBO, F.LIST, F.CHECKBOX):
                    order.append(g)
        keys = [(g.page, g.xref) for g in order]
        try:
            k = keys.index((f.page, f.xref))
        except ValueError:
            return
        nxt = order[(k + move) % len(order)]
        self.view.ensure_visible(nxt.page, nxt.rect)
        if nxt.kind == F.TEXT:
            self._text_field_editor(nxt)
        else:
            self.view.ensure_visible(nxt.page, nxt.rect)

    def _choice_editor(self, f):
        cb = QComboBox()
        vals = [c if isinstance(c, str) else c[1] if isinstance(c, (list, tuple)) else str(c) for c in f.choices]
        cb.addItems(vals)
        if f.value in vals:
            cb.setCurrentIndex(vals.index(f.value))

        def chosen(idx):
            val = cb.itemText(idx)
            self._close_editor()
            if val != f.value:
                self.run(F.set_value, self.doc, f.page, f.xref, val)

        cb.activated.connect(chosen)
        self._open_editor(cb, f.page, f.rect, self._font_for(f))
        QTimer.singleShot(0, cb.showPopup)

    def create_field(self, page, rect, kind):
        self.window_.prompt_new_field(self, page, rect, kind)

    def suggestions_changed(self):
        self.window_.suggestions_changed(self)

    # ----- links ---------------------------------------------------------------------------------------

    def follow_link(self, lk):
        if lk.uri:
            if lk.uri.lower().startswith(("http://", "https://", "mailto:")):
                webbrowser.open(lk.uri)
            else:
                self.message.emit(f"Link: {lk.uri}")
        elif lk.page is not None:
            self.view.go_to_page(lk.page, lk.y)

    # ----- search -------------------------------------------------------------------------------------

    def find(self, query, match_case=False, keep_index=False):
        prev = self.view.current_hit if keep_index else None
        self.renderer.side.cancel_search()
        self.search_query = query
        self.search_case = match_case
        self.view.hits = {}
        self.view.current_hit = None
        self._hit_order = []
        self._keep_hit = prev
        if not query.strip():
            self.search_done = True
            self.view.viewport().update()
            self.search_update.emit()
            return
        side = self.renderer.side
        if not getattr(self, "_search_connected", False):
            # results for other tabs are filtered out by request id
            side.search_hits.connect(self._on_hits)
            side.search_done.connect(self._on_search_done)
            side.search_progress.connect(self._on_search_progress)
            self._search_connected = True
        self.renderer.set_document(id(self), self.doc.version, self.doc.tobytes)
        self.search_done = False
        self.search_req = side.start_search(id(self), self.doc.version, query, match_case, self.view.current_page)
        self.search_update.emit()

    def _on_hits(self, req, page, hits):
        if req != self.search_req:
            return
        self.view.hits[page] = hits
        self._hit_order = sorted((p, k) for p, hs in self.view.hits.items() for k in range(len(hs)))
        if self.view.current_hit is None:
            keep = getattr(self, "_keep_hit", None)
            if keep and keep[0] == page and keep[1] < len(hits):
                self.view.current_hit = keep
            elif not keep:
                self.view.current_hit = (page, 0)
                self.view.ensure_visible(page, hits[0][0])
        self.view.viewport().update()
        self.search_update.emit()

    def _on_search_progress(self, req, done, total):
        if req == self.search_req:
            self.search_progress = (done, total)
            self.search_update.emit()

    def _on_search_done(self, req, complete):
        if req != self.search_req:
            return
        self.search_done = True
        if self.view.current_hit is None and self._hit_order:
            self.view.current_hit = self._hit_order[0]
        self.search_update.emit()

    def hit_count(self) -> int:
        return len(self._hit_order)

    def hit_index(self) -> int:
        try:
            return self._hit_order.index(self.view.current_hit)
        except ValueError:
            return -1

    def next_hit(self, step=1):
        if not self._hit_order:
            return
        k = self.hit_index()
        k = 0 if k < 0 and step > 0 else (k + step) % len(self._hit_order)
        self.view.current_hit = self._hit_order[k]
        p, n = self.view.current_hit
        self.view.ensure_visible(p, self.view.hits[p][n][0])
        self.view.viewport().update()
        self.search_update.emit()

    def clear_search(self):
        self.renderer.side.cancel_search()
        self.search_query = ""
        self.view.hits = {}
        self.view.current_hit = None
        self._hit_order = []
        self.view.viewport().update()
        self.search_update.emit()

    # ----- clipboard -----------------------------------------------------------------------------------

    def copy(self) -> bool:
        t = self.view.selected_text()
        if t:
            QGuiApplication.clipboard().setText(t)
            return True
        return False


class DateFieldEditor(QWidget):
    committed = Signal(str, int)
    cancelled = Signal()

    def __init__(self, value, parent=None):
        super().__init__(parent)
        lay = QHBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.setSpacing(0)
        self.line = FieldLine(value)
        self.line.setStyleSheet("background:#eef3ff;color:black;border:1px solid #1473e6;")
        self.btn = QToolButton()
        self.btn.setIcon(icons.icon("date", 14))
        self.btn.setStyleSheet("background:#eef3ff;border:1px solid #1473e6;padding:0;")
        self.btn.setFocusPolicy(Qt.FocusPolicy.NoFocus)
        lay.addWidget(self.line, 1)
        lay.addWidget(self.btn)
        self.line.committed.connect(self.committed)
        self.line.cancelled.connect(self.cancelled)
        self.btn.clicked.connect(self._calendar)
        self._cal = None

    def resizeEvent(self, ev):
        super().resizeEvent(ev)
        self.btn.setFixedWidth(max(16, self.height()))

    def finish(self, move):
        self.line.finish(move)

    def reopen(self):
        self.line._done = False
        self.line.setFocus()
        self.line.selectAll()

    def _calendar(self):
        self.line._done = True  # don't commit on focus loss while the popup is open
        cal = QCalendarWidget()
        cal.setWindowFlags(Qt.WindowType.Popup)
        d = F.parse_date(self.line.text())
        if d:
            cal.setSelectedDate(QDate(d.year, d.month, d.day))
        cal.move(self.mapToGlobal(self.rect().bottomLeft()))

        def picked(qd):
            self.line.setText(qd.toString("MM/dd/yyyy"))
            cal.close()
            self.line._done = False
            self.line.finish(0)

        cal.clicked.connect(picked)
        self._cal = cal
        cal.installEventFilter(self)
        cal.show()
        cal.setFocus()

    def eventFilter(self, obj, ev):
        if obj is self._cal and ev.type() == ev.Type.Hide:
            if self.line._done and self._cal is not None:
                self._cal = None
                self.line._done = False
                self.line.setFocus()
        return False
