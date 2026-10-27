"""Dialogs."""

from __future__ import annotations

import io
import os

from PySide6.QtCore import QPointF, QRectF, QSize, Qt, Signal
from PySide6.QtGui import QColor, QImage, QPainter, QPainterPath, QPen, QPixmap
from PySide6.QtWidgets import (QAbstractItemView, QButtonGroup, QCheckBox, QComboBox, QDialog,
                               QDialogButtonBox, QFileDialog, QFormLayout, QFrame, QGridLayout,
                               QGroupBox, QHBoxLayout, QLabel, QLineEdit, QListWidget, QListWidgetItem,
                               QMessageBox, QPushButton, QRadioButton, QSpinBox, QTabWidget, QVBoxLayout,
                               QWidget)

from ..core import pages as P
from ..core import sigstore
from . import icons, settings

PDF_FILTER = "PDF files (*.pdf)"
OPEN_FILTER = "PDF and image files (*.pdf *.png *.jpg *.jpeg *.bmp *.gif *.tif *.tiff *.webp);;PDF files (*.pdf);;All files (*)"
IMAGE_FILTER = "Images (*.png *.jpg *.jpeg *.bmp *.gif *.tif *.tiff *.webp)"


def buttons(dlg: QDialog, ok_text="OK", cancel=True) -> QDialogButtonBox:
    bb = QDialogButtonBox()
    ok = bb.addButton(ok_text, QDialogButtonBox.ButtonRole.AcceptRole)
    ok.setDefault(True)
    if cancel:
        bb.addButton("Cancel", QDialogButtonBox.ButtonRole.RejectRole)
    bb.accepted.connect(dlg.accept)
    bb.rejected.connect(dlg.reject)
    return bb


def last_dir(key="last_dir") -> str:
    d = settings.get(key, "")
    return d if d and os.path.isdir(d) else os.path.expanduser("~")


def remember_dir(path: str, key="last_dir"):
    if path:
        settings.put(key, os.path.dirname(path) if os.path.isfile(path) else path)


class NewDocumentDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("New PDF")
        form = QFormLayout()
        self.paper = QComboBox()
        self.paper.addItems(list(P.PAPER))
        self.orient = QComboBox()
        self.orient.addItems(["Portrait", "Landscape"])
        self.count = QSpinBox()
        self.count.setRange(1, 500)
        self.count.setValue(1)
        form.addRow("Page size:", self.paper)
        form.addRow("Orientation:", self.orient)
        form.addRow("Pages:", self.count)
        lay = QVBoxLayout(self)
        lay.addLayout(form)
        lay.addWidget(buttons(self, "Create"))

    def values(self):
        return self.paper.currentText(), self.orient.currentIndex() == 1, self.count.value()


class RangeDialog(QDialog):
    def __init__(self, title, prompt, default, page_count, parent=None, ok="OK"):
        super().__init__(parent)
        self.setWindowTitle(title)
        self.page_count = page_count
        lay = QVBoxLayout(self)
        lay.addWidget(QLabel(prompt))
        self.edit = QLineEdit(default)
        lay.addWidget(self.edit)
        hint = QLabel(f"Pages 1-{page_count}. Examples: 2-4, 7  or  5-")
        hint.setObjectName("muted")
        lay.addWidget(hint)
        lay.addWidget(buttons(self, ok))
        self.ranges = None

    def accept(self):
        try:
            self.ranges = P.parse_ranges(self.edit.text(), self.page_count)
        except ValueError as e:
            QMessageBox.warning(self, self.windowTitle(), str(e))
            return
        super().accept()


