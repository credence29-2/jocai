"""Acrobat-style "All tools" pane: a list of tool groups that opens into the
tools for one group, with style controls for the comment tools."""

from __future__ import annotations

from PySide6.QtCore import Qt, Signal
from PySide6.QtGui import QColor
from PySide6.QtWidgets import (QColorDialog, QComboBox, QFrame, QGridLayout, QHBoxLayout, QLabel,
                               QMenu, QPushButton, QScrollArea, QSizePolicy, QSpinBox,
                               QStackedWidget, QToolButton, QVBoxLayout, QWidget)

from . import icons

GROUPS = [
    ("comment", "Comment", "comment_tool", [
        "Text markup", "tool_highlight", "tool_underline", "tool_strike",
        "Notes and text", "tool_note", "tool_textbox", "tool_addtext",
        "Drawing", "tool_pen", "tool_rect", "tool_oval", "tool_arrow", "tool_line",
        "Stamps", "stamp_menu",
        "@style"]),
    ("fillsign", "Fill & Sign", "sign", [
        "Fill in", "tool_addtext", "tool_select",
        "Sign", "sign_menu", "create_signature"]),
    ("organize", "Organize Pages", "pages", [
        "Pages", "rotate_left", "rotate_right", "delete_pages", "insert_files", "insert_blank",
        "Split and extract", "extract_pages", "split", "export_images",
        "Bookmarks", "add_bookmark"]),
    ("combine", "Combine Files", "merge", ["Combine", "merge"]),
    ("create", "Create a PDF", "new", ["Create", "new", "images_to_pdf"]),
    ("form", "Prepare a Form", "form", [
        "Add fields", "tool_field_text", "tool_field_check", "tool_field_date",
        "Automatic", "detect_fields",
        "Finish", "flatten", "highlight_fields"]),
    ("redact", "Redact a PDF", "redact", [
        "Mark", "tool_redact", "find_redact",
        "Apply", "apply_redactions", "clear_redactions"]),
    ("protect", "Protect a PDF", "shield", ["Passwords", "protect", "remove_password"]),
    ("compress", "Compress a PDF", "compress", ["Reduce size", "compress"]),
    ("export", "Export PDF", "export", ["Export", "export_images", "extract_pages"]),
]

STYLE_TOOLS = {"highlight", "underline", "strike", "note", "textbox", "addtext", "pen", "rect", "oval",
               "arrow", "line"}

SWATCHES = ["#ffd400", "#ff8a00", "#d32f2f", "#e91e63", "#7b1fa2", "#1565c0", "#00897b", "#1a8f2e",
            "#000000", "#616161"]


