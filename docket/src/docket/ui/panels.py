"""Right-rail panels: page thumbnails, bookmarks, comments."""

from __future__ import annotations

import os

from PySide6.QtCore import QMimeData, QPoint, QRect, QSize, Qt, QTimer, Signal
from PySide6.QtGui import QAction, QColor, QDrag, QImage, QPainter, QPen, QPixmap
from PySide6.QtWidgets import (QAbstractItemView, QFrame, QHBoxLayout, QLabel, QListWidget,
                               QListWidgetItem, QMenu, QPushButton, QStyle, QStyledItemDelegate, QToolButton,
                               QTreeWidget, QTreeWidgetItem, QVBoxLayout, QWidget)

from ..core import annots as A
from ..core import meta as M
from ..core.pages import IMAGE_EXTS
from . import icons, theme
from .renderer import Request

THUMB_W = 120


def pane_header(title: str, *buttons) -> QWidget:
    w = QWidget()
    lay = QHBoxLayout(w)
    lay.setContentsMargins(10, 8, 6, 4)
    lbl = QLabel(title)
    lbl.setObjectName("paneTitle")
    lay.addWidget(lbl)
    lay.addStretch(1)
    for b in buttons:
        lay.addWidget(b)
    return w


def tool_button(icon_name: str, tip: str, cb=None, size=18) -> QToolButton:
    b = QToolButton()
    icons.set(b, icon_name, size)
    b.setToolTip(tip)
    b.setAutoRaise(True)
    if cb:
        b.clicked.connect(cb)
    return b


class ThumbDelegate(QStyledItemDelegate):
    def __init__(self, panel):
        super().__init__(panel)
        self.panel = panel

    def sizeHint(self, option, index):
        return QSize(THUMB_W + 40, int(THUMB_W * 1.4) + 30)

    def paint(self, p: QPainter, option, index):
        tab = self.panel.tab
        if tab is None:
            return
        i = index.row()
        r = option.rect
        sel = bool(option.state & QStyle.StateFlag.State_Selected)
        c = theme.current
        w, h = tab.doc.page_size(i) if i < tab.doc.page_count else (612, 792)
        scale = min(THUMB_W / w, (r.height() - 30) / h)
        tw, th = w * scale, h * scale
        x = r.x() + (r.width() - tw) / 2
        y = r.y() + 6
        box = QRect(int(x), int(y), int(tw), int(th))
        if sel:
            p.fillRect(r.adjusted(4, 2, -4, -2), QColor(c["selection"]))
        p.fillRect(box.adjusted(1, 1, 2, 2), QColor(0, 0, 0, 40))
        p.fillRect(box, Qt.GlobalColor.white)
        img = self.panel.thumb(i)
        if img is not None:
            p.setRenderHint(QPainter.RenderHint.SmoothPixmapTransform)
            p.drawImage(box, img)
        if i == tab.view.current_page:
            p.setPen(QPen(QColor(c["accent"]), 2))
            p.drawRect(box.adjusted(-1, -1, 1, 1))
        else:
            p.setPen(QPen(QColor(c["border"]), 1))
            p.drawRect(box)
        p.setPen(QColor(c["text"]))
        p.drawText(QRect(r.x(), int(y + th + 4), r.width(), 18), Qt.AlignmentFlag.AlignHCenter, str(i + 1))
        # drop indicator
        if self.panel.drop_row is not None:
            if self.panel.drop_row == i:
                p.fillRect(QRect(r.x() + 6, r.y(), r.width() - 12, 3), QColor(c["accent"]))
            elif self.panel.drop_row == i + 1 and i == index.model().rowCount() - 1:
                p.fillRect(QRect(r.x() + 6, r.bottom() - 2, r.width() - 12, 3), QColor(c["accent"]))


