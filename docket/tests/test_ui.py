"""Drive the real main window offscreen."""

import os
import time

import pytest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PySide6.QtCore import QPointF, Qt  # noqa: E402
from PySide6.QtGui import QMouseEvent  # noqa: E402
from PySide6.QtWidgets import QApplication, QMessageBox  # noqa: E402

from conftest import make_form_pdf, make_pdf, png_bytes  # noqa: E402


@pytest.fixture(scope="module")
def app():
    a = QApplication.instance() or QApplication([])
    from docket.ui import theme

    theme.apply(a, "light")
    yield a


@pytest.fixture
def win(app, monkeypatch):
    from docket.ui.mainwindow import MainWindow

    monkeypatch.setattr(QMessageBox, "question", lambda *a, **k: QMessageBox.StandardButton.Yes)
    monkeypatch.setattr(QMessageBox, "information", lambda *a, **k: QMessageBox.StandardButton.Ok)
    monkeypatch.setattr(QMessageBox, "warning", lambda *a, **k: (_ for _ in ()).throw(AssertionError(a[2])))
    w = MainWindow()
    w.resize(1200, 800)
    w.show()
    yield w
    for t in w.all_tabs():
        t.doc.dirty = False
    w.close()


def wait(app, cond, timeout=20.0):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        app.processEvents()
        if cond():
            return True
        time.sleep(0.01)
    return False


def click(view, pos: QPointF, button=Qt.MouseButton.LeftButton, release_at=None):
    vp = view.viewport()
    for typ, p in ((QMouseEvent.Type.MouseButtonPress, pos), (QMouseEvent.Type.MouseMove, release_at or pos),
                   (QMouseEvent.Type.MouseButtonRelease, release_at or pos)):
        btns = button if typ != QMouseEvent.Type.MouseButtonRelease else Qt.MouseButton.NoButton
        ev = QMouseEvent(typ, p, vp.mapToGlobal(p), button, btns, Qt.KeyboardModifier.NoModifier)
        if typ == QMouseEvent.Type.MouseButtonPress:
            view.mousePressEvent(ev)
        elif typ == QMouseEvent.Type.MouseMove:
            view.mouseMoveEvent(ev)
        else:
            view.mouseReleaseEvent(ev)
    view._click_times = []


def test_open_render_search_save(app, win, tmp_path):
    path = tmp_path / "doc.pdf"
    path.write_bytes(make_pdf(5))
    t = win.open_file(str(path))
    assert t is not None and win.tabs.count() == 1
    view = t.view
    # tiles arrive from the worker process
    assert wait(app, lambda: len(win.renderer.cache._d) > 0), "no tiles rendered"
    # zoom controls
    win.act["actual_size"].trigger()
    assert abs(view.zoom - 1.0) < 1e-6
    win.act["zoom_in"].trigger()
    assert view.zoom > 1.0
    win.act["fit_page"].trigger()
    assert view.fit_mode == "page"
    # navigation
    view.go_to_page(3)
    assert view.current_page == 3
    # search across all pages
    win.find_bar.open_bar()
    t.find("quick brown", False)
    assert wait(app, lambda: t.search_done)
    assert t.hit_count() == 5
    t.next_hit()
    assert t.view.current_hit is not None
    # select text by dragging over a line, then copy
    view.go_to_page(0)
    app.processEvents()
    a = view.to_view(0, 75, 105)
    b = view.to_view(0, 250, 105)
    click(view, a, release_at=b)
    assert view.has_selection()
    assert t.copy()
    assert "quick" in QApplication.clipboard().text()
    # highlight the selection via the toolbar action
    win.act["tool_highlight"].trigger()
    from docket.core import annots as A

    assert any(x.type == A.HIGHLIGHT for x in A.list_page(t.doc, 0))
    # page tools
    win.act["rotate_right"].trigger()
    assert t.doc.pdf[t.view.current_page].rotation == 90
    win.act["undo"].trigger()
    assert t.doc.pdf[t.view.current_page].rotation == 0
    win.act["insert_blank"].trigger()
    assert t.doc.page_count == 6
    # save
    assert win.save()
    assert not t.doc.dirty
    import pymupdf

    assert pymupdf.open(str(path)).page_count == 6


