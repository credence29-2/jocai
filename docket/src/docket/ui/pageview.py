"""Continuous-scroll page view with tiles, overlays and the mouse tools."""

from __future__ import annotations

import bisect
import math
import time

from PySide6.QtCore import QPointF, QRect, QRectF, QSize, Qt, QTimer, Signal
from PySide6.QtGui import (QColor, QCursor, QGuiApplication, QKeySequence, QPainter, QPainterPath,
                           QPen, QPolygonF)
from PySide6.QtWidgets import QAbstractScrollArea, QWidget

from ..core import annots as A
from ..core import forms as F
from ..core import meta as M
from ..core import text as T
from . import theme
from .renderer import Renderer, Request

PT_TO_PX = 96 / 72        # 100% zoom = actual size on a 96 dpi screen
GAP = 10
MARGIN = 14
TILE = 512                # device pixels
MIN_ZOOM, MAX_ZOOM = 0.08, 64.0
ZOOM_STEPS = [0.0833, 0.125, 0.25, 0.3333, 0.5, 0.6667, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0, 4.0,
              6.0, 8.0, 12.0, 16.0, 24.0, 32.0, 64.0]
HANDLE = 4

MARKUP_TOOLS = {"highlight": A.HIGHLIGHT, "underline": A.UNDERLINE, "strike": A.STRIKEOUT}
DRAG_TOOLS = {"textbox", "rect", "oval", "arrow", "line", "field_text", "field_check",
              "field_date", "sign", "stamp"}


def xform(m, x, y):
    a, b, c, d, e, f = m
    return a * x + c * y + e, b * x + d * y + f


