import os

import pymupdf
import pytest

from conftest import make_form_pdf, make_pdf, png_bytes
from docket.core import annots as A
from docket.core import detect as D
from docket.core import forms as F
from docket.core import meta as M
from docket.core import pages as P
from docket.core import redact as R
from docket.core import security as S
from docket.core import text as T
from docket.core.document import Document, PasswordRequired


def test_open_edit_undo_redo():
    d = Document(make_pdf(3))
    ids = list(d.page_ids)
    P.rotate(d, [1], 90)
    assert d.pdf[1].rotation == 90
    assert d.page_ids[1] != ids[1] and d.page_ids[0] == ids[0]
    assert d.dirty and d.can_undo()
    d.undo()
    assert d.pdf[1].rotation == 0
    assert d.page_ids == ids
    d.redo()
    assert d.pdf[1].rotation == 90


def test_failed_edit_restores():
    d = Document(make_pdf(2))
    with pytest.raises(RuntimeError):
        with d.edit("boom"):
            d.pdf.delete_page(0)
            raise RuntimeError("x")
    assert d.page_count == 2
    assert not d.can_undo()


def test_save_atomic_and_adopts_path(tmp_path):
    d = Document(make_pdf(2))
    out = tmp_path / "out.pdf"
    d.save(str(out))
    assert d.path == str(out) and not d.dirty and d.title == "out.pdf"
    P.delete(d, [0])
    d.save()
    assert pymupdf.open(str(out)).page_count == 1
    assert not [f for f in os.listdir(tmp_path) if f.startswith(".docket-")]


def test_move_pages_keeps_ids():
    d = Document(make_pdf(4))
    ids = list(d.page_ids)
    P.move(d, [3], 0)
    assert d.page_ids == [ids[3], ids[0], ids[1], ids[2]]
    assert "Page 4" in d.pdf[0].get_text()
    P.move(d, [0, 1], 4)
    assert d.page_ids == [ids[1], ids[2], ids[3], ids[0]]


def test_parse_ranges():
    assert P.parse_ranges("1-3, 5, 8-", 9) == [[0, 1, 2], [4], [7, 8]]
    with pytest.raises(ValueError):
        P.parse_ranges("4-2", 9)
    with pytest.raises(ValueError):
        P.parse_ranges("12", 9)


def test_insert_extract_split_merge(tmp_path):
    a = tmp_path / "a.pdf"
    a.write_bytes(make_pdf(2))
    d = Document(make_pdf(3))
    n = P.insert_files(d, [str(a)], 1)
    assert n == 2 and d.page_count == 5 and len(d.page_ids) == 5
    P.insert_blank(d, 0)
    assert d.page_count == 6
    out = P.extract(d, [0, 2])
    assert out.page_count == 2
    written = P.split(d, P.ranges_every(2, d.page_count), str(tmp_path), "doc")
    assert len(written) == 3 and os.path.basename(written[0]) == "doc_p1-2.pdf"
    merged = P.merge([str(a), written[0]])
    assert merged.page_count == 4


def test_images_to_pdf(tmp_path):
    img = tmp_path / "x.png"
    img.write_bytes(png_bytes(400, 200))
    doc = P.image_to_pdf_doc([str(img)], fit="Letter")
    assert doc.page_count == 1
    r = doc[0].rect
    assert r.width > r.height  # wide image -> landscape page
    own = P.image_to_pdf_doc([str(img)], fit="image")
    assert own[0].rect.width == pytest.approx(300, abs=1)  # 400 px at 96 dpi


def test_export_images(tmp_path):
    d = Document(make_pdf(2))
    files = P.export_images(d, [0, 1], str(tmp_path), "p", "jpeg", 72)
    assert len(files) == 2 and all(os.path.exists(f) for f in files)


def test_text_and_search():
    d = Document(make_pdf(1))
    pt = T.extract(d.pdf[0])
    assert "quick brown fox" in pt.text
    hits = T.find(pt, T.compile_query("QUICK brown"))
    assert len(hits) == 1
    assert T.find(pt, T.compile_query("QUICK", match_case=True)) == []
    rects = pt.rects_for(*hits[0])
    assert len(rects) == 1 and rects[0][0] > 60
    i = pt.index_at(rects[0][0] + 1, (rects[0][1] + rects[0][3]) / 2)
    s, e = pt.word_at(i)
    assert pt.text[s:e] == "quick"