def test_tools_create_annotations(app, win, tmp_path):
    from docket.core import annots as A

    t = win.add_tab(__import__("docket.core.document", fromlist=["Document"]).Document(make_pdf(1)))
    view = t.view
    app.processEvents()
    # rectangle by dragging
    win.set_tool("rect")
    click(view, view.to_view(0, 100, 300), release_at=view.to_view(0, 200, 380))
    rects = [a for a in A.list_page(t.doc, 0) if a.type == A.SQUARE]
    assert len(rects) == 1 and abs(rects[0].rect[0] - 100) < 3
    assert view.tool == "select" and view.sel_annot is not None
    # move it by dragging the selected annotation
    r = rects[0].rect
    centre = view.to_view(0, (r[0] + r[2]) / 2, (r[1] + r[3]) / 2)
    click(view, centre, release_at=centre + QPointF(30, 0))
    moved = [a for a in A.list_page(t.doc, 0) if a.type == A.SQUARE][0]
    assert moved.rect[0] > r[0] + 5
    # pen stroke
    win.set_tool("pen")
    click(view, view.to_view(0, 100, 500), release_at=view.to_view(0, 220, 560))
    assert any(a.type == A.INK for a in A.list_page(t.doc, 0))
    # signature placement
    win.start_signature(png_bytes())
    assert view.tool == "sign"
    click(view, view.to_view(0, 300, 650))
    assert any(a.is_signature for a in A.list_page(t.doc, 0))
    # sticky note through the inline editor
    win.set_tool("note")
    click(view, view.to_view(0, 400, 200))
    ed = t._editor
    assert ed is not None
    ed.setPlainText("Check this")
    ed.commit()
    notes = [a for a in A.list_page(t.doc, 0) if a.type == A.NOTE]
    assert notes and notes[0].content == "Check this"
    # add text tool
    win.set_tool("addtext")
    click(view, view.to_view(0, 72, 700))
    t._editor.setPlainText("Plain words")
    t._editor.commit()
    assert any(a.is_plain_text and a.content == "Plain words" for a in A.list_page(t.doc, 0))
    # delete key removes the selected annotation
    sig = next(a for a in A.list_page(t.doc, 0) if a.is_signature)
    view.select_annot(sig)
    win.act["delete"].trigger()
    assert not any(a.is_signature for a in A.list_page(t.doc, 0))
    # comments panel lists them
    win.show_right(2)
    app.processEvents()
    assert win.comments.list.count() >= 4