class PageView(QAbstractScrollArea):
    page_changed = Signal(int)
    zoom_changed = Signal(float)
    selection_changed = Signal()
    annot_selected = Signal(object)   # AnnotInfo or None
    tool_changed = Signal(str)
    context_menu = Signal(object, object)  # global pos, dict(context)

    def __init__(self, ctl, renderer: Renderer, parent=None):
        super().__init__(parent)
        self.ctl = ctl                  # the DocumentTab
        self.doc = ctl.doc
        self.renderer = renderer
        self.doc_key = id(ctl)
        self.zoom = 1.0
        self.fit_mode = "width"
        self.tool = "select"
        self._geo: dict[int, tuple] = {}        # page id -> geometry
        self._tops: list[float] = []
        self._rects: list[QRectF] = []
        self._content = QSize(0, 0)
        self._text: dict[int, T.PageText] = {}
        self._fields: dict[int, list] = {}
        self._annots: dict[int, list] = {}
        self._links: dict[int, list] = {}
        self._last_preview: dict[int, object] = {}
        self.current_page = 0
        # text selection: (page, start, end)
        self.sel: tuple[int, int, int] | None = None
        self._sel_anchor = None
        # selected annotation
        self.sel_annot: A.AnnotInfo | None = None
        # search
        self.hits: dict[int, list] = {}
        self.current_hit: tuple[int, int] | None = None
        self.highlight_fields = True
        self.suggestions: list[dict] | None = None
        # pending items for placing tools
        self.pending_signature: bytes | None = None
        self.pending_signature_size: tuple[float, float] = (150, 50)
        self.pending_stamp: int = 0
        self._drag = None
        self._space_pan = False
        self._click_times: list[float] = []
        self._anchored: list[tuple[QWidget, int, tuple]] = []
        self.viewport().setMouseTracking(True)
        self.setMouseTracking(True)
        self.setFocusPolicy(Qt.FocusPolicy.StrongFocus)
        self.viewport().setAutoFillBackground(False)
        self.setFrameShape(QAbstractScrollArea.Shape.NoFrame)
        self.verticalScrollBar().setSingleStep(40)
        self.horizontalScrollBar().setSingleStep(40)
        renderer.image_ready.connect(self._image_ready)
        self._req_timer = QTimer(self)
        self._req_timer.setSingleShot(True)
        self._req_timer.setInterval(0)
        self._req_timer.timeout.connect(self._request_visible)
        self.relayout()

    # ===== geometry ======================================================================

    def scale(self) -> float:
        return self.zoom * PT_TO_PX

    def geo(self, i: int):
        pid = self.doc.page_ids[i]
        g = self._geo.get(pid)
        if g is None:
            g = self.doc.geometry(i)
            self._geo[pid] = g
        return g

    def relayout(self, keep_anchor=True):
        anchor = self._anchor() if keep_anchor and self._rects else None
        n = self.doc.page_count
        if self.fit_mode:
            self.zoom = self._fit_zoom()
        s = self.scale()
        maxw = 0.0
        tops, rects = [], []
        y = MARGIN
        for i in range(n):
            w, h = self.geo(i)[:2]
            pw, ph = w * s, h * s
            maxw = max(maxw, pw)
            rects.append(QRectF(0, y, pw, ph))
            tops.append(y)
            y += ph + GAP
        total_w = maxw + 2 * MARGIN
        vw = self.viewport().width()
        cw = max(total_w, vw)
        for r in rects:
            r.moveLeft((cw - r.width()) / 2)
        self._rects = rects
        self._tops = tops
        self._content = QSize(int(math.ceil(cw)), int(math.ceil(y - GAP + MARGIN)))
        self._update_scrollbars()
        if anchor:
            self._restore_anchor(anchor)
        self._reposition_anchored()
        self.viewport().update()
        self.zoom_changed.emit(self.zoom)

    def _fit_zoom(self) -> float:
        if not self.doc.page_count:
            return 1.0
        i = min(self.current_page, self.doc.page_count - 1)
        w, h = self.geo(i)[:2]
        vw = max(50, self.viewport().width() - 2 * MARGIN - (0 if self.verticalScrollBar().isVisible() else 14))
        vh = max(50, self.viewport().height() - 2 * MARGIN)
        if self.fit_mode == "width":
            maxw = max(self.geo(k)[0] for k in range(min(self.doc.page_count, 50)))
            return max(MIN_ZOOM, min(MAX_ZOOM, vw / (maxw * PT_TO_PX)))
        return max(MIN_ZOOM, min(MAX_ZOOM, min(vw / (w * PT_TO_PX), vh / (h * PT_TO_PX))))

    def _update_scrollbars(self):
        vs, hs = self.verticalScrollBar(), self.horizontalScrollBar()
        vp = self.viewport().size()
        vs.setPageStep(vp.height())
        hs.setPageStep(vp.width())
        vs.setRange(0, max(0, self._content.height() - vp.height()))
        hs.setRange(0, max(0, self._content.width() - vp.width()))

    def _offset(self) -> QPointF:
        return QPointF(self.horizontalScrollBar().value(), self.verticalScrollBar().value())

    def _anchor(self):
        """(page, fraction down the page, x fraction) at the viewport top."""
        if not self._rects:
            return None
        off = self._offset()
        i = self.page_at_y(off.y() + 1)
        r = self._rects[i]
        return i, (off.y() - r.top()) / max(1, r.height()), off.x() / max(1, self._content.width())

    def _restore_anchor(self, a):
        i, fy, fx = a
        i = min(i, len(self._rects) - 1)
        if i < 0:
            return
        r = self._rects[i]
        self.verticalScrollBar().setValue(int(r.top() + fy * r.height()))
        self.horizontalScrollBar().setValue(int(fx * self._content.width()))

    def page_at_y(self, y: float) -> int:
        if not self._tops:
            return 0
        i = bisect.bisect_right(self._tops, y) - 1
        return max(0, min(i, len(self._tops) - 1))

    def visible_pages(self) -> list[int]:
        if not self._rects:
            return []
        off = self._offset()
        top, bottom = off.y(), off.y() + self.viewport().height()
        i = self.page_at_y(top)
        out = []
        while i < len(self._rects) and self._rects[i].top() < bottom:
            if self._rects[i].bottom() >= top:
                out.append(i)
            i += 1
        return out

    def page_rect_view(self, i: int) -> QRectF:
        return self._rects[i].translated(-self._offset())

    def to_page(self, pos: QPointF, clamp_page: int | None = None):
        """View point -> (page index, (x, y) in unrotated PDF coordinates)."""
        if not self._rects:
            return None, None
        c = pos + self._offset()
        if clamp_page is None:
            i = self.page_at_y(c.y())
            r = self._rects[i]
            if not r.adjusted(-GAP, -GAP / 2, GAP, GAP / 2).contains(c):
                return None, None
        else:
            i = clamp_page
            r = self._rects[i]
        s = self.scale()
        rx, ry = (c.x() - r.left()) / s, (c.y() - r.top()) / s
        if clamp_page is not None:
            w, h = self.geo(i)[:2]
            rx, ry = max(0, min(w, rx)), max(0, min(h, ry))
        return i, xform(self.geo(i)[3], rx, ry)

    def to_view(self, i: int, x: float, y: float) -> QPointF:
        rx, ry = xform(self.geo(i)[2], x, y)
        r = self._rects[i]
        s = self.scale()
        return QPointF(r.left() + rx * s, r.top() + ry * s) - self._offset()

    def rect_to_view(self, i: int, rect) -> QRectF:
        x0, y0, x1, y1 = rect
        a, b = self.to_view(i, x0, y0), self.to_view(i, x1, y1)
        return QRectF(a, b).normalized()

    def view_rect_to_page(self, i: int, vr: QRectF) -> tuple:
        _, a = self.to_page(vr.topLeft(), i)
        _, b = self.to_page(vr.bottomRight(), i)
        return (min(a[0], b[0]), min(a[1], b[1]), max(a[0], b[0]), max(a[1], b[1]))

    # ===== zoom / navigation ===================================================================

    def set_zoom(self, z: float, anchor_pos: QPointF | None = None):
        z = max(MIN_ZOOM, min(MAX_ZOOM, z))
        if anchor_pos is None:
            anchor_pos = QPointF(self.viewport().width() / 2, self.viewport().height() / 2)
        c = anchor_pos + self._offset()
        old = self.scale()
        self.fit_mode = None
        self.zoom = z
        i = self.page_at_y(c.y())
        r = self._rects[i] if self._rects else QRectF()
        fx = (c.x() - r.left()) / max(old, 1e-6)
        fy = (c.y() - r.top()) / max(old, 1e-6)
        self.relayout(keep_anchor=False)
        if self._rects:
            r = self._rects[i]
            s = self.scale()
            self.horizontalScrollBar().setValue(int(r.left() + fx * s - anchor_pos.x()))
            self.verticalScrollBar().setValue(int(r.top() + fy * s - anchor_pos.y()))

    def zoom_step(self, direction: int, anchor_pos=None):
        z = self.zoom
        if direction > 0:
            nxt = next((s for s in ZOOM_STEPS if s > z * 1.01), MAX_ZOOM)
        else:
            nxt = next((s for s in reversed(ZOOM_STEPS) if s < z * 0.99), MIN_ZOOM)
        self.set_zoom(nxt, anchor_pos)

    def set_fit(self, mode: str):
        self.fit_mode = mode
        keep = self.current_page
        self.relayout(keep_anchor=False)
        self.go_to_page(keep)

    def go_to_page(self, i: int, y: float | None = None):
        if not self._rects:
            return
        i = max(0, min(i, len(self._rects) - 1))
        r = self._rects[i]
        top = r.top() - (GAP if y is None else 0)
        if y is not None:
            _, py = xform(self.geo(i)[2], 0, y)
            top = r.top() + py * self.scale() - 20
        self.verticalScrollBar().setValue(int(top))
        self._set_current(i)

    def ensure_visible(self, i: int, rect, margin=60):
        vr = self.rect_to_view(i, rect)
        vp = self.viewport().rect()
        if vp.contains(vr.toRect()):
            return
        vs, hs = self.verticalScrollBar(), self.horizontalScrollBar()
        if vr.top() < margin or vr.bottom() > vp.height() - margin:
            vs.setValue(int(vs.value() + vr.center().y() - vp.height() / 2))
        if vr.left() < 0 or vr.right() > vp.width():
            hs.setValue(int(hs.value() + vr.center().x() - vp.width() / 2))

    def _set_current(self, i):
        if i != self.current_page:
            self.current_page = i
            self.page_changed.emit(i)

    def _track_current(self):
        if not self._rects:
            return
        off = self._offset()
        vh = self.viewport().height()
        best, best_area = self.current_page, -1
        for i in self.visible_pages():
            r = self._rects[i]
            vis = min(r.bottom(), off.y() + vh) - max(r.top(), off.y())
            if vis > best_area + 1:
                best, best_area = i, vis
        self._set_current(best)

    # ===== document changes =======================================================================

    def document_changed(self):
        """Called after any edit, undo or redo."""
        n = self.doc.page_count
        live = set(self.doc.page_ids)
        for cache in (self._geo, self._text, self._fields, self._annots, self._links):
            for k in [k for k in cache if k not in live]:
                if len(cache) > 400:
                    cache.pop(k, None)
        if self.sel and self.sel[0] >= n:
            self.sel = None
        if self.sel_annot is not None:
            self.sel_annot = self._refresh_annot(self.sel_annot)
            self.annot_selected.emit(self.sel_annot)
        self.current_page = min(self.current_page, max(0, n - 1))
        self.renderer.set_document(self.doc_key, self.doc.version, self.doc.tobytes)
        self.relayout()

    def _refresh_annot(self, info):
        if info.page >= self.doc.page_count:
            return None
        for a in self.annots(info.page):
            if a.xref == info.xref:
                return a
        return None

    def text(self, i: int) -> T.PageText:
        pid = self.doc.page_ids[i]
        t = self._text.get(pid)
        if t is None:
            t = T.extract(self.doc.page(i))
            self._text[pid] = t
        return t

    def fields(self, i: int) -> list:
        pid = self.doc.page_ids[i]
        f = self._fields.get(pid)
        if f is None:
            f = F.fields(self.doc, i)
            self._fields[pid] = f
        return f

    def annots(self, i: int) -> list:
        pid = self.doc.page_ids[i]
        a = self._annots.get(pid)
        if a is None:
            a = A.list_page(self.doc, i)
            self._annots[pid] = a
        return a

    def links(self, i: int) -> list:
        pid = self.doc.page_ids[i]
        lk = self._links.get(pid)
        if lk is None:
            lk = M.links(self.doc, i)
            self._links[pid] = lk
        return lk

    # ===== anchored widgets (inline editors) =======================================================

    def anchor_widget(self, w: QWidget, page: int, rect: tuple, font_pt: float | None = None):
        self._anchored = [a for a in self._anchored if a[0] is not w]
        self._anchored.append((w, page, rect, font_pt))
        w.setParent(self.viewport())
        self._place(w, page, rect, font_pt)
        w.show()

    def unanchor(self, w: QWidget):
        self._anchored = [a for a in self._anchored if a[0] is not w]

    def update_anchor_rect(self, w: QWidget, rect: tuple):
        for k, a in enumerate(self._anchored):
            if a[0] is w:
                self._anchored[k] = (w, a[1], rect, a[3])
                self._place(w, a[1], rect, a[3])

    def _place(self, w, page, rect, font_pt):
        if page >= len(self._rects):
            return
        vr = self.rect_to_view(page, rect).toAlignedRect()
        w.setGeometry(vr.adjusted(-1, -1, 1, 1))
        if font_pt:
            f = w.font()
            f.setPixelSize(max(6, int(round(font_pt * self.scale()))))
            w.setFont(f)

    def _reposition_anchored(self):
        for w, page, rect, font_pt in self._anchored:
            self._place(w, page, rect, font_pt)

    # ===== painting ==================================================================================

    def scrollContentsBy(self, dx, dy):
        self.viewport().update()
        self._reposition_anchored()
        self._track_current()

    def resizeEvent(self, ev):
        super().resizeEvent(ev)
        if self.fit_mode:
            self.relayout()
        else:
            self._update_scrollbars()
            # recentre pages horizontally
            self.relayout()

    def _image_ready(self, key, img):
        if isinstance(key, tuple) and key and key[0] in ("t", "p") and key[1] == self.doc_key:
            self.viewport().update()

    def _tile_key(self, i, z, tx, ty):
        return ("t", self.doc_key, self.doc.page_ids[i], round(z * 1000), tx, ty)

    def _preview_key(self, i):
        return ("p", self.doc_key, self.doc.page_ids[i])

    def paintEvent(self, ev):
        p = QPainter(self.viewport())
        c = theme.current
        p.fillRect(self.viewport().rect(), QColor(c["canvas"]))
        if not self._rects:
            return
        dpr = self.devicePixelRatioF()
        s = self.scale()
        z = s * dpr  # device px per pt
        vp = QRectF(self.viewport().rect())
        cache, thumbs = self.renderer.cache, self.renderer.thumbs
        for i in self.visible_pages():
            r = self.page_rect_view(i)
            p.fillRect(r.adjusted(1, 2, 3, 4), QColor(0, 0, 0, 40))
            p.fillRect(r, Qt.GlobalColor.white)
            # placeholder: low-res preview, stale if the page just changed
            prev = thumbs.get(self._preview_key(i))
            if prev is not None:
                self._last_preview[i] = prev
            else:
                prev = self._last_preview.get(i)
            vis = r.intersected(vp)
            tx0 = int(max(0, (vis.left() - r.left()) * dpr) // TILE)
            ty0 = int(max(0, (vis.top() - r.top()) * dpr) // TILE)
            tx1 = int(max(0, (vis.right() - r.left()) * dpr - 1) // TILE)
            ty1 = int(max(0, (vis.bottom() - r.top()) * dpr - 1) // TILE)
            missing = False
            if prev is not None:
                # draw the preview only where tiles are missing (cheap enough to draw whole)
                for ty in range(ty0, ty1 + 1):
                    for tx in range(tx0, tx1 + 1):
                        if self._tile_key(i, z, tx, ty) not in cache:
                            missing = True
                            break
                    if missing:
                        break
                if missing:
                    p.setRenderHint(QPainter.RenderHint.SmoothPixmapTransform, True)
                    p.drawImage(r, prev)
            for ty in range(ty0, ty1 + 1):
                for tx in range(tx0, tx1 + 1):
                    img = cache.get(self._tile_key(i, z, tx, ty))
                    if img is None:
                        continue
                    tr = QRectF(r.left() + tx * TILE / dpr, r.top() + ty * TILE / dpr,
                                img.width() / dpr, img.height() / dpr)
                    p.drawImage(tr, img)
            self._paint_overlays(p, i, r)
        self._paint_drag(p)
        p.end()
        self._req_timer.start()

    def _request_visible(self):
        if not self._rects:
            return
        dpr = self.devicePixelRatioF()
        s = self.scale()
        z = s * dpr
        vp = QRectF(self.viewport().rect())
        cache = self.renderer.cache
        reqs = []
        vis_pages = self.visible_pages()
        wanted_pages = list(vis_pages)
        if vis_pages and vis_pages[-1] + 1 < len(self._rects):
            wanted_pages.append(vis_pages[-1] + 1)
        ver = self.doc.version
        for i in wanted_pages:
            r = self.page_rect_view(i)
            w, h = self.geo(i)[:2]
            full_w, full_h = int(math.ceil(w * z)), int(math.ceil(h * z))
            area = r.intersected(vp) if i in vis_pages else QRectF(r.left(), r.top(), r.width(),
                                                                   min(r.height(), vp.height()))
            tx0 = int(max(0, (area.left() - r.left()) * dpr) // TILE)
            ty0 = int(max(0, (area.top() - r.top()) * dpr) // TILE)
            tx1 = int(max(0, (area.right() - r.left()) * dpr - 1) // TILE)
            ty1 = int(max(0, (area.bottom() - r.top()) * dpr - 1) // TILE)
            for ty in range(ty0, ty1 + 1):
                for tx in range(tx0, tx1 + 1):
                    key = self._tile_key(i, z, tx, ty)
                    if key in cache:
                        continue
                    cw = min(TILE, full_w - tx * TILE)
                    ch = min(TILE, full_h - ty * TILE)
                    if cw <= 0 or ch <= 0:
                        continue
                    reqs.append(Request("tile", self.doc_key, ver, i, z, (tx * TILE, ty * TILE, cw, ch), key))
        self.renderer.view.want(reqs)
        # previews for visible pages, to show instantly on zoom/scroll
        preq = []
        for i in wanted_pages:
            key = self._preview_key(i)
            if key in self.renderer.thumbs or self.renderer.side.is_pending(key):
                continue
            w, h = self.geo(i)[:2]
            pz = min(1.0, 500 / max(w, h))
            preq.append(Request("thumb", self.doc_key, ver, i, pz, None, key))
        if preq:
            self.renderer.side.want(preq, keep_others=True)

    def _paint_overlays(self, p: QPainter, i: int, r: QRectF):
        c = theme.current
        # form fields
        if self.highlight_fields:
            for f in self.fields(i):
                if f.readonly:
                    continue
                vr = self.rect_to_view(i, f.rect)
                p.fillRect(vr, QColor(204, 215, 255, 110))
        # search hits
        for k, rects in enumerate(self.hits.get(i, [])):
            cur = self.current_hit == (i, k)
            col = QColor(255, 150, 0, 150) if cur else QColor(255, 230, 0, 110)
            for rr in rects:
                p.fillRect(self.rect_to_view(i, rr), col)
        # text selection
        if self.sel and self.sel[0] == i and self.sel[1] != self.sel[2]:
            a, b = sorted(self.sel[1:])
            col = QColor(c["accent"])
            col.setAlpha(80)
            for rr in self.text(i).rects_for(a, b):
                p.fillRect(self.rect_to_view(i, rr), col)
        # field suggestions
        if self.suggestions is not None:
            for sgg in self.suggestions:
                if sgg["page"] != i:
                    continue
                vr = self.rect_to_view(i, sgg["rect"])
                ok = sgg.get("accepted", True)
                col = QColor(30, 160, 60) if ok else QColor(210, 40, 40)
                fill = QColor(col)
                fill.setAlpha(40)
                p.fillRect(vr, fill)
                pen = QPen(col, 1.5, Qt.PenStyle.DashLine)
                p.setPen(pen)
                p.drawRect(vr)
        # selected annotation
        a = self.sel_annot
        if a is not None and a.page == i:
            vr = self.rect_to_view(i, a.rect).adjusted(-2, -2, 2, 2)
            pen = QPen(QColor(c["accent"]), 1, Qt.PenStyle.DashLine)
            p.setPen(pen)
            p.setBrush(Qt.BrushStyle.NoBrush)
            p.drawRect(vr)
            if a.type in A.RESIZABLE:
                p.setBrush(QColor("white"))
                p.setPen(QPen(QColor(c["accent"]), 1))
                for h in self._handles(vr).values():
                    p.drawRect(h)

    def _handles(self, vr: QRectF) -> dict:
        hs = HANDLE
        pts = {
            "tl": vr.topLeft(), "tr": vr.topRight(), "bl": vr.bottomLeft(), "br": vr.bottomRight(),
            "t": QPointF(vr.center().x(), vr.top()), "b": QPointF(vr.center().x(), vr.bottom()),
            "l": QPointF(vr.left(), vr.center().y()), "r": QPointF(vr.right(), vr.center().y()),
        }
        return {k: QRectF(v.x() - hs, v.y() - hs, 2 * hs, 2 * hs) for k, v in pts.items()}

    def _paint_drag(self, p: QPainter):
        d = self._drag
        if not d:
            return
        c = theme.current
        accent = QColor(c["accent"])
        kind = d["kind"]
        if kind in ("rect_tool",):
            vr = QRectF(d["start"], d["pos"]).normalized()
            tool = d["tool"]
            col = QColor(self.ctl.tool_color()) if tool in ("rect", "oval") else accent
            if tool == "redact":
                col = QColor(200, 0, 0)
            p.setPen(QPen(col, max(1, self.ctl.tool_width() * self.scale()) if tool in ("rect", "oval") else 1,
                          Qt.PenStyle.SolidLine if tool in ("rect", "oval") else Qt.PenStyle.DashLine))
            p.setBrush(Qt.BrushStyle.NoBrush)
            if tool == "oval":
                p.drawEllipse(vr)
            elif tool in ("arrow", "line"):
                p.drawLine(d["start"], d["pos"])
            elif tool == "sign" and self.pending_signature:
                p.drawRect(self._aspect_rect(d["start"], d["pos"]))
            else:
                p.drawRect(vr)
        elif kind == "pen":
            col = QColor(self.ctl.tool_color())
            p.setRenderHint(QPainter.RenderHint.Antialiasing)
            p.setPen(QPen(col, max(1, self.ctl.tool_width() * self.scale()), Qt.PenStyle.SolidLine,
                          Qt.PenCapStyle.RoundCap, Qt.PenJoinStyle.RoundJoin))
            pts = [self.to_view(d["page"], *pt) for pt in d["points"]]
            if len(pts) > 1:
                p.drawPolyline(QPolygonF(pts))
        elif kind in ("move", "resize"):
            p.setPen(QPen(accent, 1, Qt.PenStyle.DashLine))
            p.setBrush(QColor(accent.red(), accent.green(), accent.blue(), 30))
            p.drawRect(d["ghost"])

    # ===== hit testing ========================================================================

    def field_at(self, i, x, y):
        for f in reversed(self.fields(i)):
            r = f.rect
            if r[0] <= x <= r[2] and r[1] <= y <= r[3]:
                return f
        return None

    def link_at(self, i, x, y):
        for lk in self.links(i):
            r = lk.rect
            if r[0] <= x <= r[2] and r[1] <= y <= r[3]:
                return lk
        return None

    def annot_at(self, i, x, y):
        return A.hit_test(self.doc, i, x, y, tol=3 / max(self.scale(), 0.1) + 1)

    def _handle_at(self, pos: QPointF):
        a = self.sel_annot
        if a is None or a.type not in A.RESIZABLE or a.page >= len(self._rects):
            return None
        vr = self.rect_to_view(a.page, a.rect).adjusted(-2, -2, 2, 2)
        for k, h in self._handles(vr).items():
            if h.adjusted(-3, -3, 3, 3).contains(pos):
                return k
        return None

    # ===== mouse ====================================================================================

    def set_tool(self, tool: str):
        if tool != self.tool:
            self.tool = tool
            self._drag = None
            if tool not in ("select",):
                self.select_annot(None)
            self.tool_changed.emit(tool)
            self._update_cursor(None)
            self.viewport().update()

    def _update_cursor(self, pos):
        t = self.tool
        if self._space_pan or t == "hand":
            cur = Qt.CursorShape.ClosedHandCursor if self._drag and self._drag["kind"] == "pan" else Qt.CursorShape.OpenHandCursor
        elif t in MARKUP_TOOLS:
            cur = Qt.CursorShape.IBeamCursor
        elif t in ("pen",) or t in DRAG_TOOLS or t in ("note", "addtext", "redact"):
            cur = Qt.CursorShape.CrossCursor
            if t == "addtext":
                cur = Qt.CursorShape.IBeamCursor
        elif t == "detect":
            cur = Qt.CursorShape.PointingHandCursor
        else:
            cur = Qt.CursorShape.ArrowCursor
            if pos is not None:
                h = self._handle_at(pos)
                if h:
                    cur = {"tl": Qt.CursorShape.SizeFDiagCursor, "br": Qt.CursorShape.SizeFDiagCursor,
                           "tr": Qt.CursorShape.SizeBDiagCursor, "bl": Qt.CursorShape.SizeBDiagCursor,
                           "t": Qt.CursorShape.SizeVerCursor, "b": Qt.CursorShape.SizeVerCursor,
                           "l": Qt.CursorShape.SizeHorCursor, "r": Qt.CursorShape.SizeHorCursor}[h]
                else:
                    i, pt = self.to_page(pos)
                    if i is not None:
                        if self.field_at(i, *pt) and t == "select":
                            f = self.field_at(i, *pt)
                            cur = Qt.CursorShape.IBeamCursor if f.kind == F.TEXT else Qt.CursorShape.PointingHandCursor
                        elif self.link_at(i, *pt):
                            cur = Qt.CursorShape.PointingHandCursor
                        elif self.sel_annot and self.sel_annot.page == i and \
                                self.rect_to_view(i, self.sel_annot.rect).contains(pos):
                            cur = Qt.CursorShape.SizeAllCursor
                        elif self.text(i).hit(*pt):
                            cur = Qt.CursorShape.IBeamCursor
        self.viewport().setCursor(QCursor(cur))

    def _click_count(self) -> int:
        now = time.monotonic()
        interval = QGuiApplication.styleHints().mouseDoubleClickInterval() / 1000
        self._click_times = [t for t in self._click_times if now - t < interval * 2][-2:]
        if self._click_times and now - self._click_times[-1] > interval:
            self._click_times = []
        self._click_times.append(now)
        return len(self._click_times)

    def mousePressEvent(self, ev):
        self.setFocus()
        pos = ev.position()
        if ev.button() == Qt.MouseButton.MiddleButton or (ev.button() == Qt.MouseButton.LeftButton and
                                                         (self.tool == "hand" or self._space_pan)):
            self._drag = {"kind": "pan", "last": ev.globalPosition(), "start": pos}
            self._update_cursor(pos)
            return
        if ev.button() == Qt.MouseButton.RightButton:
            return
        if ev.button() != Qt.MouseButton.LeftButton:
            return
        clicks = self._click_count()
        i, pt = self.to_page(pos)
        t = self.tool
        if t == "select":
            self._press_select(pos, i, pt, clicks, ev.modifiers())
        elif t in MARKUP_TOOLS or t == "redact":
            if i is None:
                return
            if t == "redact" and not self.text(i).hit(*pt):
                self._drag = {"kind": "rect_tool", "tool": "redact", "page": i, "start": pos, "pos": pos}
                return
            idx = self.text(i).index_at(*pt)
            self.sel = (i, idx or 0, idx or 0)
            self._drag = {"kind": "markup", "tool": t, "page": i, "start": pos}
        elif t == "pen":
            if i is None:
                return
            self._drag = {"kind": "pen", "page": i, "points": [pt], "start": pos}
        elif t in DRAG_TOOLS:
            if i is None:
                return
            self._drag = {"kind": "rect_tool", "tool": t, "page": i, "start": pos, "pos": pos}
        elif t == "note":
            if i is not None:
                self.ctl.create_note(i, pt)
        elif t == "addtext":
            if i is not None:
                self.ctl.create_plain_text(i, pt)
        elif t == "detect":
            if i is not None and self.suggestions is not None:
                for sgg in self.suggestions:
                    r = sgg["rect"]
                    if sgg["page"] == i and r[0] <= pt[0] <= r[2] and r[1] <= pt[1] <= r[3]:
                        sgg["accepted"] = not sgg.get("accepted", True)
                        self.ctl.suggestions_changed()
                        self.viewport().update()
                        break

    def _press_select(self, pos, i, pt, clicks, mods):
        h = self._handle_at(pos)
        if h:
            a = self.sel_annot
            self._drag = {"kind": "resize", "handle": h, "page": a.page, "start": pos,
                          "orig": self.rect_to_view(a.page, a.rect), "ghost": self.rect_to_view(a.page, a.rect)}
            return
        if i is None:
            self.clear_selection()
            self.select_annot(None)
            return
        # form fields take the click
        f = self.field_at(i, *pt)
        if f is not None and not f.readonly:
            self.select_annot(None)
            self.clear_selection()
            self.ctl.field_clicked(f)
            return
        # already-selected annotation: start moving
        if self.sel_annot and self.sel_annot.page == i and \
                self.rect_to_view(i, self.sel_annot.rect).adjusted(-3, -3, 3, 3).contains(pos):
            if clicks >= 2:
                self.ctl.edit_annot_text(self.sel_annot)
                return
            vr = self.rect_to_view(i, self.sel_annot.rect)
            self._drag = {"kind": "move", "page": i, "start": pos, "orig": vr, "ghost": vr, "moved": False}
            return
        pt_text = self.text(i)
        on_text = pt_text.hit(*pt)
        hit = self.annot_at(i, *pt)
        lk = self.link_at(i, *pt)
        if hit is not None:
            # markup over text: decide on release (click selects it, drag selects text)
            if hit.type in A.MARKUP and on_text:
                idx = pt_text.index_at(*pt)
                self._drag = {"kind": "maybe_annot", "page": i, "start": pos, "annot": hit,
                              "index": idx or 0}
                return
            self.clear_selection()
            self.select_annot(hit)
            if clicks >= 2:
                self.ctl.edit_annot_text(hit)
                return
            vr = self.rect_to_view(i, hit.rect)
            self._drag = {"kind": "move", "page": i, "start": pos, "orig": vr, "ghost": vr, "moved": False}
            return
        self.select_annot(None)
        if lk is not None and not on_text:
            self._drag = {"kind": "link", "link": lk, "start": pos}
            return
        idx = pt_text.index_at(*pt)
        if idx is None:
            self.clear_selection()
            if lk is not None:
                self._drag = {"kind": "link", "link": lk, "start": pos}
            return
        if mods & Qt.KeyboardModifier.ShiftModifier and self.sel and self.sel[0] == i:
            self.sel = (i, self.sel[1], idx)
        elif clicks == 2:
            s, e = pt_text.word_at(idx if idx < len(pt_text.text) else idx - 1)
            self.sel = (i, s, e)
        elif clicks >= 3:
            s, e = pt_text.line_at(idx if idx < len(pt_text.text) else idx - 1)
            self.sel = (i, s, e)
        else:
            self.sel = (i, idx, idx)
        self._drag = {"kind": "text", "page": i, "start": pos, "link": lk, "clicks": clicks}
        self.selection_changed.emit()
        self.viewport().update()

    def mouseMoveEvent(self, ev):
        pos = ev.position()
        d = self._drag
        if not d:
            self._update_cursor(pos)
            self._hover_tooltip(pos)
            return
        kind = d["kind"]
        if kind == "pan":
            delta = ev.globalPosition() - d["last"]
            d["last"] = ev.globalPosition()
            self.horizontalScrollBar().setValue(int(self.horizontalScrollBar().value() - delta.x()))
            self.verticalScrollBar().setValue(int(self.verticalScrollBar().value() - delta.y()))
            return
        self._autoscroll(pos)
        if kind == "maybe_annot":
            if (pos - d["start"]).manhattanLength() > 4:
                i = d["page"]
                self.sel = (i, d["index"], d["index"])
                self._drag = d = {"kind": "text", "page": i, "start": d["start"], "link": None, "clicks": 1}
            else:
                return
        if kind == "link" and (pos - d["start"]).manhattanLength() > 4:
            self._drag = None
            return
        if d["kind"] in ("text", "markup"):
            i = d["page"]
            _, pt = self.to_page(pos, clamp_page=i)
            idx = self.text(i).index_at(*pt)
            if idx is not None and self.sel:
                if d.get("clicks", 1) == 2:
                    a, b = self.sel[1], self.sel[2]
                    s, e = self.text(i).word_at(idx if idx < len(self.text(i).text) else idx - 1)
                    self.sel = (i, min(a, s), max(b, e))
                else:
                    self.sel = (i, self.sel[1], idx)
                self.selection_changed.emit()
                self.viewport().update()
        elif kind == "rect_tool":
            d["pos"] = pos
            self.viewport().update()
        elif kind == "pen":
            _, pt = self.to_page(pos, clamp_page=d["page"])
            d["points"].append(pt)
            self.viewport().update()
        elif kind == "move":
            delta = pos - d["start"]
            if delta.manhattanLength() > 2:
                d["moved"] = True
            d["ghost"] = d["orig"].translated(delta)
            self.viewport().update()
        elif kind == "resize":
            d["ghost"] = self._resized(d["orig"], d["handle"], pos - d["start"])
            self.viewport().update()

    def _resized(self, r: QRectF, h: str, delta: QPointF) -> QRectF:
        r = QRectF(r)
        if "l" in h:
            r.setLeft(r.left() + delta.x())
        if "r" in h:
            r.setRight(r.right() + delta.x())
        if "t" in h:
            r.setTop(r.top() + delta.y())
        if "b" in h:
            r.setBottom(r.bottom() + delta.y())
        r = r.normalized()
        a = self.sel_annot
        if a is not None and (a.is_signature or a.type == A.STAMP) and len(h) == 2:
            ow, oh = self._drag["orig"].width(), self._drag["orig"].height()
            if ow > 0 and oh > 0:
                ratio = ow / oh
                w = max(r.width(), r.height() * ratio)
                hgt = w / ratio
                anchor = {"tl": self._drag["orig"].bottomRight(), "tr": self._drag["orig"].bottomLeft(),
                          "bl": self._drag["orig"].topRight(), "br": self._drag["orig"].topLeft()}[h]
                sx = -1 if "l" in h else 1
                sy = -1 if "t" in h else 1
                r = QRectF(anchor, QPointF(anchor.x() + sx * w, anchor.y() + sy * hgt)).normalized()
        if r.width() < 6:
            r.setWidth(6)
        if r.height() < 6:
            r.setHeight(6)
        return r

    def _autoscroll(self, pos):
        vp = self.viewport().rect()
        vs = self.verticalScrollBar()
        if pos.y() < 0:
            vs.setValue(int(vs.value() + pos.y() / 2))
        elif pos.y() > vp.height():
            vs.setValue(int(vs.value() + (pos.y() - vp.height()) / 2))

    def _aspect_rect(self, a: QPointF, b: QPointF) -> QRectF:
        w0, h0 = self.pending_signature_size
        ratio = w0 / h0 if h0 else 3
        r = QRectF(a, b).normalized()
        w = max(r.width(), r.height() * ratio)
        return QRectF(r.left(), r.top(), w, w / ratio)

    def mouseReleaseEvent(self, ev):
        d = self._drag
        self._drag = None
        pos = ev.position()
        if not d:
            return
        kind = d["kind"]
        if kind == "pan":
            self._update_cursor(pos)
            return
        if kind == "maybe_annot":
            self.clear_selection()
            self.select_annot(d["annot"])
            return
        if kind == "link":
            self.ctl.follow_link(d["link"])
            return
        if kind == "text":
            if self.sel and self.sel[1] == self.sel[2]:
                if d.get("link") is not None and (pos - d["start"]).manhattanLength() < 4:
                    self.ctl.follow_link(d["link"])
                self.sel = None
                self.selection_changed.emit()
            self.viewport().update()
            return
        if kind == "markup":
            i = d["page"]
            if self.sel and self.sel[1] != self.sel[2]:
                a, b = sorted(self.sel[1:])
                rects = self.text(i).rects_for(a, b)
                self.sel = None
                self.selection_changed.emit()
                if rects:
                    if d["tool"] == "redact":
                        self.ctl.create_redaction(i, rects)
                    else:
                        self.ctl.create_markup(i, MARKUP_TOOLS[d["tool"]], rects)
            self.viewport().update()
            return
        if kind == "pen":
            pts = d["points"]
            if len(pts) >= 2:
                self.ctl.create_ink(d["page"], pts)
            self.viewport().update()
            return
        if kind == "rect_tool":
            self._finish_rect_tool(d, pos)
            self.viewport().update()
            return
        if kind == "move":
            a = self.sel_annot
            if d["moved"] and a is not None:
                i = d["page"]
                s = self.scale()
                dvx, dvy = (d["ghost"].left() - d["orig"].left()) / s, (d["ghost"].top() - d["orig"].top()) / s
                dx, dy = xform(self.geo(i)[3], dvx, dvy)
                ox, oy = xform(self.geo(i)[3], 0, 0)
                self.ctl.move_annot(a, dx - ox, dy - oy)
            self.viewport().update()
            return
        if kind == "resize":
            a = self.sel_annot
            if a is not None:
                self.ctl.resize_annot(a, self._unclamped_rect(d["page"], d["ghost"]))
            self.viewport().update()

    def _unclamped_rect(self, i, vr: QRectF):
        s = self.scale()
        r = self._rects[i].translated(-self._offset())
        pts = [((q.x() - r.left()) / s, (q.y() - r.top()) / s) for q in (vr.topLeft(), vr.bottomRight())]
        (x0, y0), (x1, y1) = (xform(self.geo(i)[3], *pts[0]), xform(self.geo(i)[3], *pts[1]))
        return (min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1))

    def _finish_rect_tool(self, d, pos):
        i = d["page"]
        tool = d["tool"]
        start = d["start"]
        is_click = (pos - start).manhattanLength() < 5
        s = self.scale()
        if tool in ("arrow", "line"):
            if is_click:
                return
            _, p1 = self.to_page(start, i)
            _, p2 = self.to_page(pos, i)
            self.ctl.create_arrow(i, p1, p2, tool == "arrow")
            return
        if tool == "sign":
            if not self.pending_signature:
                return
            w0, h0 = self.pending_signature_size
            if is_click:
                vr = QRectF(start.x() - w0 * s / 2, start.y() - h0 * s / 2, w0 * s, h0 * s)
            else:
                vr = self._aspect_rect(start, pos)
            self.ctl.create_signature(i, self.view_rect_to_page(i, vr))
            return
        if tool == "stamp":
            w, h = 160 * s, 50 * s
            vr = QRectF(start.x() - w / 2, start.y() - h / 2, w, h) if is_click else QRectF(start, pos).normalized()
            self.ctl.create_stamp(i, self.view_rect_to_page(i, vr))
            return
        defaults = {"textbox": (200, 60), "rect": (120, 80), "oval": (120, 80), "field_text": (160, 20),
                    "field_date": (100, 20), "field_check": (14, 14), "redact": (0, 0)}
        if is_click:
            w, h = defaults.get(tool, (100, 40))
            if not w:
                return
            vr = QRectF(start.x(), start.y(), w * s, h * s)
            if tool == "field_check":
                vr.moveCenter(start)
        else:
            vr = QRectF(start, pos).normalized()
        rect = self.view_rect_to_page(i, vr)
        if tool == "textbox":
            self.ctl.create_textbox(i, rect)
        elif tool in ("rect", "oval"):
            self.ctl.create_shape(i, A.SQUARE if tool == "rect" else A.CIRCLE, rect)
        elif tool.startswith("field_"):
            self.ctl.create_field(i, rect, {"field_text": "text", "field_check": "checkbox",
                                            "field_date": "date"}[tool])
        elif tool == "redact":
            self.ctl.create_redaction(i, [rect])

    def mouseDoubleClickEvent(self, ev):
        # handled through click counting in mousePressEvent
        self.mousePressEvent(ev)

    def contextMenuEvent(self, ev):
        pos = QPointF(ev.pos())
        i, pt = self.to_page(pos)
        ctx = {"page": i, "point": pt, "annot": None, "field": None, "has_selection": self.has_selection()}
        if i is not None:
            f = self.field_at(i, *pt)
            if f is not None:
                ctx["field"] = f
            else:
                a = self.annot_at(i, *pt)
                if a is not None:
                    self.select_annot(a)
                    ctx["annot"] = a
        self.context_menu.emit(ev.globalPos(), ctx)

    def _hover_tooltip(self, pos):
        i, pt = self.to_page(pos)
        tip = ""
        if i is not None:
            a = None
            for an in self.annots(i):
                if an.type == A.NOTE:
                    r = an.rect
                    if r[0] - 2 <= pt[0] <= r[2] + 2 and r[1] - 2 <= pt[1] <= r[3] + 2:
                        a = an
            if a is not None:
                tip = (a.author + ":\n" if a.author else "") + (a.content or "")
            else:
                lk = self.link_at(i, *pt)
                if lk is not None:
                    tip = lk.uri or (f"Go to page {lk.page + 1}" if lk.page is not None else "")
        if tip != self.viewport().toolTip():
            self.viewport().setToolTip(tip)

    def wheelEvent(self, ev):
        if ev.modifiers() & Qt.KeyboardModifier.ControlModifier:
            dy = ev.angleDelta().y()
            if dy:
                factor = 1.0015 ** dy
                self.set_zoom(self.zoom * factor, ev.position())
            ev.accept()
            return
        super().wheelEvent(ev)

    def keyPressEvent(self, ev):
        k = ev.key()
        vs = self.verticalScrollBar()
        mods = ev.modifiers()
        if k == Qt.Key.Key_Escape:
            if self._drag:
                self._drag = None
            elif self.sel_annot:
                self.select_annot(None)
            elif self.tool not in ("select",):
                self.ctl.set_tool("select")
            else:
                self.clear_selection()
            self.viewport().update()
            return
        if k in (Qt.Key.Key_Delete, Qt.Key.Key_Backspace) and self.sel_annot:
            self.ctl.delete_annot(self.sel_annot)
            return
        if k == Qt.Key.Key_PageDown or (k == Qt.Key.Key_Space and not mods & Qt.KeyboardModifier.ShiftModifier):
            vs.setValue(vs.value() + int(self.viewport().height() * 0.9))
        elif k == Qt.Key.Key_PageUp or (k == Qt.Key.Key_Space and mods & Qt.KeyboardModifier.ShiftModifier):
            vs.setValue(vs.value() - int(self.viewport().height() * 0.9))
        elif k == Qt.Key.Key_Home and not mods & Qt.KeyboardModifier.ShiftModifier:
            vs.setValue(0)
        elif k == Qt.Key.Key_End and not mods & Qt.KeyboardModifier.ShiftModifier:
            vs.setValue(vs.maximum())
        elif k == Qt.Key.Key_Down:
            vs.setValue(vs.value() + 40)
        elif k == Qt.Key.Key_Up:
            vs.setValue(vs.value() - 40)
        elif k == Qt.Key.Key_Right:
            if self.horizontalScrollBar().maximum() > 0:
                self.horizontalScrollBar().setValue(self.horizontalScrollBar().value() + 40)
            else:
                self.go_to_page(self.current_page + 1)
        elif k == Qt.Key.Key_Left:
            if self.horizontalScrollBar().maximum() > 0:
                self.horizontalScrollBar().setValue(self.horizontalScrollBar().value() - 40)
            else:
                self.go_to_page(self.current_page - 1)
        else:
            super().keyPressEvent(ev)

    # ===== selection API ======================================================================

    def has_selection(self) -> bool:
        return bool(self.sel and self.sel[1] != self.sel[2])

    def selected_text(self) -> str:
        if not self.has_selection():
            return ""
        i, a, b = self.sel
        a, b = sorted((a, b))
        return self.text(i).text[a:b]

    def selection_rects(self):
        if not self.has_selection():
            return None, []
        i, a, b = self.sel
        a, b = sorted((a, b))
        return i, self.text(i).rects_for(a, b)

    def clear_selection(self):
        if self.sel is not None:
            self.sel = None
            self.selection_changed.emit()
            self.viewport().update()

    def select_all(self):
        i = self.current_page
        self.sel = (i, 0, len(self.text(i).text))
        self.selection_changed.emit()
        self.viewport().update()

    def select_annot(self, a):
        if a is None and self.sel_annot is None:
            return
        self.sel_annot = a
        self.annot_selected.emit(a)
        self.viewport().update()