class StyleBox(QFrame):
    """Colour / width / font size for the active comment tool or selected comment."""

    color_picked = Signal(str)
    width_picked = Signal(float)
    size_picked = Signal(float)

    def __init__(self, parent=None):
        super().__init__(parent)
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 6, 0, 0)
        self.title = QLabel("Style")
        self.title.setObjectName("muted")
        lay.addWidget(self.title)
        grid = QGridLayout()
        grid.setSpacing(4)
        for k, col in enumerate(SWATCHES):
            b = QToolButton()
            b.setIcon(icons.color_swatch(QColor(col), 18))
            b.setAutoRaise(True)
            b.setToolTip(col)
            b.clicked.connect(lambda _=False, c=col: self.color_picked.emit(c))
            grid.addWidget(b, k // 5, k % 5)
        more = QToolButton()
        more.setText("...")
        more.setToolTip("More colours")
        more.clicked.connect(self._more)
        grid.addWidget(more, 2, 0)
        lay.addLayout(grid)
        row = QHBoxLayout()
        row.addWidget(QLabel("Line"))
        self.width = QSpinBox()
        self.width.setRange(1, 20)
        self.width.setSuffix(" pt")
        self.width.valueChanged.connect(lambda v: self._emit(self.width_picked, float(v)))
        row.addWidget(self.width)
        row.addWidget(QLabel("Font"))
        self.size = QSpinBox()
        self.size.setRange(6, 72)
        self.size.setSuffix(" pt")
        self.size.valueChanged.connect(lambda v: self._emit(self.size_picked, float(v)))
        row.addWidget(self.size)
        lay.addLayout(row)
        self._quiet = False
        self.current = "#000000"

    def _emit(self, sig, v):
        if not self._quiet:
            sig.emit(v)

    def _more(self):
        c = QColorDialog.getColor(QColor(self.current), self, "Colour")
        if c.isValid():
            self.color_picked.emit(c.name())

    def show_values(self, title, color, width, size, has_width=True, has_size=True):
        self._quiet = True
        self.title.setText(title)
        self.current = color
        self.width.setValue(int(round(width or 1)))
        self.size.setValue(int(round(size or 12)))
        self.width.setEnabled(has_width)
        self.size.setEnabled(has_size)
        self._quiet = False


class ToolsPane(QFrame):
    closed = Signal()
    group_opened = Signal(str)

    def __init__(self, actions: dict, menus: dict, parent=None):
        super().__init__(parent)
        self.setObjectName("toolsPane")
        self.actions = actions
        self.menus = menus
        self.setMinimumWidth(230)
        self.setMaximumWidth(300)
        self.stack = QStackedWidget()
        lay = QVBoxLayout(self)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.addWidget(self.stack)
        self.style_box = StyleBox()
        self.pages: dict[str, int] = {}
        self.current_group = None
        self.stack.addWidget(self._home())
        for key, title, icon_name, items in GROUPS:
            self.pages[key] = self.stack.count()
            self.stack.addWidget(self._group(key, title, icon_name, items))

    def _header(self, title, back=False):
        w = QWidget()
        h = QHBoxLayout(w)
        h.setContentsMargins(8, 8, 4, 4)
        if back:
            b = QToolButton()
            icons.set(b, "chev_left", 18)
            b.setAutoRaise(True)
            b.setToolTip("All tools")
            b.clicked.connect(self.show_home)
            h.addWidget(b)
        lbl = QLabel(title)
        lbl.setObjectName("paneTitle")
        h.addWidget(lbl, 1)
        close = QToolButton()
        icons.set(close, "close", 16)
        close.setAutoRaise(True)
        close.setToolTip("Hide tools (Shift+F4)")
        close.clicked.connect(self.closed)
        h.addWidget(close)
        return w

    def _scroll(self, inner):
        sa = QScrollArea()
        sa.setWidgetResizable(True)
        sa.setFrameShape(QFrame.Shape.NoFrame)
        sa.setStyleSheet("QScrollArea { background: transparent; }")
        inner.setObjectName("paneInner")
        sa.setWidget(inner)
        sa.viewport().setAutoFillBackground(False)
        return sa

    def _home(self):
        w = QWidget()
        lay = QVBoxLayout(w)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.addWidget(self._header("All tools"))
        inner = QWidget()
        il = QVBoxLayout(inner)
        il.setContentsMargins(8, 0, 8, 8)
        il.setSpacing(1)
        for key, title, icon_name, _ in GROUPS:
            b = QPushButton("  " + title.replace("&", "&&"))
            icons.set(b, icon_name, 20)
            b.setObjectName("toolEntry")
            b.clicked.connect(lambda _=False, k=key: self.open_group(k))
            il.addWidget(b)
        il.addStretch(1)
        lay.addWidget(self._scroll(inner), 1)
        return w

    def _group(self, key, title, icon_name, items):
        w = QWidget()
        lay = QVBoxLayout(w)
        lay.setContentsMargins(0, 0, 0, 0)
        lay.addWidget(self._header(title, back=True))
        inner = QWidget()
        il = QVBoxLayout(inner)
        il.setContentsMargins(8, 0, 8, 8)
        il.setSpacing(1)
        for item in items:
            if item == "@style":
                holder = QWidget()
                holder.setObjectName("styleHolder")
                QVBoxLayout(holder).setContentsMargins(4, 0, 4, 0)
                il.addWidget(holder)
                self._style_holder = holder
                continue
            if item in self.menus:
                b = QToolButton()
                act = self.actions.get(item)
                if act is not None:
                    b.setDefaultAction(act)
                b.setMenu(self.menus[item])
                b.setPopupMode(QToolButton.ToolButtonPopupMode.InstantPopup)
                b.setToolButtonStyle(Qt.ToolButtonStyle.ToolButtonTextBesideIcon)
                b.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)
                b.setStyleSheet("QToolButton { text-align: left; padding: 6px 8px; }")
                il.addWidget(b)
                continue
            act = self.actions.get(item)
            if act is None:
                lbl = QLabel(item.replace("&", "&&"))
                lbl.setObjectName("muted")
                lbl.setContentsMargins(4, 10, 0, 2)
                il.addWidget(lbl)
                continue
            b = QToolButton()
            b.setDefaultAction(act)
            b.setToolButtonStyle(Qt.ToolButtonStyle.ToolButtonTextBesideIcon)
            b.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)
            b.setStyleSheet("QToolButton { text-align: left; padding: 6px 8px; }")
            il.addWidget(b)
        il.addStretch(1)
        lay.addWidget(self._scroll(inner), 1)
        return w

    def open_group(self, key):
        self.current_group = key
        self.stack.setCurrentIndex(self.pages[key])
        if key == "comment" and hasattr(self, "_style_holder"):
            self._style_holder.layout().addWidget(self.style_box)
        self.group_opened.emit(key)

    def show_home(self):
        self.current_group = None
        self.stack.setCurrentIndex(0)
        self.group_opened.emit("")
