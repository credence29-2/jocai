"""Main window: Acrobat-style layout with tabs, All tools pane, quick tools, right rail."""

from __future__ import annotations

import logging
import os
import sys

from PySide6.QtCore import QByteArray, QPoint, QSize, Qt, QTimer, QUrl
from PySide6.QtGui import (QAction, QActionGroup, QColor, QDesktopServices, QIcon, QImage, QKeySequence,
                           QPainter)
from PySide6.QtWidgets import (QApplication, QFileDialog, QFrame, QHBoxLayout, QInputDialog, QLabel,
                               QLineEdit, QListWidget, QListWidgetItem, QMainWindow, QMenu, QMessageBox,
                               QProgressDialog, QPushButton, QSizePolicy, QSplitter, QStackedWidget,
                               QTabWidget, QToolBar, QToolButton, QVBoxLayout, QWidget)

from .. import APP_NAME, __version__
from ..core import annots as A
from ..core import compress as C
from ..core import detect as D
from ..core import forms as F
from ..core import meta as M
from ..core import pages as P
from ..core import redact as R
from ..core import security as S
from ..core import sigstore
from ..core.document import Document, NotAPdf, PasswordRequired
from ..paths import log_dir
from . import dialogs as dlg
from . import icons, settings, theme
from .docview import ANNOT_TOOL, DocumentTab, ToolStyles, hexcolor
from .panels import BookmarkPanel, CommentsPanel, ThumbnailPanel
from .renderer import Renderer
from .toolspane import STYLE_TOOLS, ToolsPane

log = logging.getLogger(__name__)

TOOL_LABELS = {
    "select": ("Select", "select", "V"), "hand": ("Hand", "hand", "H"),
    "highlight": ("Highlight", "highlight", None), "underline": ("Underline", "underline", None),
    "strike": ("Strikethrough", "strike", None), "note": ("Sticky Note", "note", None),
    "textbox": ("Text Box", "textbox", None), "addtext": ("Add Text", "addtext", None),
    "pen": ("Draw", "pen", None), "rect": ("Rectangle", "rect", None), "oval": ("Oval", "oval", None),
    "arrow": ("Arrow", "arrow", None), "line": ("Line", "line", None),
    "redact": ("Mark for Redaction", "redact", None),
    "field_text": ("Add Text Field", "field", None), "field_check": ("Add Checkbox", "checkbox", None),
    "field_date": ("Add Date Field", "date", None),
}

TOOL_HINTS = {
    "highlight": "Drag over text to highlight it. The tool stays on until you press Esc.",
    "underline": "Drag over text to underline it. Esc to stop.",
    "strike": "Drag over text to strike it through. Esc to stop.",
    "note": "Click where the note goes.",
    "textbox": "Drag a box (or click), then type.",
    "addtext": "Click anywhere and type. Esc to stop.",
    "pen": "Draw with the mouse. Each stroke is separate. Esc to stop.",
    "rect": "Drag to draw a rectangle.", "oval": "Drag to draw an oval.",
    "arrow": "Drag to draw an arrow.", "line": "Drag to draw a line.",
    "redact": "Select text or drag boxes over anything to remove. Marks are applied with Apply Redactions. Esc to stop.",
    "field_text": "Drag a box for the text field, or click for a default size.",
    "field_check": "Click where the checkbox goes, or drag to size it.",
    "field_date": "Drag a box for the date field, or click for a default size.",
    "sign": "Click to place your signature, or drag to size it. Esc to cancel.",
    "stamp": "Click to place the stamp, or drag to size it. Esc to cancel.",
    "hand": "Drag to move around the page.",
}


class Welcome(QWidget):
    def __init__(self, win):
        super().__init__()
        self.win = win
        lay = QVBoxLayout(self)
        lay.setContentsMargins(60, 50, 60, 40)
        title = QLabel(f"<span style='font-size:26px;font-weight:600'>{APP_NAME}</span>")
        lay.addWidget(title)
        sub = QLabel("View, comment, fill, sign, organize and protect PDFs.")
        sub.setObjectName("muted")
        lay.addWidget(sub)
        row = QHBoxLayout()
        for text, ic, act in (("Open File", "open", win.act["open"]), ("New PDF", "new", win.act["new"]),
                              ("Combine Files", "merge", win.act["merge"]),
                              ("Images to PDF", "image", win.act["images_to_pdf"])):
            b = QPushButton(" " + text)
            icons.set(b, ic, 18)
            b.clicked.connect(act.trigger)
            if text == "Open File":
                b.setObjectName("primary")
            row.addWidget(b)
        row.addStretch(1)
        lay.addSpacing(16)
        lay.addLayout(row)
        lay.addSpacing(24)
        lbl = QLabel("Recent files")
        lbl.setObjectName("paneTitle")
        lay.addWidget(lbl)
        self.recent = QListWidget()
        self.recent.itemActivated.connect(lambda it: win.open_paths([it.data(Qt.ItemDataRole.UserRole)]))
        self.recent.itemClicked.connect(lambda it: win.open_paths([it.data(Qt.ItemDataRole.UserRole)]))
        lay.addWidget(self.recent, 1)
        hint = QLabel("Tip: drag PDFs or images onto this window to open them.")
        hint.setObjectName("muted")
        lay.addWidget(hint)
        self.refresh()

    def refresh(self):
        self.recent.clear()
        for p in settings.recent():
            it = QListWidgetItem(icons.icon("file", 16), f"{os.path.basename(p)}    ·    {os.path.dirname(p)}")
            it.setData(Qt.ItemDataRole.UserRole, p)
            if not os.path.exists(p):
                it.setForeground(QColor(theme.current["disabled"]))
            self.recent.addItem(it)


class FindBar(QFrame):
    def __init__(self, win):
        super().__init__()
        self.win = win
        self.setObjectName("findBar")
        lay = QHBoxLayout(self)
        lay.setContentsMargins(6, 4, 4, 4)
        lay.setSpacing(2)
        ic = QLabel()
        ic.setProperty("docketIcon", "search:16")
        ic.setPixmap(icons.icon("search", 16).pixmap(16, 16))
        lay.addWidget(ic)
        self.edit = QLineEdit()
        self.edit.setPlaceholderText("Find text")
        self.edit.setMinimumWidth(200)
        self.edit.setFrame(False)
        self.edit.returnPressed.connect(self._enter)
        self.edit.textChanged.connect(lambda _: self._timer.start())
        lay.addWidget(self.edit)
        self.count = QLabel("")
        self.count.setObjectName("muted")
        self.count.setMinimumWidth(70)
        lay.addWidget(self.count)
        self.case = QToolButton()
        self.case.setText("Aa")
        self.case.setCheckable(True)
        self.case.setToolTip("Match case")
        self.case.toggled.connect(lambda _: self._run())
        lay.addWidget(self.case)
        for name, tip, step in (("chev_up", "Previous (Shift+Enter)", -1), ("chev_down", "Next (Enter)", 1)):
            b = QToolButton()
            icons.set(b, name, 16)
            b.setToolTip(tip)
            b.setAutoRaise(True)
            b.clicked.connect(lambda _=False, s=step: self.win.find_step(s))
            lay.addWidget(b)
        close = QToolButton()
        icons.set(close, "close", 14)
        close.setAutoRaise(True)
        close.clicked.connect(self.close_bar)
        lay.addWidget(close)
        self._timer = QTimer(self)
        self._timer.setSingleShot(True)
        self._timer.setInterval(250)
        self._timer.timeout.connect(self._run)
        self.hide()

    def keyPressEvent(self, ev):
        if ev.key() == Qt.Key.Key_Escape:
            self.close_bar()
            return
        super().keyPressEvent(ev)

    def _enter(self):
        tab = self.win.tab()
        if tab is None:
            return
        if tab.search_query != self.edit.text() or tab.search_case != self.case.isChecked():
            self._run()
        else:
            step = -1 if QApplication.keyboardModifiers() & Qt.KeyboardModifier.ShiftModifier else 1
            self.win.find_step(step)

    def _run(self):
        self._timer.stop()
        tab = self.win.tab()
        if tab is not None:
            tab.find(self.edit.text(), self.case.isChecked())

    def open_bar(self):
        self.show()
        self.raise_()
        tab = self.win.tab()
        if tab is not None and tab.view.has_selection():
            sel = tab.view.selected_text().strip()
            if sel and "\n" not in sel and len(sel) < 100:
                self.edit.setText(sel)
        self.edit.setFocus()
        self.edit.selectAll()
        self.win.position_overlays()

    def close_bar(self):
        self.hide()
        tab = self.win.tab()
        if tab is not None:
            tab.clear_search()
            tab.view.setFocus()

    def update_status(self, tab):
        if tab is None or not tab.search_query:
            self.count.setText("")
            return
        n = tab.hit_count()
        k = tab.hit_index()
        if not tab.search_done:
            done, total = tab.search_progress
            self.count.setText(f"{n} found..." if n else "Searching...")
        elif n == 0:
            self.count.setText("No results")
        else:
            self.count.setText(f"{k + 1} of {n}" if k >= 0 else f"{n} found")