def test_search_across_lines():
    doc = pymupdf.open()
    p = doc.new_page()
    p.insert_text((72, 72), "the annual")
    p.insert_text((72, 86), "report is here")
    d = Document(doc.tobytes())
    pt = T.extract(d.pdf[0])
    hits = T.find(pt, T.compile_query("annual report"))
    assert len(hits) == 1
    assert len(pt.rects_for(*hits[0])) == 2


def test_annotations_roundtrip(tmp_path):
    d = Document(make_pdf(1))
    pt = T.extract(d.pdf[0])
    s, e = T.find(pt, T.compile_query("brown fox"))[0]
    A.add_markup(d, 0, A.HIGHLIGHT, pt.rects_for(s, e), (1, 1, 0), "me")
    A.add_note(d, 0, (300, 300), "hello", author="me")
    tb = A.add_textbox(d, 0, (100, 400, 300, 450), "box text", 12, (1, 0, 0), True)
    A.add_textbox(d, 0, (100, 500, 200, 520), "plain", 11, (0, 0, 0), False)
    ink = A.add_ink(d, 0, [[(100, 600), (150, 620), (200, 600)]], (0, 0, 1), 2)
    A.add_shape(d, 0, A.SQUARE, (300, 600, 400, 650), (1, 0, 0), 2)
    A.add_arrow(d, 0, (100, 700), (200, 720), (1, 0, 0), 2)
    sig = A.add_signature(d, 0, (350, 680, 500, 740), png_bytes())
    infos = A.list_page(d, 0)
    kinds = sorted(a.type_name for a in infos)
    assert "Highlight" in kinds and "Sticky Note" in kinds and "Drawing" in kinds
    assert any(a.is_signature for a in infos)
    assert any(a.is_plain_text for a in infos)
    # move ink, resize signature, edit text, restyle
    before = next(a for a in infos if a.xref == ink).rect
    A.move(d, 0, ink, 10, 20)
    after = next(a for a in A.list_page(d, 0) if a.xref == ink).rect
    assert after[0] == pytest.approx(before[0] + 10, abs=1.5)
    assert after[1] == pytest.approx(before[1] + 20, abs=1.5)
    A.resize(d, 0, sig, (350, 680, 650, 800))
    A.set_text(d, 0, tb, "changed")
    A.set_style(d, 0, tb, fontsize=18)
    info = next(a for a in A.list_page(d, 0) if a.xref == tb)
    assert info.content == "changed" and info.fontsize == pytest.approx(18)
    hit = A.hit_test(d, 0, 400, 710)
    assert hit is not None and hit.is_signature
    out = tmp_path / "a.pdf"
    d.save(str(out))
    again = Document.open_file(str(out))
    assert len(A.list_page(again, 0)) == len(infos)
    sig_info = next(a for a in A.list_page(again, 0) if a.is_signature)
    assert sig_info.rect[2] == pytest.approx(650, abs=1)
    A.delete(again, 0, [sig_info.xref])
    assert not any(a.is_signature for a in A.list_page(again, 0))


def test_annots_on_rotated_page():
    d = Document(make_pdf(1))
    P.rotate(d, [0], 90)
    x = A.add_shape(d, 0, A.SQUARE, (100, 100, 200, 150), (1, 0, 0), 2)
    info = next(a for a in A.list_page(d, 0) if a.xref == x)
    assert info.rect[0] == pytest.approx(100, abs=2) and info.rect[3] == pytest.approx(150, abs=2)


def test_forms_fill_create_flatten(tmp_path):
    d = Document(make_pdf(1))
    tx = F.add_field(d, 0, (100, 200, 300, 220), "text", "Full name")
    cx = F.add_field(d, 0, (100, 240, 114, 254), "checkbox", "Agree")
    dx = F.add_field(d, 0, (100, 280, 200, 300), "date", "Date")
    fs = {f.xref: f for f in F.fields(d, 0)}
    assert fs[tx].name == "Full_name" and fs[dx].is_date and fs[cx].kind == F.CHECKBOX
    F.set_value(d, 0, tx, "Jane Doe")
    F.toggle(d, 0, cx)
    F.set_value(d, 0, dx, F.format_date(F.parse_date("3/5/26")))
    fs = {f.xref: f for f in F.fields(d, 0)}
    assert fs[tx].value == "Jane Doe" and fs[cx].value not in ("Off", "") and fs[dx].value == "03/05/2026"
    # survives save, reorder, and keeps the date marker
    out = tmp_path / "f.pdf"
    d.save(str(out))
    d2 = Document.open_file(str(out))
    P.insert_blank(d2, 0)
    f2 = {f.name: f for f in F.fields(d2, 1)}
    assert f2["Full_name"].value == "Jane Doe" and f2["Date"].is_date
    F.update_field(d2, 1, f2["Full_name"].xref, name="Name", fontsize=10)
    assert any(f.name == "Name" and f.fontsize == 10 for f in F.fields(d2, 1))
    F.flatten(d2)
    assert not F.has_fields(d2)
    assert "Jane Doe" in d2.pdf[1].get_text()