class FileListDialog(QDialog):
    """Ordered list of files with Add / Remove / Move up / Move down."""

    def __init__(self, title, intro, ok_text, file_filter, initial=None, parent=None, extra=None):
        super().__init__(parent)
        self.setWindowTitle(title)
        self.file_filter = file_filter
        self.resize(560, 420)
        lay = QVBoxLayout(self)
        lbl = QLabel(intro)
        lbl.setWordWrap(True)
        lay.addWidget(lbl)
        row = QHBoxLayout()
        self.list = QListWidget()
        self.list.setDragDropMode(QAbstractItemView.DragDropMode.InternalMove)
        self.list.setSelectionMode(QAbstractItemView.SelectionMode.ExtendedSelection)
        self.list.setAcceptDrops(True)
        row.addWidget(self.list, 1)
        side = QVBoxLayout()
        for text, fn in (("Add Files...", self.add), ("Remove", self.remove), ("Move Up", lambda: self.shift(-1)),
                         ("Move Down", lambda: self.shift(1))):
            b = QPushButton(text)
            b.clicked.connect(fn)
            side.addWidget(b)
        side.addStretch(1)
        row.addLayout(side)
        lay.addLayout(row, 1)
        if extra is not None:
            lay.addWidget(extra)
        lay.addWidget(buttons(self, ok_text))
        for p in initial or []:
            self._append(p)
        self.setAcceptDrops(True)

    def _append(self, path):
        it = QListWidgetItem(icons.icon("image" if P.is_image(path) else "file", 16), os.path.basename(path))
        it.setData(Qt.ItemDataRole.UserRole, path)
        it.setToolTip(path)
        self.list.addItem(it)

    def add(self):
        files, _ = QFileDialog.getOpenFileNames(self, "Add Files", last_dir(), self.file_filter)
        for f in files:
            self._append(f)
        if files:
            remember_dir(files[0])

    def remove(self):
        for it in self.list.selectedItems():
            self.list.takeItem(self.list.row(it))

    def shift(self, d):
        rows = sorted((self.list.row(i) for i in self.list.selectedItems()), reverse=d > 0)
        for r in rows:
            nr = r + d
            if 0 <= nr < self.list.count():
                it = self.list.takeItem(r)
                self.list.insertItem(nr, it)
                it.setSelected(True)

    def dragEnterEvent(self, ev):
        if ev.mimeData().hasUrls():
            ev.acceptProposedAction()

    def dropEvent(self, ev):
        for u in ev.mimeData().urls():
            p = u.toLocalFile()
            if p and os.path.isfile(p):
                self._append(p)

    def paths(self) -> list[str]:
        return [self.list.item(i).data(Qt.ItemDataRole.UserRole) for i in range(self.list.count())]

    def accept(self):
        if not self.paths():
            QMessageBox.information(self, self.windowTitle(), "Add at least one file.")
            return
        super().accept()