class FloatBar(QFrame):
    """Floating page navigation and zoom controls, bottom right of the document."""

    def __init__(self, win):
        super().__init__()
        self.win = win
        self.setObjectName("floatBar")
        lay = QVBoxLayout(self)
        lay.setContentsMargins(3, 4, 3, 4)
        lay.setSpacing(2)

        def btn(name, tip, act):
            b = QToolButton()
            icons.set(b, name, 18)
            b.setToolTip(tip)
            b.setAutoRaise(True)
            b.clicked.connect(act.trigger)
            lay.addWidget(b, 0, Qt.AlignmentFlag.AlignHCenter)
            return b

        btn("chev_up", "Previous page", win.act["prev_page"])
        self.page = QLineEdit()
        self.page.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.page.setFixedWidth(40)
        self.page.returnPressed.connect(self._go)
        lay.addWidget(self.page, 0, Qt.AlignmentFlag.AlignHCenter)
        self.total = QLabel("/ 0")
        self.total.setObjectName("muted")
        self.total.setAlignment(Qt.AlignmentFlag.AlignCenter)
        lay.addWidget(self.total)
        btn("chev_down", "Next page", win.act["next_page"])
        line = QFrame()
        line.setFrameShape(QFrame.Shape.HLine)
        line.setStyleSheet(f"color: {theme.current['border']}")
        lay.addWidget(line)
        btn("zoom_in", "Zoom in (Ctrl+=)", win.act["zoom_in"])
        self.zoom = QLineEdit()
        self.zoom.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.zoom.setFixedWidth(48)
        self.zoom.returnPressed.connect(self._zoom)
        lay.addWidget(self.zoom, 0, Qt.AlignmentFlag.AlignHCenter)
        btn("zoom_out", "Zoom out (Ctrl+-)", win.act["zoom_out"])
        fit = QToolButton()
        icons.set(fit, "fit_page", 18)
        fit.setToolTip("Page fit")
        fit.setAutoRaise(True)
        m = QMenu(fit)
        for k in ("fit_page", "fit_width", "actual_size"):
            m.addAction(win.act[k])
        fit.setMenu(m)
        fit.setPopupMode(QToolButton.ToolButtonPopupMode.InstantPopup)
        lay.addWidget(fit, 0, Qt.AlignmentFlag.AlignHCenter)

    def _go(self):
        tab = self.win.tab()
        try:
            n = int(self.page.text())
        except ValueError:
            n = 0
        if tab and 1 <= n <= tab.doc.page_count:
            tab.view.go_to_page(n - 1)
            tab.view.setFocus()
        self.update_state(tab)

    def _zoom(self):
        tab = self.win.tab()
        t = self.zoom.text().strip().rstrip("%")
        try:
            v = float(t) / 100
        except ValueError:
            v = 0
        if tab and v > 0:
            tab.view.set_zoom(v)
            tab.view.setFocus()
        self.update_state(tab)

    def update_state(self, tab):
        if tab is None:
            return
        if not self.page.hasFocus():
            self.page.setText(str(tab.view.current_page + 1))
        self.total.setText(f"/ {tab.doc.page_count}")
        if not self.zoom.hasFocus():
            self.zoom.setText(f"{tab.view.zoom * 100:.0f}%")


class ContextBar(QFrame):
    """Strip above the document with a hint and buttons for the active task."""

    def __init__(self):
        super().__init__()
        self.setObjectName("contextBar")
        self.lay = QHBoxLayout(self)
        self.lay.setContentsMargins(12, 5, 8, 5)
        self.label = QLabel()
        self.label.setWordWrap(True)
        self.lay.addWidget(self.label, 1)
        self.buttons: list[QPushButton] = []
        self.hide()

    def show_hint(self, text, buttons=()):
        for b in self.buttons:
            b.deleteLater()
        self.buttons = []
        self.label.setText(text)
        for label, cb, primary in buttons:
            b = QPushButton(label)
            if primary:
                b.setObjectName("primary")
            b.clicked.connect(cb)
            self.lay.addWidget(b)
            self.buttons.append(b)
        self.setVisible(bool(text))