def test_unique_field_names():
    d = Document(make_pdf(1))
    F.add_field(d, 0, (10, 10, 50, 30), "text", "Name")
    F.add_field(d, 0, (10, 40, 50, 60), "text", "Name")
    names = sorted(f.name for f in F.fields(d, 0))
    assert names == ["Name", "Name_2"]


def test_parse_dates():
    assert F.format_date(F.parse_date("3/5/26")) == "03/05/2026"
    assert F.format_date(F.parse_date("2026-03-05")) == "03/05/2026"
    assert F.format_date(F.parse_date("03.05.2026")) == "03/05/2026"
    assert F.parse_date("13/45/2026") is None
    assert F.parse_date("hello") is None


def test_detect_fields():
    d = Document(make_form_pdf())
    found = D.detect(d)
    names = {f["name"] for f in found}
    kinds = {f["name"]: f["kind"] for f in found}
    assert "Full_name" in names
    assert kinds.get("Date") == "date"
    assert "Email" in names
    assert any(f["kind"] == "checkbox" for f in found)
    n = F.add_fields_bulk(d, found)
    assert n == len(found) and len(F.fields(d, 0)) == n
    assert D.detect(d) == []  # existing fields aren't suggested again


def test_redaction_removes_text():
    d = Document(make_pdf(2))
    m = R.find_matches(d, "quick brown", emails=True, phones=True, ssns=True)
    assert set(m) == {0, 1}
    R.mark_matches(d, m)
    assert R.count_marks(d) >= 8
    d.pdf.set_metadata({"title": "Secret", "author": "Someone"})
    n = R.apply(d)
    assert n >= 8
    text = d.pdf[0].get_text()
    assert "quick brown" not in text
    assert "jane.doe@example.com" not in text
    assert "555-123-4567" not in text
    assert "123-45-6789" not in text
    assert "lazy dog" in text
    assert R.count_marks(d) == 0
    assert not d.pdf.metadata.get("title")


def test_redaction_marks_clear_and_undo():
    d = Document(make_pdf(1))
    R.add_marks(d, 0, [(50, 50, 200, 80)])
    assert R.count_marks(d) == 1
    R.clear_marks(d)
    assert R.count_marks(d) == 0
    d.undo()
    assert R.count_marks(d) == 1


def test_password_protect_and_remove(tmp_path):
    d = Document(make_pdf(1))
    S.protect(d, "open123", "owner456", {"print": True, "copy": False, "edit": False, "comment": True})
    out = tmp_path / "p.pdf"
    d.save(str(out))
    raw = out.read_bytes()
    with pytest.raises(PasswordRequired):
        Document(raw)
    with pytest.raises(PasswordRequired):
        Document(raw, password="wrong")
    d2 = Document(raw, password="open123")
    assert "quick brown" in d2.pdf[0].get_text()
    assert d2.security.user_pw == "open123"
    # saving again keeps the password
    P.rotate(d2, [0], 90)
    d2.save(str(out))
    assert S.file_info(out.read_bytes())[0] is True
    d3 = Document(out.read_bytes(), password="open123")
    assert d3.pdf[0].rotation == 90
    S.remove(d3)
    d3.save(str(out))
    assert S.file_info(out.read_bytes()) == (False, False)
    assert Document(out.read_bytes()).page_count == 1


def test_owner_password_detected():
    d = Document(make_pdf(1))
    S.protect(d, None, "owner456", {"print": True, "copy": False, "edit": False, "comment": False})
    raw = d._encoded()
    plain = Document(raw)
    assert plain.info.restricted and plain.info.extra["owner_unknown"]
    owner = Document(raw, password="owner456")
    assert owner.info.opened_as_owner