class ThumbList(QListWidget):
    def __init__(self, panel):
        super().__init__()
        self.panel = panel
        self.setSelectionMode(QAbstractItemView.SelectionMode.ExtendedSelection)
        self.setDragEnabled(True)
        self.setAcceptDrops(True)
        self.setDropIndicatorShown(False)
        self.setDragDropMode(QAbstractItemView.DragDropMode.DragDrop)
        self.setUniformItemSizes(True)
        self.setVerticalScrollMode(QAbstractItemView.ScrollMode.ScrollPerPixel)

    def startDrag(self, actions):
        rows = sorted(i.row() for i in self.selectedIndexes())
        if not rows:
            return
        md = QMimeData()
        md.setData("application/x-docket-pages", ",".join(map(str, rows)).encode())
        drag = QDrag(self)
        drag.setMimeData(md)
        img = self.panel.thumb(rows[0])
        if img is not None:
            pm = QPixmap.fromImage(img.scaledToWidth(70, Qt.TransformationMode.SmoothTransformation))
            drag.setPixmap(pm)
        drag.exec(Qt.DropAction.MoveAction)
        self.panel.drop_row = None
        self.viewport().update()

    def _drop_row(self, pos) -> int:
        idx = self.indexAt(pos)
        if not idx.isValid():
            return self.count()
        r = self.visualRect(idx)
        return idx.row() + (1 if pos.y() > r.center().y() else 0)

    def dragEnterEvent(self, ev):
        md = ev.mimeData()
        if md.hasFormat("application/x-docket-pages") or self._files(md):
            ev.acceptProposedAction()
        else:
            ev.ignore()

    def dragMoveEvent(self, ev):
        md = ev.mimeData()
        if md.hasFormat("application/x-docket-pages") or self._files(md):
            self.panel.drop_row = self._drop_row(ev.position().toPoint())
            self.viewport().update()
            ev.setDropAction(Qt.DropAction.MoveAction if md.hasFormat("application/x-docket-pages")
                             else Qt.DropAction.CopyAction)
            ev.accept()
        else:
            ev.ignore()

    def dragLeaveEvent(self, ev):
        self.panel.drop_row = None
        self.viewport().update()

    @staticmethod
    def _files(md) -> list[str]:
        if not md.hasUrls():
            return []
        out = []
        for u in md.urls():
            p = u.toLocalFile()
            if p and (p.lower().endswith(".pdf") or os.path.splitext(p)[1].lower() in IMAGE_EXTS):
                out.append(p)
        return out

    def dropEvent(self, ev):
        md = ev.mimeData()
        row = self._drop_row(ev.position().toPoint())
        self.panel.drop_row = None
        if md.hasFormat("application/x-docket-pages"):
            rows = [int(x) for x in bytes(md.data("application/x-docket-pages")).decode().split(",") if x]
            ev.setDropAction(Qt.DropAction.IgnoreAction)
            ev.accept()
            self.panel.move_requested.emit(rows, row)
        else:
            files = self._files(md)
            if files:
                ev.acceptProposedAction()
                self.panel.insert_files_requested.emit(files, row)
        self.viewport().update()

    def keyPressEvent(self, ev):
        if ev.key() == Qt.Key.Key_Delete:
            self.panel.delete_requested.emit()
            return
        super().keyPressEvent(ev)