class MainWindow(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle(APP_NAME)
        self.setAcceptDrops(True)
        self.renderer = Renderer(self)
        self.styles = ToolStyles()
        self.act: dict[str, QAction] = {}
        self.menus: dict[str, QMenu] = {}
        self._make_actions()
        self._make_ui()
        self._make_menus()
        self._restore()
        self._update_actions()
        self._print_progress = None

    # ===================================================================== setup

    def a(self, key, text, slot=None, shortcut=None, icon=None, checkable=False, tip=None):
        act = QAction(text, self)
        if icon:
            icons.set(act, icon)
        if shortcut:
            if isinstance(shortcut, (list, tuple)):
                act.setShortcuts([QKeySequence(s) for s in shortcut])
            else:
                act.setShortcut(QKeySequence(shortcut))
            act.setShortcutContext(Qt.ShortcutContext.WindowShortcut)
        act.setCheckable(checkable)
        if slot:
            act.triggered.connect(slot)
        tip = tip or text.replace("...", "").replace("&", "")
        sc = act.shortcut().toString(QKeySequence.SequenceFormat.NativeText)
        act.setToolTip(f"{tip} ({sc})" if sc else tip)
        self.addAction(act)
        self.act[key] = act
        return act

    def _make_actions(self):
        a = self.a
        a("new", "&New PDF...", self.new_document, "Ctrl+N", "new")
        a("open", "&Open...", self.open_dialog, "Ctrl+O", "open")
        a("save", "&Save", self.save, "Ctrl+S", "save")
        a("save_as", "Save &As...", self.save_as, "Ctrl+Shift+S")
        a("print", "&Print...", self.print_doc, "Ctrl+P", "print")
        a("properties", "Document P&roperties...", self.properties, "Ctrl+D", "info")
        a("close", "&Close", self.close_current, "Ctrl+W")
        a("exit", "E&xit", self.close, "Ctrl+Q")
        a("compress", "Reduce File Size...", self.compress, None, "compress")
        a("undo", "&Undo", lambda: self._doc_call("undo"), "Ctrl+Z", "undo")
        a("redo", "&Redo", lambda: self._doc_call("redo"), ["Ctrl+Y", "Ctrl+Shift+Z"], "redo")
        a("copy", "&Copy", self.copy, "Ctrl+C")
        a("select_all", "Select &All", lambda: self.tab() and self.tab().view.select_all(), "Ctrl+A")
        a("find", "&Find...", self.find_bar_open, "Ctrl+F", "search")
        a("find_next", "Find Next", lambda: self.find_step(1), "F3")
        a("find_prev", "Find Previous", lambda: self.find_step(-1), "Shift+F3")
        a("delete", "&Delete", self.delete_selected, None, "delete")
        a("prefs", "Pr&eferences...", self.preferences, "Ctrl+K")
        a("zoom_in", "Zoom &In", lambda: self._view_call("zoom_step", 1), ["Ctrl+=", "Ctrl++"], "zoom_in")
        a("zoom_out", "Zoom &Out", lambda: self._view_call("zoom_step", -1), "Ctrl+-", "zoom_out")
        a("fit_page", "Zoom to &Page Level", lambda: self._view_call("set_fit", "page"), "Ctrl+0", "fit_page")
        a("actual_size", "&Actual Size", lambda: self._view_call("set_zoom", 1.0), "Ctrl+1")
        a("fit_width", "Fit &Width", lambda: self._view_call("set_fit", "width"), "Ctrl+2", "fit_width")
        a("goto", "&Go to Page...", self.goto_page, "Ctrl+Shift+N")
        a("next_page", "Next Page", lambda: self._page_step(1), "Ctrl+Down")
        a("prev_page", "Previous Page", lambda: self._page_step(-1), "Ctrl+Up")
        a("first_page", "First Page", lambda: self._view_call("go_to_page", 0), "Ctrl+Home")
        a("last_page", "Last Page", lambda: self.tab() and self.tab().view.go_to_page(self.tab().doc.page_count - 1),
          "Ctrl+End")
        a("toggle_tools", "&All Tools Pane", self.toggle_tools_pane, "Shift+F4", "tools", checkable=True)
        a("toggle_right", "&Right Pane", self.toggle_right_pane, "F4", checkable=True)
        a("full_screen", "&Full Screen Mode", self.toggle_full_screen, "Ctrl+L")
        a("next_tab", "Next Tab", lambda: self._tab_step(1), "Ctrl+Tab")
        a("prev_tab", "Previous Tab", lambda: self._tab_step(-1), "Ctrl+Shift+Tab")
        a("open_logs", "Open Log Folder", lambda: QDesktopServices.openUrl(QUrl.fromLocalFile(log_dir())))
        a("about", f"About {APP_NAME}", self.about)
        # tools
        self.tool_group = QActionGroup(self)
        self.tool_group.setExclusive(True)
        for key, (label, ic, sc) in TOOL_LABELS.items():
            act = a("tool_" + key, label, lambda _=False, k=key: self.set_tool(k), sc, ic, checkable=True)
            self.tool_group.addAction(act)
        self.act["tool_select"].setChecked(True)
        a("sign_menu", "Sign Yourself", None, None, "sign")
        a("create_signature", "Create Signature...", self.create_signature, None, "plus")
        a("stamp_menu", "Stamp", None, None, "stamp")
        # pages
        a("rotate_left", "Rotate Left", lambda: self.rotate(-90), "Ctrl+Shift+-", "rotate_left")
        a("rotate_right", "Rotate Right", lambda: self.rotate(90), "Ctrl+Shift+=", "rotate_right")
        a("delete_pages", "Delete Pages...", self.delete_pages, "Ctrl+Shift+D", "delete")
        a("insert_files", "Insert from File...", self.insert_files, "Ctrl+Shift+I", "insert")
        a("insert_blank", "Insert Blank Page", self.insert_blank, None, "blank_page")
        a("extract_pages", "Extract Pages...", self.extract_pages, None, "extract")
        a("split", "Split Document...", self.split_doc, None, "split")
        a("merge", "Combine Files...", self.merge_files, None, "merge")
        a("images_to_pdf", "Images to PDF...", self.images_to_pdf, None, "image")
        a("export_images", "Export Pages as Images...", self.export_images, None, "export")
        a("add_bookmark", "Add Bookmark", self.add_bookmark, "Ctrl+B", "bookmark")
        # forms
        a("detect_fields", "Detect Form Fields", self.detect_fields, None, "wand")
        a("flatten", "Flatten Form...", self.flatten_form, None, "flatten")
        a("highlight_fields", "Highlight Fields", self.toggle_highlight_fields, None, "eye", checkable=True)
        self.act["highlight_fields"].setChecked(bool(settings.get("highlight_fields", True, bool)))
        # redact / protect
        a("find_redact", "Find and Redact...", self.find_and_redact, None, "search")
        a("apply_redactions", "Apply Redactions...", self.apply_redactions, None, "check")
        a("clear_redactions", "Remove Redaction Marks", self.clear_redactions, None, "close")
        a("protect", "Password Protect...", self.protect, None, "lock")
        a("remove_password", "Remove Password...", self.remove_password, None, "unlock")
        # theme
        self.theme_group = QActionGroup(self)
        for key, label in (("system", "System Setting"), ("light", "Light"), ("dark", "Dark")):
            act = a("theme_" + key, label, lambda _=False, k=key: self.set_theme(k), checkable=True)
            self.theme_group.addAction(act)
        self.act["theme_" + (settings.get("theme", "system") or "system")].setChecked(True)

    def _make_ui(self):
        # quick tools toolbar (top)
        tb = QToolBar("Quick tools")
        tb.setObjectName("quick")
        tb.setMovable(False)
        tb.setIconSize(QSize(20, 20))
        tb.setToolButtonStyle(Qt.ToolButtonStyle.ToolButtonIconOnly)
        for k in ("toggle_tools",):
            tb.addAction(self.act[k])
        tb.addSeparator()
        for k in ("open", "save", "print"):
            tb.addAction(self.act[k])
        tb.addSeparator()
        for k in ("undo", "redo"):
            tb.addAction(self.act[k])
        tb.addSeparator()
        for k in ("tool_select", "tool_hand"):
            tb.addAction(self.act[k])
        tb.addSeparator()
        for k in ("tool_note", "tool_highlight", "tool_pen", "tool_addtext"):
            tb.addAction(self.act[k])
        self.sign_btn = QToolButton()
        icons.set(self.sign_btn, "sign")
        self.sign_btn.setToolTip("Sign yourself")
        self.sign_btn.setPopupMode(QToolButton.ToolButtonPopupMode.InstantPopup)
        tb.addWidget(self.sign_btn)
        spacer = QWidget()
        spacer.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Preferred)
        tb.addWidget(spacer)
        tb.addAction(self.act["find"])
        self.addToolBar(Qt.ToolBarArea.TopToolBarArea, tb)
        self.quick_bar = tb

        # menus used by tool buttons
        self.sign_menu = QMenu(self)
        self.sign_menu.aboutToShow.connect(self._fill_sign_menu)
        self.sign_btn.setMenu(self.sign_menu)
        self.menus["sign_menu"] = self.sign_menu
        self.stamp_menu = QMenu(self)
        for k, name in enumerate(A.STAMPS):
            self.stamp_menu.addAction(name, lambda _=False, i=k: self.start_stamp(i))
        self.menus["stamp_menu"] = self.stamp_menu

        # tools pane (left)
        self.tools = ToolsPane(self.act, self.menus)
        self.tools.closed.connect(lambda: self.toggle_tools_pane(False))
        self.tools.group_opened.connect(self._group_opened)
        st = self.tools.style_box
        st.color_picked.connect(lambda c: self._style("color", c))
        st.width_picked.connect(lambda v: self._style("width", v))
        st.size_picked.connect(lambda v: self._style("size", v))

        # documents (centre)
        self.tabs = QTabWidget()
        self.tabs.setMovable(True)
        self.tabs.setDocumentMode(True)
        self.tabs.tabCloseRequested.connect(self.close_tab)
        self.tabs.currentChanged.connect(self._tab_changed)
        self.welcome = Welcome(self)
        self.center_stack = QStackedWidget()
        self.center_stack.addWidget(self.welcome)
        self.center_stack.addWidget(self.tabs)
        self.context_bar = ContextBar()
        center = QWidget()
        cl = QVBoxLayout(center)
        cl.setContentsMargins(0, 0, 0, 0)
        cl.setSpacing(0)
        cl.addWidget(self.context_bar)
        cl.addWidget(self.center_stack, 1)
        self.center = center
        self.find_bar = FindBar(self)
        self.find_bar.setParent(center)
        self.float_bar = FloatBar(self)
        self.float_bar.setParent(center)
        self.float_bar.hide()

        # right pane + rail
        self.thumbs = ThumbnailPanel(self.renderer)
        self.thumbs.page_clicked.connect(lambda i: self.tab() and self.tab().view.go_to_page(i))
        self.thumbs.move_requested.connect(self.move_pages)
        self.thumbs.insert_files_requested.connect(self.insert_files_at)
        self.thumbs.delete_requested.connect(self.delete_pages)
        self.thumbs.context_requested.connect(self._thumb_menu)
        self.bookmarks = BookmarkPanel()
        self.bookmarks.go.connect(lambda p, y: self.tab() and self.tab().view.go_to_page(p, y))
        self.bookmarks.edit_requested.connect(self.add_bookmark)
        self.comments = CommentsPanel()
        self.comments.go.connect(self._go_annot)
        self.right_stack = QStackedWidget()
        for w in (self.thumbs, self.bookmarks, self.comments):
            self.right_stack.addWidget(w)
        self.right_pane = QFrame()
        self.right_pane.setObjectName("rightPane")
        rl = QVBoxLayout(self.right_pane)
        rl.setContentsMargins(0, 0, 0, 0)
        rl.addWidget(self.right_stack)
        self.right_pane.setMinimumWidth(170)
        self.rail = QFrame()
        self.rail.setObjectName("rail")
        rail_l = QVBoxLayout(self.rail)
        rail_l.setContentsMargins(4, 8, 4, 8)
        rail_l.setSpacing(4)
        self.rail_buttons = []
        for k, (ic, tip) in enumerate((("thumbs", "Page thumbnails"), ("bookmark", "Bookmarks"),
                                       ("comments", "Comments"))):
            b = QToolButton()
            icons.set(b, ic, 20)
            b.setToolTip(tip)
            b.setCheckable(True)
            b.setAutoRaise(True)
            b.clicked.connect(lambda _=False, i=k: self.show_right(i, toggle=True))
            rail_l.addWidget(b)
            self.rail_buttons.append(b)
        rail_l.addStretch(1)

        self.split = QSplitter(Qt.Orientation.Horizontal)
        self.split.addWidget(self.tools)
        self.split.addWidget(center)
        self.split.addWidget(self.right_pane)
        self.split.setCollapsible(1, False)
        self.split.setStretchFactor(1, 1)
        self.split.setSizes([250, 900, 200])
        self.split.splitterMoved.connect(lambda *_: self.position_overlays())
        outer = QWidget()
        ol = QHBoxLayout(outer)
        ol.setContentsMargins(0, 0, 0, 0)
        ol.setSpacing(0)
        ol.addWidget(self.split, 1)
        ol.addWidget(self.rail)
        self.setCentralWidget(outer)
        self.status = self.statusBar()
        self.status_label = QLabel()
        self.status.addPermanentWidget(self.status_label)

    def _make_menus(self):
        mb = self.menuBar()
        m = mb.addMenu("&File")
        for k in ("new", "open"):
            m.addAction(self.act[k])
        self.recent_menu = m.addMenu("Open &Recent")
        self.recent_menu.aboutToShow.connect(self._fill_recent)
        m.addSeparator()
        for k in ("save", "save_as", "compress"):
            m.addAction(self.act[k])
        m.addSeparator()
        cm = m.addMenu("&Create")
        cm.addAction(self.act["merge"])
        cm.addAction(self.act["images_to_pdf"])
        m.addAction(self.act["export_images"])
        m.addSeparator()
        m.addAction(self.act["print"])
        m.addAction(self.act["properties"])
        m.addSeparator()
        m.addAction(self.act["close"])
        m.addAction(self.act["exit"])

        m = mb.addMenu("&Edit")
        for k in ("undo", "redo"):
            m.addAction(self.act[k])
        m.addSeparator()
        for k in ("copy", "select_all", "delete"):
            m.addAction(self.act[k])
        m.addSeparator()
        for k in ("find", "find_next", "find_prev"):
            m.addAction(self.act[k])
        m.addSeparator()
        m.addAction(self.act["prefs"])

        m = mb.addMenu("&View")
        for k in ("zoom_in", "zoom_out", "fit_page", "actual_size", "fit_width"):
            m.addAction(self.act[k])
        m.addSeparator()
        nav = m.addMenu("Page &Navigation")
        for k in ("first_page", "prev_page", "next_page", "last_page", "goto"):
            nav.addAction(self.act[k])
        m.addSeparator()
        for k in ("toggle_tools", "toggle_right", "highlight_fields", "full_screen"):
            m.addAction(self.act[k])
        tm = m.addMenu("&Theme")
        for k in ("theme_system", "theme_light", "theme_dark"):
            tm.addAction(self.act[k])

        m = mb.addMenu("&Tools")
        m.addAction(self.act["tool_select"])
        m.addAction(self.act["tool_hand"])
        m.addSeparator()
        cm = m.addMenu("&Comment")
        icons.set(cm.menuAction(), "comment_tool")
        for k in ("tool_highlight", "tool_underline", "tool_strike", None, "tool_note", "tool_textbox",
                  "tool_addtext", None, "tool_pen", "tool_rect", "tool_oval", "tool_arrow", "tool_line"):
            cm.addSeparator() if k is None else cm.addAction(self.act[k])
        cm.addMenu(self.stamp_menu).setText("Stamp")
        sm = m.addMenu("Fill && &Sign")
        icons.set(sm.menuAction(), "sign")
        sm.addAction(self.act["tool_addtext"])
        sm.addMenu(self.sign_menu).setText("Sign Yourself")
        om = m.addMenu("&Organize Pages")
        icons.set(om.menuAction(), "pages")
        for k in ("rotate_left", "rotate_right", "delete_pages", None, "insert_files", "insert_blank", None,
                  "extract_pages", "split", "merge", "export_images", None, "add_bookmark"):
            om.addSeparator() if k is None else om.addAction(self.act[k])
        fm = m.addMenu("Prepare a &Form")
        icons.set(fm.menuAction(), "form")
        for k in ("tool_field_text", "tool_field_check", "tool_field_date", None, "detect_fields", "flatten",
                  "highlight_fields"):
            fm.addSeparator() if k is None else fm.addAction(self.act[k])
        rm = m.addMenu("&Redact")
        icons.set(rm.menuAction(), "redact")
        for k in ("tool_redact", "find_redact", None, "apply_redactions", "clear_redactions"):
            rm.addSeparator() if k is None else rm.addAction(self.act[k])
        pm = m.addMenu("&Protect")
        icons.set(pm.menuAction(), "shield")
        for k in ("protect", "remove_password"):
            pm.addAction(self.act[k])
        m.addAction(self.act["compress"])

        m = mb.addMenu("&Help")
        m.addAction(self.act["open_logs"])
        m.addAction(self.act["about"])

    # ===================================================================== state

    def _restore(self):
        geo = settings.get("geometry")
        if isinstance(geo, (QByteArray, bytes)):
            self.restoreGeometry(QByteArray(geo))
        else:
            self.resize(1280, 860)
        tools_on = bool(settings.get("tools_pane", True, bool))
        right_on = bool(settings.get("right_pane", True, bool))
        self.toggle_tools_pane(tools_on)
        self.show_right(int(settings.get("right_index", 0, int) or 0))
        self.toggle_right_pane(right_on)
        sizes = settings.get("split_sizes")
        if isinstance(sizes, list) and len(sizes) == 3:
            try:
                self.split.setSizes([int(x) for x in sizes])
            except (TypeError, ValueError):
                pass

    def _save_state(self):
        settings.put("geometry", self.saveGeometry())
        settings.put("tools_pane", self.tools.isVisible())
        settings.put("right_pane", self.right_pane.isVisible())
        settings.put("right_index", self.right_stack.currentIndex())
        settings.put("split_sizes", self.split.sizes())
        tab = self.tab()
        if tab is not None:
            v = tab.view
            settings.put("last_zoom", v.fit_mode or v.zoom)
        if settings.get("restore_session", False, bool):
            settings.put("session", [t.doc.path for t in self.all_tabs() if t.doc.path])

    def tab(self) -> DocumentTab | None:
        w = self.tabs.currentWidget()
        return w if isinstance(w, DocumentTab) else None

    def all_tabs(self) -> list[DocumentTab]:
        return [self.tabs.widget(i) for i in range(self.tabs.count())]

    def _doc_call(self, name):
        t = self.tab()
        if t is None:
            return
        t.finish_editing()
        getattr(t.doc, name)()

    def _view_call(self, name, *args):
        t = self.tab()
        if t is not None:
            getattr(t.view, name)(*args)

    def _page_step(self, d):
        t = self.tab()
        if t is not None:
            t.view.go_to_page(t.view.current_page + d)

    def _tab_step(self, d):
        n = self.tabs.count()
        if n:
            self.tabs.setCurrentIndex((self.tabs.currentIndex() + d) % n)

    def _update_actions(self):
        t = self.tab()
        has = t is not None
        for k, act in self.act.items():
            if k in ("new", "open", "exit", "prefs", "merge", "images_to_pdf", "about", "open_logs", "toggle_tools",
                     "toggle_right", "full_screen") or k.startswith("theme_"):
                continue
            act.setEnabled(has)
        self.sign_btn.setEnabled(has)
        if has:
            d = t.doc
            self.act["undo"].setEnabled(d.can_undo())
            self.act["undo"].setText(f"&Undo {d.undo_label()}" if d.can_undo() else "&Undo")
            self.act["redo"].setEnabled(d.can_redo())
            self.act["redo"].setText(f"&Redo {d.redo_label()}" if d.can_redo() else "&Redo")
            self.act["copy"].setEnabled(t.view.has_selection())
            self.act["delete"].setEnabled(t.view.sel_annot is not None)
            self.act["remove_password"].setEnabled(d.security.encrypted)
            title = f"{'*' if d.dirty else ''}{d.title} - {APP_NAME}"
            self.setWindowTitle(title)
            i = self.tabs.indexOf(t)
            self.tabs.setTabText(i, ("● " if d.dirty else "") + d.title)
            self.tabs.setTabToolTip(i, d.path or d.title)
        else:
            self.setWindowTitle(APP_NAME)
        self.center_stack.setCurrentIndex(1 if self.tabs.count() else 0)
        self.right_pane.setVisible(has and self.act["toggle_right"].isChecked())
        self.float_bar.setVisible(has)
        self.float_bar.update_state(t)
        self.find_bar.update_status(t)
        self.position_overlays()

    def position_overlays(self):
        c = self.center
        top = self.context_bar.height() if self.context_bar.isVisible() else 0
        tabbar = self.tabs.tabBar().height() if self.tabs.count() else 0
        fb = self.find_bar
        fb.adjustSize()
        fb.move(c.width() - fb.width() - 24, top + tabbar + 8)
        fb.raise_()
        fl = self.float_bar
        fl.adjustSize()
        fl.move(c.width() - fl.width() - 24, c.height() - fl.height() - 24)
        fl.raise_()

    def resizeEvent(self, ev):
        super().resizeEvent(ev)
        QTimer.singleShot(0, self.position_overlays)

    def showEvent(self, ev):
        super().showEvent(ev)
        QTimer.singleShot(0, self.position_overlays)

    def message(self, text, ms=6000):
        self.status.showMessage(text, ms)

    # ===================================================================== tabs

    def add_tab(self, doc: Document, select=True) -> DocumentTab:
        t = DocumentTab(doc, self.renderer, self.styles, self)
        t.changed.connect(lambda tab=t: self._tab_doc_changed(tab))
        t.message.connect(self.message)
        t.tool_changed.connect(self._tool_changed)
        t.selection_changed.connect(self._update_actions)
        t.annot_selected.connect(self._annot_selected)
        t.search_update.connect(lambda tab=t: self.find_bar.update_status(tab) if tab is self.tab() else None)
        t.view.page_changed.connect(lambda i, tab=t: self._page_changed(tab, i))
        t.view.zoom_changed.connect(lambda z, tab=t: self.float_bar.update_state(tab) if tab is self.tab() else None)
        t.view.context_menu.connect(self._view_menu)
        idx = self.tabs.addTab(t, doc.title)
        close = QToolButton()
        icons.set(close, "close", 12)
        close.setAutoRaise(True)
        close.setToolTip("Close (Ctrl+W)")
        close.setFixedSize(18, 18)
        close.clicked.connect(lambda _=False, tab=t: self.close_tab(self.tabs.indexOf(tab)))
        self.tabs.tabBar().setTabButton(idx, self.tabs.tabBar().ButtonPosition.RightSide, close)
        self._apply_default_zoom(t)
        if select:
            self.tabs.setCurrentIndex(idx)
        self._update_actions()
        if doc.info.signed:
            self.message("This document is digitally signed. Saving changes will invalidate the signatures.", 10000)
        return t

    def _apply_default_zoom(self, t):
        mode = int(settings.get("default_zoom", 0, int) or 0)
        if mode == 0:
            t.view.fit_mode = "width"
        elif mode == 1:
            t.view.fit_mode = "page"
        elif mode == 2:
            t.view.fit_mode = None
            t.view.zoom = 1.0
        else:
            last = settings.get("last_zoom", "width")
            if last in ("width", "page"):
                t.view.fit_mode = last
            else:
                try:
                    t.view.fit_mode, t.view.zoom = None, float(last)
                except (TypeError, ValueError):
                    t.view.fit_mode = "width"
        QTimer.singleShot(0, t.view.relayout)

    def _tab_changed(self, _i):
        t = self.tab()
        self.thumbs.set_tab(t)
        self.bookmarks.set_tab(t)
        self.comments.set_tab(t)
        if t is not None:
            self._sync_tool_actions(t.view.tool)
            if self.find_bar.isVisible() and self.find_bar.edit.text() and t.search_query != self.find_bar.edit.text():
                t.find(self.find_bar.edit.text(), self.find_bar.case.isChecked())
            if t.view.suggestions is not None:
                self._show_detect_bar(t)
            else:
                self._show_tool_hint(t.view.tool)
        else:
            self.context_bar.show_hint("")
        self._update_actions()

    def _tab_doc_changed(self, t):
        if t is self.tab():
            self.thumbs.refresh()
            self.bookmarks.refresh()
            self.comments.refresh()
        self._update_actions()

    def _page_changed(self, t, i):
        if t is self.tab():
            self.thumbs.current_changed(i)
            self.float_bar.update_state(t)

    def close_tab(self, index) -> bool:
        t = self.tabs.widget(index)
        if t is None:
            return True
        t.finish_editing()
        if t.doc.dirty:
            self.tabs.setCurrentIndex(index)
            r = QMessageBox.question(
                self, APP_NAME, f"Save changes to “{t.doc.title}” before closing?",
                QMessageBox.StandardButton.Save | QMessageBox.StandardButton.Discard |
                QMessageBox.StandardButton.Cancel, QMessageBox.StandardButton.Save)
            if r == QMessageBox.StandardButton.Cancel:
                return False
            if r == QMessageBox.StandardButton.Save and not self.save():
                return False
        self.tabs.removeTab(index)
        t.close_document()
        t.deleteLater()
        self._update_actions()
        if not self.tabs.count():
            self.welcome.refresh()
            self.context_bar.show_hint("")
        return True

    def close_current(self):
        if self.tabs.count():
            self.close_tab(self.tabs.currentIndex())

    def closeEvent(self, ev):
        self._save_state()
        for i in reversed(range(self.tabs.count())):
            if not self.close_tab(i):
                ev.ignore()
                return
        self.renderer.shutdown()
        ev.accept()

    # ===================================================================== open / save

    def open_dialog(self):
        files, _ = QFileDialog.getOpenFileNames(self, "Open", dlg.last_dir(), dlg.OPEN_FILTER)
        if files:
            dlg.remember_dir(files[0])
            self.open_paths(files)

    def open_paths(self, paths: list[str]):
        images = [p for p in paths if P.is_image(p)]
        for p in paths:
            if P.is_image(p):
                continue
            self.open_file(p)
        if images:
            try:
                pdf = P.image_to_pdf_doc(images, fit="image")
                name = os.path.splitext(os.path.basename(images[0]))[0] + ".pdf"
                self.add_tab(Document.from_pymupdf(pdf, name))
            except Exception as e:
                log.exception("images to pdf")
                QMessageBox.warning(self, APP_NAME, f"Couldn't open the image(s):\n{e}")
        self.raise_()
        self.activateWindow()

    def open_file(self, path: str):
        path = os.path.abspath(path)
        for t in self.all_tabs():
            if t.doc.path and os.path.normcase(t.doc.path) == os.path.normcase(path):
                self.tabs.setCurrentWidget(t)
                return t
        if not os.path.exists(path):
            QMessageBox.warning(self, APP_NAME, f"“{path}” doesn't exist any more.")
            return None
        password = None
        while True:
            try:
                doc = Document.open_file(path, password)
                break
            except PasswordRequired:
                prompt = (f"“{os.path.basename(path)}” is password protected.\n\nPassword:" if password is None
                          else "That password is wrong. Try again:")
                password, ok = QInputDialog.getText(self, "Password", prompt, QLineEdit.EchoMode.Password)
                if not ok:
                    return None
            except NotAPdf as e:
                QMessageBox.warning(self, APP_NAME, f"“{os.path.basename(path)}” isn't a PDF that can be opened.\n\n{e}")
                return None
            except OSError as e:
                QMessageBox.warning(self, APP_NAME, f"Couldn't open “{os.path.basename(path)}”:\n{e}")
                return None
        settings.add_recent(path)
        self.welcome.refresh()
        return self.add_tab(doc)

    def _fill_recent(self):
        m = self.recent_menu
        m.clear()
        items = settings.recent()
        for k, p in enumerate(items):
            act = m.addAction(f"&{k + 1 if k < 9 else 0}  {os.path.basename(p)}" if k < 10 else os.path.basename(p))
            act.setToolTip(p)
            act.setStatusTip(p)
            act.setEnabled(os.path.exists(p))
            act.triggered.connect(lambda _=False, path=p: self.open_paths([path]))
        if not items:
            m.addAction("No recent files").setEnabled(False)
        else:
            m.addSeparator()
            m.addAction("Clear Recent Files", lambda: (settings.clear_recent(), self.welcome.refresh()))

    def new_document(self):
        d = dlg.NewDocumentDialog(self)
        if d.exec():
            paper, landscape, count = d.values()
            doc = P.new_blank(paper, landscape, count)
            n = sum(1 for t in self.all_tabs() if t.doc.path is None) + 1
            doc.title = f"Untitled {n}.pdf"
            self.add_tab(doc)

    def save(self) -> bool:
        t = self.tab()
        if t is None:
            return False
        t.finish_editing()
        if t.doc.path is None:
            return self.save_as(adopt=True)
        return self._write(t, t.doc.path)

    def save_as(self, adopt=False) -> bool:
        t = self.tab()
        if t is None:
            return False
        t.finish_editing()
        start = t.doc.path or os.path.join(dlg.last_dir(), t.doc.title)
        path, _ = QFileDialog.getSaveFileName(self, "Save As" if not adopt else "Save", start, dlg.PDF_FILTER)
        if not path:
            return False
        if not path.lower().endswith(".pdf"):
            path += ".pdf"
        dlg.remember_dir(path)
        ok = self._write(t, path)
        if ok:
            settings.add_recent(path)
        return ok

    def _write(self, t: DocumentTab, path: str) -> bool:
        d = t.doc
        if R.count_marks(d):
            r = QMessageBox.question(
                self, APP_NAME, "This document has redaction marks that haven't been applied. The content under "
                "them is still in the file.\n\nApply the redactions before saving?",
                QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No | QMessageBox.StandardButton.Cancel,
                QMessageBox.StandardButton.Yes)
            if r == QMessageBox.StandardButton.Cancel:
                return False
            if r == QMessageBox.StandardButton.Yes:
                t.run(R.apply, d)
        if d.info.signed and not getattr(d, "_sign_warned", False):
            r = QMessageBox.warning(
                self, APP_NAME, "This document is digitally signed. Saving changes will invalidate the "
                "signatures.\n\nSave anyway?", QMessageBox.StandardButton.Save | QMessageBox.StandardButton.Cancel)
            if r != QMessageBox.StandardButton.Save:
                return False
            d._sign_warned = True
        try:
            QApplication.setOverrideCursor(Qt.CursorShape.WaitCursor)
            d.save(path)
        except Exception as e:
            log.exception("save failed")
            QApplication.restoreOverrideCursor()
            QMessageBox.critical(self, APP_NAME, f"Couldn't save “{os.path.basename(path)}”:\n\n{e}\n\n"
                                 "If the file is open in another program, close it and try again.")
            return False
        QApplication.restoreOverrideCursor()
        self.message(f"Saved {os.path.basename(path)}")
        self._update_actions()
        return True

    # ===================================================================== drag & drop

    def dragEnterEvent(self, ev):
        if ev.mimeData().hasUrls():
            ev.acceptProposedAction()

    def dropEvent(self, ev):
        files = [u.toLocalFile() for u in ev.mimeData().urls() if u.toLocalFile()]
        files = [f for f in files if f.lower().endswith(".pdf") or P.is_image(f)]
        if files:
            self.open_paths(files)

    # ===================================================================== tools

    def set_tool(self, tool):
        t = self.tab()
        if t is None:
            return
        if tool in ("highlight", "underline", "strike") and t.view.has_selection():
            kind = {"highlight": A.HIGHLIGHT, "underline": A.UNDERLINE, "strike": A.STRIKEOUT}[tool]
            t.markup_selection(kind)
            self._sync_tool_actions(t.view.tool)
            return
        if tool == "redact" and t.view.has_selection():
            t.redact_selection()
        if t.view.suggestions is not None and tool != "detect":
            self._end_detect(t)
        t.set_tool(tool)

    def _tool_changed(self, tool):
        self._sync_tool_actions(tool)
        self._show_tool_hint(tool)
        self._refresh_style_box()

    def _sync_tool_actions(self, tool):
        act = self.act.get("tool_" + tool)
        if act is not None:
            act.setChecked(True)
        else:
            for a in self.tool_group.actions():
                a.setChecked(False)

    def _show_tool_hint(self, tool):
        hint = TOOL_HINTS.get(tool, "")
        if hint:
            self.context_bar.show_hint(hint, [("Done", lambda: self.set_tool("select"), False)])
        else:
            self.context_bar.show_hint("")
        QTimer.singleShot(0, self.position_overlays)

    def _group_opened(self, key):
        if key == "comment":
            self._refresh_style_box()

    def _annot_selected(self, a):
        self._update_actions()
        self._refresh_style_box()

    def _refresh_style_box(self):
        t = self.tab()
        box = self.tools.style_box
        if t is None:
            return
        a = t.view.sel_annot
        if a is not None and a.type in ANNOT_TOOL and not a.is_signature:
            tool = ANNOT_TOOL[a.type]
            box.show_values(f"Selected {a.type_name.lower()}", hexcolor(a.color), a.width, a.fontsize or 12,
                            has_width=a.type in (A.INK, A.SQUARE, A.CIRCLE, A.LINE), has_size=a.type == A.FREETEXT)
            return
        tool = t.view.tool if t.view.tool in STYLE_TOOLS else "highlight"
        box.show_values(f"{TOOL_LABELS[tool][0]} style", self.styles.get(tool, "color"),
                        self.styles.get(tool, "width"), self.styles.get(tool, "size"),
                        has_width=tool in ("pen", "rect", "oval", "arrow", "line"),
                        has_size=tool in ("textbox", "addtext"))

    def _style(self, key, value):
        t = self.tab()
        if t is None:
            return
        a = t.view.sel_annot
        if a is not None and a.type in ANNOT_TOOL and not a.is_signature:
            if key == "color":
                t.style_annot(a, color=value)
            elif key == "width":
                t.style_annot(a, width=value)
            else:
                t.style_annot(a, fontsize=value)
            return
        tool = t.view.tool if t.view.tool in STYLE_TOOLS else None
        if tool is None:
            self.message("Choose a comment tool or select a comment first.")
            return
        self.styles.set(tool, key, value)
        self._refresh_style_box()

    # ----- signatures / stamps

    def _fill_sign_menu(self):
        m = self.sign_menu
        m.clear()
        sigs = sigstore.load_all()
        for path, png in sigs:
            img = QImage.fromData(png)
            ic = QIcon()
            if not img.isNull():
                from PySide6.QtGui import QPixmap

                pm = QPixmap.fromImage(img.scaled(160, 50, Qt.AspectRatioMode.KeepAspectRatio,
                                                  Qt.TransformationMode.SmoothTransformation))
                ic = QIcon(pm)
            act = m.addAction(ic, "")
            act.triggered.connect(lambda _=False, b=png: self.start_signature(b))
        if sigs:
            m.setStyleSheet("QMenu::icon { width: 160px; height: 50px; } QMenu { icon-size: 160px; }")
            m.addSeparator()
            dm = m.addMenu("Delete Saved Signature")
            for k, (path, _png) in enumerate(sigs):
                dm.addAction(f"Signature {k + 1}", lambda _=False, p=path: sigstore.delete(p))
        m.addAction(self.act["create_signature"])
        note = m.addAction("These are visual signatures, not certificate-based digital signatures.")
        note.setEnabled(False)

    def create_signature(self):
        if self.tab() is None:
            return
        d = dlg.CreateSignatureDialog(self)
        if d.exec() and d.png:
            self.start_signature(d.png)

    def start_signature(self, png: bytes):
        t = self.tab()
        if t is None:
            return
        img = QImage.fromData(png)
        w, h = (img.width(), img.height()) if not img.isNull() else (300, 100)
        width = 150.0
        t.view.pending_signature = png
        t.view.pending_signature_size = (width, width * h / max(1, w))
        t.set_tool("sign")
        self._show_tool_hint("sign")

    def start_stamp(self, index):
        t = self.tab()
        if t is None:
            return
        t.view.pending_stamp = index
        t.set_tool("stamp")

    # ===================================================================== edit menu

    def copy(self):
        t = self.tab()
        if t is not None and t.copy():
            self.message("Copied")

    def delete_selected(self):
        t = self.tab()
        if t is not None and t.view.sel_annot is not None:
            t.delete_annot(t.view.sel_annot)

    def find_bar_open(self):
        if self.tab() is not None:
            self.find_bar.open_bar()

    def find_step(self, step):
        t = self.tab()
        if t is None:
            return
        if not t.search_query and self.find_bar.edit.text():
            t.find(self.find_bar.edit.text(), self.find_bar.case.isChecked())
            return
        if not self.find_bar.isVisible():
            self.find_bar.open_bar()
        t.next_hit(step)

    def preferences(self):
        d = dlg.PreferencesDialog(self)
        if d.exec():
            d.apply()
            mode = settings.get("theme", "system")
            self.set_theme(mode)

    def set_theme(self, mode):
        settings.put("theme", mode)
        act = self.act.get("theme_" + mode)
        if act:
            act.setChecked(True)
        theme.apply(QApplication.instance(), mode)
        self._reicon()
        for t in self.all_tabs():
            t.view.viewport().update()

    def _reicon(self):
        icons.refresh(self)
        for w in QApplication.topLevelWidgets():
            if w is not self:
                icons.refresh(w)

    # ===================================================================== view menu

    def goto_page(self):
        t = self.tab()
        if t is None:
            return
        n, ok = QInputDialog.getInt(self, "Go to Page", f"Page (1-{t.doc.page_count}):", t.view.current_page + 1, 1,
                                    t.doc.page_count)
        if ok:
            t.view.go_to_page(n - 1)

    def toggle_tools_pane(self, on=None):
        if on is None:
            on = not self.tools.isVisible()
        self.tools.setVisible(bool(on))
        self.act["toggle_tools"].setChecked(bool(on))
        QTimer.singleShot(0, self.position_overlays)

    def toggle_right_pane(self, on=None):
        if on is None:
            on = not self.right_pane.isVisible()
        self.right_pane.setVisible(bool(on) and self.tab() is not None)
        self.act["toggle_right"].setChecked(bool(on))
        for k, b in enumerate(self.rail_buttons):
            b.setChecked(bool(on) and self.right_stack.currentIndex() == k)
        if on:
            self.thumbs._timer.start()
        QTimer.singleShot(0, self.position_overlays)

    def show_right(self, index, toggle=False):
        if toggle and self.right_pane.isVisible() and self.right_stack.currentIndex() == index:
            self.toggle_right_pane(False)
            return
        self.right_stack.setCurrentIndex(index)
        self.toggle_right_pane(True)

    def toggle_full_screen(self):
        if self.isFullScreen():
            self.showNormal()
            self.menuBar().show()
            self.quick_bar.show()
            self.rail.show()
            self.toggle_tools_pane(self._fs_state[0])
            self.toggle_right_pane(self._fs_state[1])
        else:
            self._fs_state = (self.tools.isVisible(), self.right_pane.isVisible())
            self.menuBar().hide()
            self.quick_bar.hide()
            self.rail.hide()
            self.toggle_tools_pane(False)
            self.toggle_right_pane(False)
            self.showFullScreen()
            self.message("Full screen. Press Esc or Ctrl+L to exit.")

    def keyPressEvent(self, ev):
        if ev.key() == Qt.Key.Key_Escape and self.isFullScreen():
            self.toggle_full_screen()
            return
        super().keyPressEvent(ev)

    def toggle_highlight_fields(self):
        on = self.act["highlight_fields"].isChecked()
        settings.put("highlight_fields", on)
        for t in self.all_tabs():
            t.view.highlight_fields = on
            t.view.viewport().update()

    # ===================================================================== context menus

    def _view_menu(self, gpos, ctx):
        t = self.tab()
        if t is None:
            return
        m = QMenu(self)
        a = ctx.get("annot")
        f = ctx.get("field")
        if f is not None:
            m.addAction("Field Properties...", lambda: self.field_properties(t, f))
            m.addAction("Delete Field", lambda: t.run(F.delete_field, t.doc, f.page, f.xref))
        elif a is not None:
            if a.type in (A.FREETEXT, A.NOTE) or a.type in A.MARKUP or a.type in (A.SQUARE, A.CIRCLE, A.INK, A.LINE):
                m.addAction("Edit Text..." if a.type in (A.FREETEXT,) else "Add/Edit Comment...",
                            lambda: t.edit_annot_text(a))
            if a.type in ANNOT_TOOL and not a.is_signature:
                cm = m.addMenu("Colour")
                from .toolspane import SWATCHES

                for c in SWATCHES:
                    cm.addAction(icons.color_swatch(QColor(c)), c, lambda _=False, col=c: t.style_annot(a, color=col))
                if a.type in (A.INK, A.SQUARE, A.CIRCLE, A.LINE):
                    wm = m.addMenu("Line Width")
                    for w in (1, 2, 3, 4, 6, 8):
                        wm.addAction(f"{w} pt", lambda _=False, v=w: t.style_annot(a, width=v))
                if a.type == A.FREETEXT:
                    fm = m.addMenu("Font Size")
                    for s in (8, 9, 10, 11, 12, 14, 16, 18, 24, 36):
                        fm.addAction(f"{s} pt", lambda _=False, v=s: t.style_annot(a, fontsize=v))
            m.addSeparator()
            m.addAction(icons.icon("delete"), "Delete", lambda: t.delete_annot(a))
        else:
            if ctx.get("has_selection"):
                m.addAction(self.act["copy"])
                m.addSeparator()
                m.addAction(icons.icon("highlight"), "Highlight Text", lambda: t.markup_selection(A.HIGHLIGHT))
                m.addAction(icons.icon("underline"), "Underline Text", lambda: t.markup_selection(A.UNDERLINE))
                m.addAction(icons.icon("strike"), "Strikethrough Text", lambda: t.markup_selection(A.STRIKEOUT))
                m.addAction(icons.icon("redact"), "Mark for Redaction", t.redact_selection)
                m.addSeparator()
            if ctx.get("page") is not None:
                i, pt = ctx["page"], ctx["point"]
                m.addAction(icons.icon("note"), "Add Sticky Note", lambda: t.create_note(i, pt))
                m.addAction(icons.icon("addtext"), "Add Text Here", lambda: t.create_plain_text(i, pt))
                m.addSeparator()
            m.addAction(self.act["select_all"])
            m.addSeparator()
            for k in ("rotate_left", "rotate_right"):
                m.addAction(self.act[k])
            m.addAction(self.act["add_bookmark"])
            m.addSeparator()
            for k in ("fit_page", "fit_width", "actual_size"):
                m.addAction(self.act[k])
            m.addAction(self.act["print"])
        m.exec(gpos)

    def _thumb_menu(self, gpos):
        if self.tab() is None:
            return
        m = QMenu(self)
        for k in ("rotate_left", "rotate_right", "delete_pages", None, "insert_files", "insert_blank", None,
                  "extract_pages", "export_images", "print"):
            m.addSeparator() if k is None else m.addAction(self.act[k])
        m.exec(gpos)

    def _go_annot(self, a):
        t = self.tab()
        if t is None or a.page >= t.doc.page_count:
            return
        t.view.go_to_page(a.page)
        t.view.ensure_visible(a.page, a.rect)
        t.set_tool("select")
        for x in t.view.annots(a.page):
            if x.xref == a.xref:
                t.view.select_annot(x)

    # ===================================================================== pages

    def _target_pages(self, t) -> list[int]:
        sel = self.thumbs.selected_pages() if self.right_pane.isVisible() and self.right_stack.currentIndex() == 0 else []
        if len(sel) > 1 or (sel and self.thumbs.list.hasFocus()):
            return sel
        return [t.view.current_page]

    def rotate(self, deg):
        t = self.tab()
        if t:
            t.run(P.rotate, t.doc, self._target_pages(t), deg)

    def delete_pages(self):
        t = self.tab()
        if t is None:
            return
        pages = self._target_pages(t)
        if len(pages) >= t.doc.page_count:
            QMessageBox.information(self, "Delete Pages", "A PDF needs at least one page.")
            return
        label = f"page {pages[0] + 1}" if len(pages) == 1 else f"{len(pages)} pages"
        if QMessageBox.question(self, "Delete Pages", f"Delete {label}?") != QMessageBox.StandardButton.Yes:
            return
        t.run(P.delete, t.doc, pages)
        self.thumbs.list.clearSelection()

    def move_pages(self, rows, to):
        t = self.tab()
        if t:
            t.run(P.move, t.doc, rows, to)
            new_start = to - sum(1 for r in rows if r < to)
            self.thumbs.list.clearSelection()
            for k in range(len(rows)):
                it = self.thumbs.list.item(new_start + k)
                if it:
                    it.setSelected(True)

    def _password_cb(self, name):
        pw, ok = QInputDialog.getText(self, "Password", f"“{name}” is password protected.\n\nPassword:",
                                      QLineEdit.EchoMode.Password)
        return pw if ok else None

    def insert_files(self):
        t = self.tab()
        if t is None:
            return
        files, _ = QFileDialog.getOpenFileNames(self, "Insert Pages", dlg.last_dir(), dlg.OPEN_FILTER)
        if files:
            dlg.remember_dir(files[0])
            self.insert_files_at(files, t.view.current_page + 1)

    def insert_files_at(self, files, at):
        t = self.tab()
        if t is None:
            return
        n = t.run(P.insert_files, t.doc, files, at, self._password_cb)
        if n:
            self.message(f"Inserted {n} page{'s' if n != 1 else ''}")
            t.view.go_to_page(at)

    def insert_blank(self):
        t = self.tab()
        if t:
            at = t.view.current_page + 1
            t.run(P.insert_blank, t.doc, at, t.view.current_page)
            t.view.go_to_page(at)

    def extract_pages(self):
        t = self.tab()
        if t is None:
            return
        sel = self._target_pages(t)
        default = ", ".join(str(p + 1) for p in sel)
        d = dlg.RangeDialog("Extract Pages", "Pages to copy into a new document:", default, t.doc.page_count, self,
                            "Extract")
        if d.exec():
            pages = P.flatten_ranges(d.ranges)
            out = t.run(P.extract, t.doc, pages)
            if out is not None:
                stem = os.path.splitext(t.doc.title)[0]
                self.add_tab(Document.from_pymupdf(out, f"{stem} (pages).pdf"))

    def split_doc(self):
        t = self.tab()
        if t is None:
            return
        d = dlg.SplitDialog(t.doc.page_count, self)
        if not d.exec():
            return
        folder = QFileDialog.getExistingDirectory(self, "Save the parts in", dlg.last_dir("split_dir"))
        if not folder:
            return
        dlg.remember_dir(folder, "split_dir")
        stem = os.path.splitext(t.doc.title)[0]
        written = t.run(P.split, t.doc, d.result_ranges, folder, stem)
        if written:
            r = QMessageBox.information(self, "Split Document", f"Saved {len(written)} files in\n{folder}",
                                        QMessageBox.StandardButton.Open | QMessageBox.StandardButton.Ok)
            if r == QMessageBox.StandardButton.Open:
                QDesktopServices.openUrl(QUrl.fromLocalFile(folder))

    def merge_files(self):
        initial = [t.doc.path for t in self.all_tabs() if t.doc.path][:1] if self.tab() else []
        d = dlg.FileListDialog("Combine Files", "Add PDFs and images, then drag them into order.", "Combine",
                               dlg.OPEN_FILTER, initial, self)
        if not d.exec():
            return
        try:
            QApplication.setOverrideCursor(Qt.CursorShape.WaitCursor)
            out = P.merge(d.paths(), self._password_cb)
        except Exception as e:
            QApplication.restoreOverrideCursor()
            QMessageBox.warning(self, "Combine Files", f"Couldn't combine the files:\n{e}")
            return
        QApplication.restoreOverrideCursor()
        self.add_tab(Document.from_pymupdf(out, "Combined.pdf"))
        self.message("Combined. Press Ctrl+S to choose where to save it.")

    def images_to_pdf(self):
        from PySide6.QtWidgets import QComboBox, QFormLayout

        extra = QWidget()
        fl = QFormLayout(extra)
        fl.setContentsMargins(0, 0, 0, 0)
        size = QComboBox()
        size.addItems(["Letter", "A4", "Image's own size"])
        fl.addRow("Page size:", size)
        d = dlg.FileListDialog("Images to PDF", "Each image becomes one page, in this order.", "Create",
                               dlg.IMAGE_FILTER, [], self, extra)
        if not d.exec():
            return
        fit = {0: "Letter", 1: "A4", 2: "image"}[size.currentIndex()]
        try:
            out = P.image_to_pdf_doc(d.paths(), fit=fit, margin=0 if fit == "image" else 18)
        except Exception as e:
            QMessageBox.warning(self, "Images to PDF", f"Couldn't read the images:\n{e}")
            return
        self.add_tab(Document.from_pymupdf(out, "Images.pdf"))

    def export_images(self):
        t = self.tab()
        if t is None:
            return
        d = dlg.ExportImagesDialog(t.doc.page_count, t.view.current_page, self)
        if not d.exec():
            return
        folder = QFileDialog.getExistingDirectory(self, "Save the images in", dlg.last_dir("export_dir"))
        if not folder:
            return
        dlg.remember_dir(folder, "export_dir")
        prog = QProgressDialog("Exporting pages...", "Cancel", 0, len(d.result_pages), self)
        prog.setWindowModality(Qt.WindowModality.WindowModal)
        prog.setMinimumDuration(300)

        def step(n, total):
            prog.setValue(n)
            QApplication.processEvents()
            return not prog.wasCanceled()

        stem = os.path.splitext(t.doc.title)[0]
        written = t.run(P.export_images, t.doc, d.result_pages, folder, stem, d.fmt.currentText(), d.dpi.value(), step)
        prog.close()
        if written:
            self.message(f"Saved {len(written)} images in {folder}")

    def add_bookmark(self):
        t = self.tab()
        if t is None:
            return
        default = t.view.selected_text().strip()[:80] if t.view.has_selection() else f"Page {t.view.current_page + 1}"
        title, ok = QInputDialog.getText(self, "Add Bookmark", "Bookmark title:", text=default)
        if not ok or not title.strip():
            return
        items = M.bookmarks(t.doc)
        page = t.view.current_page
        pos = len(items)
        for k, b in enumerate(items):
            if b.level == 1 and b.page > page:
                pos = k
                break
        items.insert(pos, M.Bookmark(1, title.strip(), page, None))
        t.run(M.set_bookmarks, t.doc, items, "Add Bookmark")
        self.show_right(1)

    # ===================================================================== forms

    def prompt_new_field(self, t, page, rect, kind):
        base = {"text": "Text", "checkbox": "Check", "date": "Date"}[kind]
        name = F._unique_name(t.doc, base)
        d = dlg.FieldDialog("New Field", name, 0, kind, self)
        if d.exec():
            nm, fs = d.values()
            t.run(F.add_field, t.doc, page, rect, kind, nm or name, fs)

    def field_properties(self, t, f):
        d = dlg.FieldDialog("Field Properties", f.name, f.fontsize, "checkbox" if f.kind == F.CHECKBOX else "text",
                            self)
        if d.exec():
            nm, fs = d.values()
            t.run(F.update_field, t.doc, f.page, f.xref, nm, fs if f.kind == F.TEXT else None)

    def detect_fields(self):
        t = self.tab()
        if t is None:
            return
        prog = QProgressDialog("Looking for form fields...", "Cancel", 0, t.doc.page_count, self)
        prog.setWindowModality(Qt.WindowModality.WindowModal)
        prog.setMinimumDuration(400)

        def step(n, total):
            prog.setValue(n)
            QApplication.processEvents()
            return not prog.wasCanceled()

        found = t.run(D.detect, t.doc, step)
        prog.close()
        if found is None:
            return
        if not found:
            QMessageBox.information(self, "Detect Form Fields", "No blank fields were found in this document.")
            return
        for s in found:
            s["accepted"] = True
        t.view.suggestions = found
        t.set_tool("detect")
        self._show_detect_bar(t)
        t.view.go_to_page(found[0]["page"])
        t.view.viewport().update()

    def _show_detect_bar(self, t):
        n = sum(1 for s in t.view.suggestions or [] if s.get("accepted", True))
        total = len(t.view.suggestions or [])
        self.context_bar.show_hint(
            f"{total} suggested field{'s' if total != 1 else ''} (green). Click a box to reject it (red) or accept it again.",
            [(f"Create {n} Field{'s' if n != 1 else ''}", lambda: self._create_detected(t), True),
             ("Cancel", lambda: self._end_detect(t), False)])
        QTimer.singleShot(0, self.position_overlays)

    def suggestions_changed(self, t):
        if t is self.tab():
            self._show_detect_bar(t)

    def _create_detected(self, t):
        specs = [s for s in t.view.suggestions or [] if s.get("accepted", True)]
        self._end_detect(t)
        n = t.run(F.add_fields_bulk, t.doc, specs)
        if n:
            self.message(f"Created {n} fields. Click a field to fill it in.")

    def _end_detect(self, t):
        t.view.suggestions = None
        if t.view.tool == "detect":
            t.view.set_tool("select")
        t.view.viewport().update()
        self.context_bar.show_hint("")

    def flatten_form(self):
        t = self.tab()
        if t is None:
            return
        if not F.has_fields(t.doc):
            QMessageBox.information(self, "Flatten Form", "This document has no form fields.")
            return
        if QMessageBox.question(self, "Flatten Form",
                                "Bake the current values into the pages and remove the fields? They can't be "
                                "edited afterwards (you can undo until you close the document).") \
                == QMessageBox.StandardButton.Yes:
            t.run(F.flatten, t.doc)

    # ===================================================================== redact / protect

    def find_and_redact(self):
        t = self.tab()
        if t is None:
            return
        d = dlg.FindRedactDialog(self)
        if t.view.has_selection():
            d.text.setText(t.view.selected_text().strip()[:100])
        if not d.exec():
            return
        QApplication.setOverrideCursor(Qt.CursorShape.WaitCursor)
        try:
            matches = R.find_matches(t.doc, **d.values())
        finally:
            QApplication.restoreOverrideCursor()
        n = sum(len(v) for v in matches.values())
        if not n:
            QMessageBox.information(self, "Find and Redact", "Nothing matched.")
            return
        t.run(R.mark_matches, t.doc, matches)
        self.message(f"Marked {n} area{'s' if n != 1 else ''} for redaction. Review them, then Apply Redactions.")
        t.view.go_to_page(min(matches))

    def apply_redactions(self):
        t = self.tab()
        if t is None:
            return
        n = R.count_marks(t.doc)
        if not n:
            QMessageBox.information(self, "Apply Redactions",
                                    "There are no redaction marks. Use Mark for Redaction or Find and Redact first.")
            return
        d = dlg.ApplyRedactionsDialog(n, self)
        if d.exec():
            done = t.run(R.apply, t.doc, d.graphics.isChecked(), d.meta.isChecked())
            if done:
                self.message(f"Applied {done} redaction{'s' if done != 1 else ''}. Save to make it permanent.")

    def clear_redactions(self):
        t = self.tab()
        if t:
            t.run(R.clear_marks, t.doc)

    def protect(self):
        t = self.tab()
        if t is None:
            return
        d = dlg.ProtectDialog(self)
        if d.exec():
            open_pw, perm_pw, allow = d.values()
            t.run(S.protect, t.doc, open_pw, perm_pw, allow)
            self.message("Password protection set. Save to apply it to the file.")

    def remove_password(self):
        t = self.tab()
        if t is None:
            return
        s = t.doc.security
        if not s.encrypted:
            QMessageBox.information(self, "Remove Password", "This document isn't password protected.")
            return
        if s.restricted and t.doc.info.extra.get("owner_unknown"):
            pw, ok = QInputDialog.getText(self, "Remove Password",
                                          "This document has a permissions password.\n\nEnter it to remove security:",
                                          QLineEdit.EchoMode.Password)
            if not ok:
                return
            if not self._check_owner_pw(t, pw):
                QMessageBox.warning(self, "Remove Password", "That permissions password is wrong.")
                return
        if QMessageBox.question(self, "Remove Password", "Remove all password protection from this document? "
                                "It takes effect when you save.") == QMessageBox.StandardButton.Yes:
            t.run(S.remove, t.doc)

    def _check_owner_pw(self, t, pw) -> bool:
        if not t.doc.path:
            return False
        try:
            with open(t.doc.path, "rb") as fh:
                data = fh.read()
            d = Document(data, password=pw)
            ok = d.info.opened_as_owner
            d.close()
            return ok
        except Exception:
            return False

    # ===================================================================== compress / print / properties

    def compress(self):
        t = self.tab()
        if t is None:
            return
        t.finish_editing()
        data = t.doc._encoded(garbage=1)
        d = dlg.CompressDialog(len(data), self)
        if not d.exec():
            return
        stem = os.path.splitext(t.doc.title)[0]
        start = os.path.join(os.path.dirname(t.doc.path) if t.doc.path else dlg.last_dir(), f"{stem} (reduced).pdf")
        path, _ = QFileDialog.getSaveFileName(self, "Save Smaller Copy", start, dlg.PDF_FILTER)
        if not path:
            return
        if t.doc.path and os.path.abspath(path) == os.path.abspath(t.doc.path):
            QMessageBox.warning(self, "Reduce File Size", "Choose a different name: the original is never overwritten.")
            return
        tmp = path + ".part"
        proc, conn = C.start(data, d.preset(), tmp)
        prog = QProgressDialog("Reducing file size...", "Cancel", 0, 0, self)
        prog.setWindowModality(Qt.WindowModality.WindowModal)
        prog.setMinimumDuration(0)
        prog.show()
        result = None
        while True:
            QApplication.processEvents()
            if prog.wasCanceled():
                C.cancel(proc, tmp)
                prog.close()
                return
            if conn.poll(0.05):
                result = conn.recv()
                break
            if not proc.is_alive() and not conn.poll(0):
                result = ("error", "The helper process stopped unexpectedly.")
                break
        prog.close()
        proc.join(1)
        if result[0] != "ok":
            QMessageBox.warning(self, "Reduce File Size", f"Couldn't reduce the file:\n{result[1]}")
            try:
                os.unlink(tmp)
            except OSError:
                pass
            return
        os.replace(tmp, path)
        new = os.path.getsize(path)
        pct = 100 - new * 100 / max(1, len(data))
        QMessageBox.information(self, "Reduce File Size",
                                f"Saved {os.path.basename(path)}\n\n{dlg.human_size(len(data))} → {dlg.human_size(new)}"
                                f" ({pct:.0f}% smaller)" if pct > 0 else
                                f"Saved {os.path.basename(path)}\n\n{dlg.human_size(new)}. This file was already "
                                "well compressed.")

    def print_doc(self):
        t = self.tab()
        if t is None:
            return
        from PySide6.QtPrintSupport import QPrintDialog, QPrinter

        t.finish_editing()
        printer = QPrinter(QPrinter.PrinterMode.HighResolution)
        printer.setDocName(t.doc.title)
        printer.setFromTo(1, t.doc.page_count)
        pd = QPrintDialog(printer, self)
        pd.setOption(QPrintDialog.PrintDialogOption.PrintPageRange, True)
        pd.setOption(QPrintDialog.PrintDialogOption.PrintCurrentPage, True)
        pd.setMinMax(1, t.doc.page_count)
        if pd.exec() != QPrintDialog.DialogCode.Accepted:
            return
        rng = printer.printRange()
        if rng == QPrinter.PrintRange.PageRange:
            pages = list(range(printer.fromPage() - 1, printer.toPage()))
        elif rng == QPrinter.PrintRange.CurrentPage:
            pages = [t.view.current_page]
        else:
            pages = list(range(t.doc.page_count))
        dpi = min(300, max(150, printer.resolution()))
        prog = QProgressDialog("Printing...", "Cancel", 0, len(pages), self)
        prog.setWindowModality(Qt.WindowModality.WindowModal)
        prog.setMinimumDuration(300)
        painter = QPainter()
        if not painter.begin(printer):
            QMessageBox.warning(self, "Print", "Couldn't start printing.")
            return
        try:
            for n, i in enumerate(pages):
                if prog.wasCanceled():
                    printer.abort()
                    break
                if n:
                    printer.newPage()
                w, h, stride, data = P.render_page(t.doc, i, dpi)
                img = QImage(data, w, h, stride, QImage.Format.Format_RGB888)
                area = printer.pageLayout().paintRectPixels(printer.resolution())
                area.moveTo(0, 0)
                scaled = img.size().scaled(area.size(), Qt.AspectRatioMode.KeepAspectRatio)
                x = (area.width() - scaled.width()) // 2
                y = (area.height() - scaled.height()) // 2
                from PySide6.QtCore import QRect

                painter.drawImage(QRect(x, y, scaled.width(), scaled.height()), img)
                prog.setValue(n + 1)
                QApplication.processEvents()
        finally:
            painter.end()
            prog.close()

    def properties(self):
        t = self.tab()
        if t is None:
            return
        st = S.status(t.doc)
        sec = "No security"
        if st.encrypted:
            parts = []
            if st.open_password:
                parts.append("password required to open")
            if st.restricted:
                denied = [k for k, v in st.allowed.items() if not v]
                parts.append("restricted: no " + ", ".join(denied) if denied else "restricted")
            sec = "AES-256; " + "; ".join(parts) if parts else "Encrypted"
        d = dlg.PropertiesDialog(M.properties(t.doc), t.doc.path, sec, self)
        if d.exec():
            t.run(M.set_properties, t.doc, d.values())

    def about(self):
        QMessageBox.about(self, f"About {APP_NAME}",
                          f"<b>{APP_NAME}</b> {__version__}<br>by 804 Industries<br><br>"
                          "A PDF viewer and editor.<br><br>Uses PyMuPDF/MuPDF (AGPL-3.0) and Qt for Python "
                          "(LGPL-3.0). See LICENSE-NOTICE.txt.")