class SplitDialog(QDialog):
    def __init__(self, page_count, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Split Document")
        self.page_count = page_count
        lay = QVBoxLayout(self)
        self.by_ranges = QRadioButton("By page ranges:")
        self.ranges = QLineEdit("1-")
        self.by_n = QRadioButton("Every")
        self.n = QSpinBox()
        self.n.setRange(1, max(1, page_count))
        self.n.setValue(min(2, max(1, page_count)))
        self.each = QRadioButton("One file per page")
        self.by_ranges.setChecked(True)
        r1 = QHBoxLayout(); r1.addWidget(self.by_ranges); r1.addWidget(self.ranges, 1)
        r2 = QHBoxLayout(); r2.addWidget(self.by_n); r2.addWidget(self.n); r2.addWidget(QLabel("pages")); r2.addStretch(1)
        lay.addLayout(r1)
        hint = QLabel(f"Each range becomes its own file, e.g. 1-3, 4-10, 11-   ({page_count} pages)")
        hint.setObjectName("muted")
        lay.addWidget(hint)
        lay.addLayout(r2)
        lay.addWidget(self.each)
        lay.addWidget(buttons(self, "Choose Folder..."))
        self.result_ranges = None

    def accept(self):
        try:
            if self.by_ranges.isChecked():
                self.result_ranges = P.parse_ranges(self.ranges.text(), self.page_count)
            elif self.by_n.isChecked():
                self.result_ranges = P.ranges_every(self.n.value(), self.page_count)
            else:
                self.result_ranges = P.ranges_every(1, self.page_count)
        except ValueError as e:
            QMessageBox.warning(self, "Split Document", str(e))
            return
        super().accept()


class ExportImagesDialog(QDialog):
    def __init__(self, page_count, current, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Export Pages as Images")
        self.page_count = page_count
        form = QFormLayout(self)
        self.fmt = QComboBox()
        self.fmt.addItems(["PNG", "JPEG"])
        self.dpi = QSpinBox()
        self.dpi.setRange(72, 600)
        self.dpi.setValue(150)
        self.dpi.setSuffix(" dpi")
        self.pages = QLineEdit(f"1-{page_count}")
        form.addRow("Format:", self.fmt)
        form.addRow("Resolution:", self.dpi)
        form.addRow("Pages:", self.pages)
        form.addRow(buttons(self, "Choose Folder..."))
        self.result_pages = None

    def accept(self):
        try:
            self.result_pages = P.flatten_ranges(P.parse_ranges(self.pages.text(), self.page_count))
        except ValueError as e:
            QMessageBox.warning(self, self.windowTitle(), str(e))
            return
        super().accept()


class FieldDialog(QDialog):
    def __init__(self, title, name, fontsize=0, kind="text", parent=None):
        super().__init__(parent)
        self.setWindowTitle(title)
        form = QFormLayout(self)
        self.name = QLineEdit(name)
        form.addRow("Field name:", self.name)
        self.size = QComboBox()
        self.size.setEditable(True)
        self.size.addItems(["Auto", "8", "9", "10", "11", "12", "14", "16", "18", "24"])
        self.size.setCurrentText("Auto" if not fontsize else f"{fontsize:g}")
        if kind != "checkbox":
            form.addRow("Font size:", self.size)
        form.addRow(buttons(self))

    def values(self):
        t = self.size.currentText().strip()
        try:
            fs = 0 if t.lower() == "auto" or not t else float(t)
        except ValueError:
            fs = 0
        return self.name.text().strip(), fs


class SignaturePad(QWidget):
    changed = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self.setMinimumSize(460, 180)
        self.strokes: list[list[QPointF]] = []
        self.setCursor(Qt.CursorShape.CrossCursor)

    def clear(self):
        self.strokes = []
        self.update()
        self.changed.emit()

    def mousePressEvent(self, ev):
        self.strokes.append([ev.position()])

    def mouseMoveEvent(self, ev):
        if self.strokes and ev.buttons() & Qt.MouseButton.LeftButton:
            self.strokes[-1].append(ev.position())
            self.update()

    def mouseReleaseEvent(self, ev):
        self.changed.emit()

    def _path(self, scale=1.0, offset=QPointF(0, 0)) -> QPainterPath:
        path = QPainterPath()
        for s in self.strokes:
            if not s:
                continue
            path.moveTo((s[0] - offset) * scale)
            if len(s) == 1:
                path.lineTo((s[0] - offset) * scale + QPointF(0.5, 0.5))
            for k in range(1, len(s)):
                a, b = s[k - 1], s[k]
                mid = (a + b) / 2
                path.quadTo((a - offset) * scale, (mid - offset) * scale)
            path.lineTo((s[-1] - offset) * scale)
        return path

    def paintEvent(self, ev):
        p = QPainter(self)
        p.fillRect(self.rect(), Qt.GlobalColor.white)
        p.setPen(QPen(QColor(200, 200, 200), 1, Qt.PenStyle.DashLine))
        y = int(self.height() * 0.75)
        p.drawLine(20, y, self.width() - 20, y)
        if not self.strokes:
            p.setPen(QColor(150, 150, 150))
            p.drawText(self.rect(), Qt.AlignmentFlag.AlignCenter, "Draw your signature here")
        p.setRenderHint(QPainter.RenderHint.Antialiasing)
        p.setPen(QPen(QColor(20, 20, 60), 2.6, Qt.PenStyle.SolidLine, Qt.PenCapStyle.RoundCap,
                      Qt.PenJoinStyle.RoundJoin))
        p.drawPath(self._path())

    def png(self) -> bytes | None:
        if not self.strokes:
            return None
        bounds = self._path().boundingRect().adjusted(-4, -4, 4, 4)
        scale = 3.0
        img = QImage(int(bounds.width() * scale) + 1, int(bounds.height() * scale) + 1,
                     QImage.Format.Format_ARGB32)
        img.fill(Qt.GlobalColor.transparent)
        p = QPainter(img)
        p.setRenderHint(QPainter.RenderHint.Antialiasing)
        p.setPen(QPen(QColor(20, 20, 60), 2.6 * scale, Qt.PenStyle.SolidLine, Qt.PenCapStyle.RoundCap,
                      Qt.PenJoinStyle.RoundJoin))
        p.drawPath(self._path(scale, bounds.topLeft()))
        p.end()
        from PySide6.QtCore import QBuffer, QByteArray, QIODevice

        ba = QByteArray()
        buf = QBuffer(ba)
        buf.open(QIODevice.OpenModeFlag.WriteOnly)
        img.save(buf, "PNG")
        return bytes(ba)


class CreateSignatureDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Create Signature")
        self.png = None
        lay = QVBoxLayout(self)
        self.tabs = QTabWidget()
        draw = QWidget()
        dl = QVBoxLayout(draw)
        self.pad = SignaturePad()
        dl.addWidget(self.pad)
        clear = QPushButton("Clear")
        clear.clicked.connect(self.pad.clear)
        dl.addWidget(clear, 0, Qt.AlignmentFlag.AlignRight)
        self.tabs.addTab(draw, "Draw")
        imp = QWidget()
        il = QVBoxLayout(imp)
        self.preview = QLabel("Choose a photo or scan of your signature on white paper.\nThe white background is removed.")
        self.preview.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.preview.setMinimumSize(460, 180)
        self.preview.setStyleSheet("background: white; color: #777;")
        il.addWidget(self.preview)
        pick = QPushButton("Choose Image...")
        pick.clicked.connect(self._pick)
        il.addWidget(pick, 0, Qt.AlignmentFlag.AlignRight)
        self.tabs.addTab(imp, "Image")
        lay.addWidget(self.tabs)
        self.save_it = QCheckBox("Save this signature for next time")
        self.save_it.setChecked(True)
        lay.addWidget(self.save_it)
        lay.addWidget(buttons(self, "Use Signature"))
        self._imported = None

    def _pick(self):
        path, _ = QFileDialog.getOpenFileName(self, "Signature Image", last_dir(), IMAGE_FILTER)
        if not path:
            return
        try:
            with open(path, "rb") as fh:
                self._imported = sigstore.clean_scan(fh.read())
        except Exception as e:
            QMessageBox.warning(self, "Create Signature", f"Couldn't read that image:\n{e}")
            return
        img = QImage.fromData(self._imported)
        self.preview.setPixmap(QPixmap.fromImage(img).scaled(QSize(440, 170), Qt.AspectRatioMode.KeepAspectRatio,
                                                             Qt.TransformationMode.SmoothTransformation))

    def accept(self):
        self.png = self.pad.png() if self.tabs.currentIndex() == 0 else self._imported
        if not self.png:
            QMessageBox.information(self, "Create Signature", "Draw or choose a signature first.")
            return
        if self.save_it.isChecked():
            try:
                sigstore.save(self.png)
            except Exception as e:
                QMessageBox.warning(self, "Create Signature", f"Couldn't save the signature:\n{e}")
        super().accept()


class FindRedactDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Find and Redact")
        lay = QVBoxLayout(self)
        form = QFormLayout()
        self.text = QLineEdit()
        self.text.setPlaceholderText("Word or phrase")
        form.addRow("Find:", self.text)
        lay.addLayout(form)
        self.case = QCheckBox("Match case")
        self.whole = QCheckBox("Whole words only")
        lay.addWidget(self.case)
        lay.addWidget(self.whole)
        grp = QGroupBox("Also find")
        gl = QVBoxLayout(grp)
        self.emails = QCheckBox("Email addresses")
        self.phones = QCheckBox("Phone numbers")
        self.ssns = QCheckBox("Social Security numbers")
        for c in (self.emails, self.phones, self.ssns):
            gl.addWidget(c)
        lay.addWidget(grp)
        lay.addWidget(buttons(self, "Mark Matches"))

    def values(self):
        return dict(query=self.text.text(), match_case=self.case.isChecked(), whole_word=self.whole.isChecked(),
                    emails=self.emails.isChecked(), phones=self.phones.isChecked(), ssns=self.ssns.isChecked())


class ApplyRedactionsDialog(QDialog):
    def __init__(self, count, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Apply Redactions")
        lay = QVBoxLayout(self)
        lbl = QLabel(f"<b>Permanently remove the content under {count} redaction mark{'s' if count != 1 else ''}?</b>"
                     "<br><br>Text, the covered parts of images, and any notes, form fields and links under the "
                     "marks are deleted and replaced with black boxes. You can undo until you save; after "
                     "that it can't be recovered.")
        lbl.setWordWrap(True)
        lay.addWidget(lbl)
        self.graphics = QCheckBox("Also remove drawings (lines, shapes) that touch a mark")
        self.graphics.setChecked(True)
        self.meta = QCheckBox("Remove document properties (title, author, etc.)")
        self.meta.setChecked(True)
        lay.addWidget(self.graphics)
        lay.addWidget(self.meta)
        lay.addWidget(buttons(self, "Apply"))


class ProtectDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Password Protect")
        lay = QVBoxLayout(self)
        self.need_open = QCheckBox("Require a password to open the document")
        lay.addWidget(self.need_open)
        f1 = QFormLayout()
        self.pw1 = QLineEdit(); self.pw1.setEchoMode(QLineEdit.EchoMode.Password)
        self.pw2 = QLineEdit(); self.pw2.setEchoMode(QLineEdit.EchoMode.Password)
        f1.addRow("Password:", self.pw1)
        f1.addRow("Confirm:", self.pw2)
        lay.addLayout(f1)
        self.restrict = QCheckBox("Restrict editing and printing (needs a permissions password)")
        lay.addWidget(self.restrict)
        f2 = QFormLayout()
        self.op1 = QLineEdit(); self.op1.setEchoMode(QLineEdit.EchoMode.Password)
        self.op2 = QLineEdit(); self.op2.setEchoMode(QLineEdit.EchoMode.Password)
        f2.addRow("Permissions password:", self.op1)
        f2.addRow("Confirm:", self.op2)
        lay.addLayout(f2)
        grp = QGroupBox("Allow")
        gl = QVBoxLayout(grp)
        self.allow = {}
        for key, label in (("print", "Printing"), ("copy", "Copying text and images"),
                           ("edit", "Changing the document (pages, content)"),
                           ("comment", "Commenting and filling in forms")):
            cb = QCheckBox(label)
            cb.setChecked(key in ("print", "comment"))
            self.allow[key] = cb
            gl.addWidget(cb)
        lay.addWidget(grp)
        note = QLabel("AES-256 encryption. Takes effect when you save.")
        note.setObjectName("muted")
        lay.addWidget(note)
        lay.addWidget(buttons(self, "Apply"))
        for w in (self.need_open, self.restrict):
            w.toggled.connect(self._sync)
        self._sync()

    def _sync(self):
        for w in (self.pw1, self.pw2):
            w.setEnabled(self.need_open.isChecked())
        for w in [self.op1, self.op2] + list(self.allow.values()):
            w.setEnabled(self.restrict.isChecked())

    def accept(self):
        if not self.need_open.isChecked() and not self.restrict.isChecked():
            QMessageBox.information(self, self.windowTitle(), "Choose at least one kind of protection.")
            return
        if self.need_open.isChecked():
            if not self.pw1.text():
                QMessageBox.warning(self, self.windowTitle(), "Enter a password.")
                return
            if self.pw1.text() != self.pw2.text():
                QMessageBox.warning(self, self.windowTitle(), "The passwords don't match.")
                return
        if self.restrict.isChecked():
            if not self.op1.text():
                QMessageBox.warning(self, self.windowTitle(), "Enter a permissions password.")
                return
            if self.op1.text() != self.op2.text():
                QMessageBox.warning(self, self.windowTitle(), "The permissions passwords don't match.")
                return
            if self.need_open.isChecked() and self.op1.text() == self.pw1.text():
                QMessageBox.warning(self, self.windowTitle(),
                                    "Use a different permissions password, otherwise anyone who can open the "
                                    "file can remove the restrictions.")
                return
        super().accept()

    def values(self):
        return (self.pw1.text() if self.need_open.isChecked() else None,
                self.op1.text() if self.restrict.isChecked() else None,
                {k: cb.isChecked() for k, cb in self.allow.items()} if self.restrict.isChecked() else None)


class CompressDialog(QDialog):
    def __init__(self, size_bytes, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Reduce File Size")
        lay = QVBoxLayout(self)
        lay.addWidget(QLabel(f"Current size: {human_size(size_bytes)}. A smaller copy is saved; the original isn't changed."))
        self.group = QButtonGroup(self)
        for k, (key, label, desc) in enumerate((
                ("lossless", "Lossless cleanup", "Recompresses and removes unused data. Nothing visible changes."),
                ("balanced", "Balanced", "Images above 200 dpi are downsampled to 150 dpi."),
                ("smallest", "Smallest", "Images above 120 dpi are downsampled to 96 dpi."))):
            rb = QRadioButton(label)
            rb.setProperty("key", key)
            if key == "balanced":
                rb.setChecked(True)
            self.group.addButton(rb, k)
            lay.addWidget(rb)
            d = QLabel(desc)
            d.setObjectName("muted")
            d.setContentsMargins(24, 0, 0, 6)
            lay.addWidget(d)
        lay.addWidget(buttons(self, "Save As..."))

    def preset(self):
        return self.group.checkedButton().property("key")


class PropertiesDialog(QDialog):
    def __init__(self, props: dict, path: str | None, security_text: str, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Document Properties")
        self.resize(480, 0)
        lay = QVBoxLayout(self)
        form = QFormLayout()
        self.edits = {}
        for key, label in (("title", "Title"), ("author", "Author"), ("subject", "Subject"), ("keywords", "Keywords")):
            e = QLineEdit(props.get(key) or "")
            self.edits[key] = e
            form.addRow(label + ":", e)
        lay.addLayout(form)
        info = QFormLayout()
        def ro(text):
            l = QLabel(text or "")
            l.setTextInteractionFlags(Qt.TextInteractionFlag.TextSelectableByMouse)
            l.setWordWrap(True)
            return l
        info.addRow("File:", ro(path or "Not saved yet"))
        if path and os.path.exists(path):
            info.addRow("File size:", ro(human_size(os.path.getsize(path))))
        info.addRow("Pages:", ro(str(props.get("pages"))))
        info.addRow("Page size:", ro(props.get("page_size", "")))
        info.addRow("PDF version:", ro(props.get("pdf_version", "")))
        info.addRow("Created by:", ro(props.get("creator", "")))
        info.addRow("PDF producer:", ro(props.get("producer", "")))
        info.addRow("Created:", ro(pdf_date(props.get("creationDate", ""))))
        info.addRow("Modified:", ro(pdf_date(props.get("modDate", ""))))
        info.addRow("Security:", ro(security_text))
        grp = QGroupBox("Advanced")
        grp.setLayout(info)
        lay.addWidget(grp)
        lay.addWidget(buttons(self))

    def values(self):
        return {k: e.text() for k, e in self.edits.items()}


class PreferencesDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("Preferences")
        form = QFormLayout(self)
        self.author = QLineEdit(settings.author())
        form.addRow("Your name (for comments):", self.author)
        self.theme = QComboBox()
        self.theme.addItems(["System setting", "Light", "Dark"])
        self.theme.setCurrentIndex(["system", "light", "dark"].index(settings.get("theme", "system")
                                                                     if settings.get("theme", "system") in
                                                                     ("system", "light", "dark") else "system"))
        form.addRow("Theme:", self.theme)
        self.zoom = QComboBox()
        self.zoom.addItems(["Fit Width", "Fit Page", "Actual Size", "Remember last zoom"])
        self.zoom.setCurrentIndex(int(settings.get("default_zoom", 0, int) or 0))
        form.addRow("Open documents at:", self.zoom)
        self.restore = QCheckBox("Reopen documents from last time")
        self.restore.setChecked(bool(settings.get("restore_session", False, bool)))
        form.addRow("", self.restore)
        form.addRow(buttons(self))

    def apply(self):
        settings.put("author", self.author.text().strip())
        settings.put("theme", ["system", "light", "dark"][self.theme.currentIndex()])
        settings.put("default_zoom", self.zoom.currentIndex())
        settings.put("restore_session", self.restore.isChecked())


def human_size(n: int) -> str:
    for unit in ("bytes", "KB", "MB", "GB"):
        if n < 1024 or unit == "GB":
            return f"{n:,.0f} {unit}" if unit == "bytes" else f"{n:,.1f} {unit}"
        n /= 1024
    return str(n)


def pdf_date(s: str) -> str:
    if not s or not s.startswith("D:") or len(s) < 10:
        return s or ""
    s = s[2:]
    try:
        y, mo, d = s[0:4], s[4:6], s[6:8]
        hh, mm = s[8:10] or "00", s[10:12] or "00"
        return f"{mo}/{d}/{y} {hh}:{mm}"
    except Exception:
        return s