class ThumbnailPanel(QFrame):
    page_clicked = Signal(int)
    move_requested = Signal(list, int)
    insert_files_requested = Signal(list, int)
    delete_requested = Signal()
    context_requested = Signal(QPoint)

    def __init__(self, renderer, parent=None):
        super().__init__(parent)
        self.renderer = renderer
        self.tab = None
        self.drop_row = None
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.setSpacing(0)
        lay.addWidget(pane_header("Pages"))
        self.list = ThumbList(self)
        self.list.setItemDelegate(ThumbDelegate(self))
        self.list.itemClicked.connect(lambda it: self.page_clicked.emit(self.list.row(it)))
        self.list.setContextMenuPolicy(Qt.ContextMenuPolicy.CustomContextMenu)
        self.list.customContextMenuRequested.connect(lambda p: self.context_requested.emit(self.list.mapToGlobal(p)))
        self.list.verticalScrollBar().valueChanged.connect(lambda _: self._request())
        lay.addWidget(self.list, 1)
        renderer.image_ready.connect(self._image_ready)
        self._timer = QTimer(self)
        self._timer.setSingleShot(True)
        self._timer.setInterval(30)
        self._timer.timeout.connect(self._request)

    def set_tab(self, tab):
        self.tab = tab
        self.refresh()

    def refresh(self):
        n = self.tab.doc.page_count if self.tab else 0
        sel = self.selected_pages() if self.list.count() == n else []
        self.list.blockSignals(True)
        if self.list.count() != n:
            self.list.clear()
            for _ in range(n):
                QListWidgetItem(self.list)
        for r in sel:
            if r < n:
                self.list.item(r).setSelected(True)
        self.list.blockSignals(False)
        self.list.viewport().update()
        self._timer.start()

    def current_changed(self, i):
        if self.tab is None or i >= self.list.count():
            return
        it = self.list.item(i)
        if it and not it.isSelected() and len(self.selected_pages()) <= 1:
            self.list.blockSignals(True)
            self.list.setCurrentItem(it)
            self.list.blockSignals(False)
        self.list.scrollToItem(it, QAbstractItemView.ScrollHint.EnsureVisible)
        self.list.viewport().update()

    def selected_pages(self) -> list[int]:
        return sorted(self.list.row(i) for i in self.list.selectedItems())

    def _key(self, i):
        return ("p", id(self.tab), self.tab.doc.page_ids[i])

    def thumb(self, i):
        if self.tab is None or i >= self.tab.doc.page_count:
            return None
        return self.renderer.thumbs.get(self._key(i))

    def _image_ready(self, key, img):
        if self.tab is not None and isinstance(key, tuple) and key[0] == "p" and key[1] == id(self.tab):
            self.list.viewport().update()

    def _request(self):
        if self.tab is None or not self.isVisible() or self.list.count() == 0:
            return
        first = self.list.indexAt(QPoint(5, 5)).row()
        last = self.list.indexAt(QPoint(5, self.list.viewport().height() - 5)).row()
        if first < 0:
            first = 0
        if last < 0:
            last = min(self.list.count() - 1, first + 12)
        reqs = []
        doc = self.tab.doc
        for i in range(first, min(last + 3, doc.page_count)):
            key = self._key(i)
            if key in self.renderer.thumbs or self.renderer.side.is_pending(key):
                continue
            w, h = doc.page_size(i)
            reqs.append(Request("thumb", id(self.tab), doc.version, i, min(1.0, 500 / max(w, h)), None, key))
        if reqs:
            self.renderer.set_document(id(self.tab), doc.version, doc.tobytes)
            self.renderer.side.want(reqs, keep_others=True)

    def showEvent(self, ev):
        super().showEvent(ev)
        self._timer.start()

    def resizeEvent(self, ev):
        super().resizeEvent(ev)
        self._timer.start()