def test_compress(tmp_path):
    from docket.core import compress as C

    doc = pymupdf.open()
    from PIL import Image
    import io
    import random

    random.seed(1)
    im = Image.new("RGB", (2400, 2400))
    im.putdata([(random.randrange(256), 120, 60) for _ in range(2400 * 2400)])
    b = io.BytesIO()
    im.save(b, "PNG")
    p = doc.new_page()
    p.insert_image(pymupdf.Rect(72, 72, 300, 300), stream=b.getvalue())
    data = doc.tobytes()
    small = C.compress_bytes(data, "smallest")
    assert len(small) < len(data) / 2
    assert pymupdf.open(stream=small).page_count == 1
    lossless = C.compress_bytes(data, "lossless")
    assert pymupdf.open(stream=lossless).page_count == 1


def test_compress_in_process(tmp_path):
    from docket.core import compress as C

    out = tmp_path / "small.pdf"
    proc, conn = C.start(make_pdf(2), "lossless", str(out))
    assert conn.poll(60)
    status, size = conn.recv()
    proc.join(5)
    assert status == "ok" and out.stat().st_size == size


def test_bookmarks_and_properties():
    d = Document(make_pdf(3))
    M.set_bookmarks(d, [M.Bookmark(1, "Intro", 0, None), M.Bookmark(2, "Detail", 1, None),
                        M.Bookmark(1, "End", 2, None)])
    bms = M.bookmarks(d)
    assert [(b.level, b.title, b.page) for b in bms] == [(1, "Intro", 0), (2, "Detail", 1), (1, "End", 2)]
    M.set_properties(d, {"title": "My Doc", "author": "Jane"})
    assert M.properties(d)["title"] == "My Doc"
    P.move(d, [2], 0)
    assert [b.page for b in M.bookmarks(d)] == [1, 2, 0]


def test_links():
    doc = pymupdf.open()
    doc.new_page()
    doc.new_page()
    p = doc[0]
    p.insert_link({"kind": pymupdf.LINK_GOTO, "from": pymupdf.Rect(10, 10, 100, 30), "page": 1})
    p.insert_link({"kind": pymupdf.LINK_URI, "from": pymupdf.Rect(10, 40, 100, 60), "uri": "https://example.com"})
    d = Document(doc.tobytes())
    lks = M.links(d, 0)
    assert any(l.page == 1 for l in lks) and any(l.uri == "https://example.com" for l in lks)


def test_signature_store_and_scan():
    from docket.core import sigstore

    path = sigstore.save(png_bytes())
    items = sigstore.load_all()
    assert len(items) == 1 and items[0][1] == png_bytes()
    sigstore.delete(path)
    assert sigstore.load_all() == []
    from PIL import Image
    import io

    im = Image.new("RGB", (300, 120), "white")
    for x in range(50, 250):
        im.putpixel((x, 60), (10, 10, 10))
    b = io.BytesIO()
    im.save(b, "JPEG")
    cleaned = Image.open(io.BytesIO(sigstore.clean_scan(b.getvalue())))
    assert cleaned.mode == "RGBA" and cleaned.size[0] < 300 and cleaned.getpixel((0, 0))[3] == 0


def test_worker_renders_and_searches():
    import multiprocessing as mp

    from docket.core import worker

    ctx = mp.get_context("spawn")
    parent, child = ctx.Pipe()
    proc = ctx.Process(target=worker.main, args=(child,), daemon=True)
    proc.start()
    try:
        parent.send(("load", 1, 1, make_pdf(3)))
        parent.send(("tile", 5, 1, 1, 0, 2.0, (0, 0, 256, 256)))
        assert parent.poll(30)
        msg = parent.recv()
        assert msg[0] == "img" and msg[2] == 256 and msg[3] == 256
        parent.send(("search", 6, 1, 1, "quick brown", False, 1))
        got, done = [], None
        while done is None:
            assert parent.poll(30)
            m = parent.recv()
            if m[0] == "hits":
                got.append(m[2])
            elif m[0] == "search_done":
                done = m
        assert sorted(got) == [0, 1, 2] and got[0] == 1 and done[2] is True
    finally:
        parent.send(("quit",))
        proc.join(5)