def test_form_fill_and_detect(app, win, tmp_path, monkeypatch):
    from docket.core import forms as F
    from docket.core.document import Document
    from docket.ui import dialogs

    t = win.add_tab(Document(make_form_pdf()))
    app.processEvents()
    win.detect_fields()
    assert t.view.suggestions
    n = len(t.view.suggestions)
    # reject one by clicking it
    s0 = t.view.suggestions[0]
    r = s0["rect"]
    click(t.view, t.view.to_view(0, (r[0] + r[2]) / 2, (r[1] + r[3]) / 2))
    assert s0["accepted"] is False
    win._create_detected(t)
    fields = F.fields(t.doc, 0)
    assert len(fields) == n - 1
    # fill a text field by clicking it and typing
    tf = next(f for f in fields if f.kind == F.TEXT and not f.is_date)
    click(t.view, t.view.to_view(0, tf.rect[0] + 5, (tf.rect[1] + tf.rect[3]) / 2))
    ed = t._editor
    assert ed is not None
    ed.setText("Jane Doe")
    ed.finish(0)
    assert any(f.value == "Jane Doe" for f in F.fields(t.doc, 0))
    # date field normalises the value
    df = next((f for f in F.fields(t.doc, 0) if f.is_date), None)
    if df is not None:
        click(t.view, t.view.to_view(0, df.rect[0] + 3, (df.rect[1] + df.rect[3]) / 2))
        t._editor.line.setText("3/5/26")
        t._editor.finish(0)
        assert any(f.value == "03/05/2026" for f in F.fields(t.doc, 0))
    # new field via the tool and the dialog
    monkeypatch.setattr(dialogs.FieldDialog, "exec", lambda self: 1)
    win.set_tool("field_text")
    click(t.view, t.view.to_view(0, 300, 600), release_at=t.view.to_view(0, 450, 620))
    assert len(F.fields(t.doc, 0)) == n
    # checkbox toggles on click
    cb = next(f for f in F.fields(t.doc, 0) if f.kind == F.CHECKBOX)
    win.set_tool("select")
    click(t.view, t.view.to_view(0, (cb.rect[0] + cb.rect[2]) / 2, (cb.rect[1] + cb.rect[3]) / 2))
    cb2 = next(f for f in F.fields(t.doc, 0) if f.xref == cb.xref)
    assert cb2.value not in ("Off", "")


def test_redact_and_protect_flow(app, win, tmp_path, monkeypatch):
    from docket.core import redact as R
    from docket.core.document import Document
    from docket.ui import dialogs

    t = win.add_tab(Document(make_pdf(2)))
    monkeypatch.setattr(dialogs.FindRedactDialog, "exec", lambda self: 1)
    monkeypatch.setattr(dialogs.FindRedactDialog, "values",
                        lambda self: dict(query="lazy dog", match_case=False, whole_word=False, emails=True,
                                          phones=False, ssns=False))
    win.find_and_redact()
    assert R.count_marks(t.doc) == 4
    monkeypatch.setattr(dialogs.ApplyRedactionsDialog, "exec", lambda self: 1)
    win.apply_redactions()
    assert R.count_marks(t.doc) == 0
    assert "lazy dog" not in t.doc.pdf[0].get_text()
    monkeypatch.setattr(dialogs.ProtectDialog, "exec", lambda self: 1)
    monkeypatch.setattr(dialogs.ProtectDialog, "values", lambda self: ("pw", None, None))
    win.protect()
    path = tmp_path / "secret.pdf"
    monkeypatch.setattr("PySide6.QtWidgets.QFileDialog.getSaveFileName", lambda *a, **k: (str(path), ""))
    assert win.save()
    from docket.core import security as S

    assert S.file_info(path.read_bytes())[0]


def test_page_reorder_and_thumbnails(app, win):
    from docket.core.document import Document

    t = win.add_tab(Document(make_pdf(4)))
    win.show_right(0)
    assert wait(app, lambda: win.thumbs.thumb(0) is not None), "no thumbnail"
    win.move_pages([3], 0)
    assert "Page 4" in t.doc.pdf[0].get_text()
    assert win.thumbs.list.count() == 4


def test_smoke_main(tmp_path, monkeypatch):
    """The real entry point opens a file and exits by itself."""
    import subprocess
    import sys

    path = tmp_path / "doc.pdf"
    path.write_bytes(make_pdf(2))
    shot = tmp_path / "shot.png"
    env = dict(os.environ, DOCKET_EXIT_AFTER_MS="2500", DOCKET_SMOKE_SCREENSHOT=str(shot),
               QT_QPA_PLATFORM="offscreen", DOCKET_MULTI_INSTANCE="1")
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    r = subprocess.run([sys.executable, os.path.join(root, "run.py"), str(path)], env=env, capture_output=True,
                       text=True, timeout=120)
    assert r.returncode == 0, r.stderr
    assert "smoke ok: 1 tab(s)" in r.stdout
    assert shot.exists() and shot.stat().st_size > 10000