class BookmarkPanel(QFrame):
    go = Signal(int, object)
    edit_requested = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self.tab = None
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.setSpacing(0)
        self.add_btn = tool_button("plus", "Add bookmark for the current page", lambda: self.edit_requested.emit())
        lay.addWidget(pane_header("Bookmarks", self.add_btn))
        self.tree = QTreeWidget()
        self.tree.setHeaderHidden(True)
        self.tree.itemClicked.connect(self._clicked)
        self.tree.itemActivated.connect(self._clicked)
        self.tree.setContextMenuPolicy(Qt.ContextMenuPolicy.CustomContextMenu)
        self.tree.customContextMenuRequested.connect(self._menu)
        lay.addWidget(self.tree, 1)
        self.empty = QLabel("This document has no bookmarks.\nUse + to add one for the current page.")
        self.empty.setObjectName("muted")
        self.empty.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.empty.setWordWrap(True)
        lay.addWidget(self.empty)
        self.items: list[M.Bookmark] = []

    def set_tab(self, tab):
        self.tab = tab
        self.refresh()

    def refresh(self):
        self.tree.clear()
        self.items = M.bookmarks(self.tab.doc) if self.tab else []
        stack = [(0, self.tree.invisibleRootItem())]
        for k, b in enumerate(self.items):
            while stack and stack[-1][0] >= b.level:
                stack.pop()
            parent = stack[-1][1] if stack else self.tree.invisibleRootItem()
            it = QTreeWidgetItem(parent, [b.title])
            it.setData(0, Qt.ItemDataRole.UserRole, k)
            stack.append((b.level, it))
        self.tree.expandToDepth(0)
        self.empty.setVisible(not self.items)
        self.tree.setVisible(bool(self.items))

    def _clicked(self, it, _col=0):
        k = it.data(0, Qt.ItemDataRole.UserRole)
        b = self.items[k]
        if b.page >= 0:
            self.go.emit(b.page, b.y)

    def _menu(self, pos):
        it = self.tree.itemAt(pos)
        if not it or self.tab is None:
            return
        k = it.data(0, Qt.ItemDataRole.UserRole)
        m = QMenu(self)
        m.addAction("Rename...", lambda: self._rename(k))
        m.addAction("Delete", lambda: self._delete(k))
        m.exec(self.tree.viewport().mapToGlobal(pos))

    def _rename(self, k):
        from PySide6.QtWidgets import QInputDialog

        b = self.items[k]
        text, ok = QInputDialog.getText(self, "Rename Bookmark", "Title:", text=b.title)
        if ok and text.strip():
            items = list(self.items)
            items[k] = M.Bookmark(b.level, text.strip(), b.page, b.y)
            self.tab.run(M.set_bookmarks, self.tab.doc, items, "Rename Bookmark")

    def _delete(self, k):
        items = list(self.items)
        lvl = items[k].level
        end = k + 1
        while end < len(items) and items[end].level > lvl:
            end += 1
        del items[k:end]
        self.tab.run(M.set_bookmarks, self.tab.doc, items, "Delete Bookmark")


class CommentsPanel(QFrame):
    go = Signal(object)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.tab = None
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.setSpacing(0)
        lay.addWidget(pane_header("Comments"))
        self.list = QListWidget()
        self.list.setWordWrap(True)
        self.list.setSpacing(2)
        self.list.itemClicked.connect(self._clicked)
        self.list.setContextMenuPolicy(Qt.ContextMenuPolicy.CustomContextMenu)
        self.list.customContextMenuRequested.connect(self._menu)
        lay.addWidget(self.list, 1)
        self.empty = QLabel("No comments yet.\nUse Comment in All tools to add highlights, notes and drawings.")
        self.empty.setObjectName("muted")
        self.empty.setWordWrap(True)
        self.empty.setAlignment(Qt.AlignmentFlag.AlignCenter)
        lay.addWidget(self.empty)
        self.infos = []

    def set_tab(self, tab):
        self.tab = tab
        self.refresh()

    def refresh(self):
        if not self.isVisible() and self.tab is not None:
            self._stale = True
            return
        self._stale = False
        self.list.clear()
        self.infos = [a for a in A.list_all(self.tab.doc)] if self.tab else []
        self.infos = [a for a in self.infos if a.type != A.REDACT]
        for a in self.infos:
            who = a.author or "Unknown"
            body = a.content.strip() if a.content else ""
            if a.is_signature:
                label, body = "Signature", ""
            elif a.is_plain_text:
                label = "Text"
            else:
                label = a.type_name
            text = f"{label}  ·  page {a.page + 1}\n{who}"
            if body:
                text += f"\n{body[:300]}"
            it = QListWidgetItem(icons.color_swatch(QColor.fromRgbF(*(a.color or (0.5, 0.5, 0.5))[:3]), 12), text)
            self.list.addItem(it)
        self.empty.setVisible(not self.infos)
        self.list.setVisible(bool(self.infos))

    def showEvent(self, ev):
        super().showEvent(ev)
        if getattr(self, "_stale", False):
            self.refresh()

    def _clicked(self, it):
        a = self.infos[self.list.row(it)]
        self.go.emit(a)

    def _menu(self, pos):
        it = self.list.itemAt(pos)
        if not it or self.tab is None:
            return
        a = self.infos[self.list.row(it)]
        m = QMenu(self)
        m.addAction("Go To", lambda: self.go.emit(a))
        m.addAction("Edit Text...", lambda: (self.go.emit(a), self.tab.edit_annot_text(a)))
        m.addAction("Delete", lambda: self.tab.delete_annot(a))
        m.exec(self.list.viewport().mapToGlobal(pos))
