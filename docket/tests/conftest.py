import io
import os
import sys

import pymupdf
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "src"))


@pytest.fixture(autouse=True)
def _isolate(tmp_path, monkeypatch):
    monkeypatch.setenv("DOCKET_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setenv("DOCKET_SETTINGS_DIR", str(tmp_path / "settings"))
    monkeypatch.setenv("DOCKET_MULTI_INSTANCE", "1")


def make_pdf(pages=3, text=True) -> bytes:
    doc = pymupdf.open()
    for i in range(pages):
        p = doc.new_page(width=612, height=792)
        if text:
            p.insert_text((72, 72), f"Page {i + 1} heading", fontsize=16)
            p.insert_text((72, 110), "The quick brown fox jumps over the lazy dog.", fontsize=11)
            p.insert_text((72, 126), "Contact jane.doe@example.com or 555-123-4567.", fontsize=11)
            p.insert_text((72, 142), "SSN 123-45-6789 is confidential.", fontsize=11)
    return doc.tobytes()


def make_form_pdf() -> bytes:
    doc = pymupdf.open()
    p = doc.new_page()
    p.insert_text((72, 100), "Full name:", fontsize=11)
    p.draw_line((140, 102), (400, 102))
    p.insert_text((72, 140), "Date:", fontsize=11)
    p.draw_line((110, 142), (250, 142))
    p.insert_text((72, 180), "Email: ______________________", fontsize=11)
    p.draw_rect(pymupdf.Rect(72, 210, 82, 220))
    p.insert_text((88, 219), "I agree", fontsize=11)
    return doc.tobytes()


def png_bytes(w=200, h=80) -> bytes:
    from PIL import Image, ImageDraw

    im = Image.new("RGBA", (w, h), (255, 255, 255, 0))
    ImageDraw.Draw(im).line((10, h - 10, w - 10, 10), fill=(0, 0, 128, 255), width=5)
    b = io.BytesIO()
    im.save(b, "PNG")
    return b.getvalue()
