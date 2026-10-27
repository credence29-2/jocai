"""Page tools: reorder, rotate, delete, insert, extract, merge, split, images."""

from __future__ import annotations

import io
import os
import re

import pymupdf

from .document import Document, new_page_id

PAPER = {
    "Letter": (612, 792),
    "Legal": (612, 1008),
    "Tabloid": (792, 1224),
    "A4": (595.28, 841.89),
    "A5": (419.53, 595.28),
}

IMAGE_EXTS = {".png", ".jpg", ".jpeg", ".bmp", ".gif", ".tif", ".tiff", ".webp", ".jxr", ".pnm"}


def is_image(path: str) -> bool:
    return os.path.splitext(path)[1].lower() in IMAGE_EXTS


def parse_ranges(text: str, page_count: int) -> list[list[int]]:
    """``"1-3, 5, 8-"`` -> [[0,1,2],[4],[7..n-1]] (0-based). Raises ValueError."""
    out = []
    for part in re.split(r"[,;]", text):
        part = part.strip()
        if not part:
            continue
        m = re.fullmatch(r"(\d*)\s*-\s*(\d*)", part)
        if m:
            a = int(m.group(1)) if m.group(1) else 1
            b = int(m.group(2)) if m.group(2) else page_count
        elif part.isdigit():
            a = b = int(part)
        else:
            raise ValueError(f"Can't read “{part}”")
        if not (1 <= a <= page_count and 1 <= b <= page_count) or a > b:
            raise ValueError(f"“{part}” is outside pages 1-{page_count}")
        out.append(list(range(a - 1, b)))
    if not out:
        raise ValueError("Enter at least one page")
    return out


def flatten_ranges(ranges: list[list[int]]) -> list[int]:
    seen, out = set(), []
    for r in ranges:
        for i in r:
            if i not in seen:
                seen.add(i); out.append(i)
    return out


def rotate(doc: Document, pages: list[int], degrees: int):
    with doc.edit("Rotate Pages", pages=pages):
        for i in pages:
            p = doc.pdf[i]
            p.set_rotation((p.rotation + degrees) % 360)


def delete(doc: Document, pages: list[int]):
    if len(set(pages)) >= doc.page_count:
        raise ValueError("A PDF needs at least one page")
    with doc.edit("Delete Pages", structure=True):
        doc.pdf.delete_pages(sorted(set(pages)))
        doc.page_ids = [pid for i, pid in enumerate(doc.page_ids) if i not in set(pages)]


def move(doc: Document, pages: list[int], to: int):
    """Move ``pages`` (in their current order) so they sit before index ``to``
    (``to`` counts positions in the original list, ``page_count`` = end)."""
    pages = sorted(set(pages))
    order = list(range(doc.page_count))
    moving = [order[i] for i in pages]
    rest = [i for i in order if i not in set(pages)]
    insert_at = to - sum(1 for p in pages if p < to)
    new_order = rest[:insert_at] + moving + rest[insert_at:]
    if new_order == order:
        return
    with doc.edit("Move Pages", structure=True):
        doc.pdf.select(new_order)
        doc.page_ids = [doc.page_ids[i] for i in new_order]


def _open_source(path: str, password_cb=None) -> pymupdf.Document:
    if is_image(path):
        return image_to_pdf_doc([path], fit="image")
    src = pymupdf.open(path)
    if not src.is_pdf:
        pdf = pymupdf.open("pdf", src.convert_to_pdf())
        src.close()
        return pdf
    if src.needs_pass:
        while True:
            pw = password_cb(os.path.basename(path)) if password_cb else None
            if pw is None:
                src.close()
                raise PermissionError(f"{os.path.basename(path)} needs a password")
            if src.authenticate(pw):
                break
        # decrypted copy
        plain = pymupdf.open("pdf", src.tobytes())
        src.close()
        return plain
    return src


def insert_files(doc: Document, paths: list[str], at: int, password_cb=None) -> int:
    """Insert pages from PDFs/images before index ``at``. Returns pages added."""
    sources = [_open_source(p, password_cb) for p in paths]
    added = 0
    try:
        with doc.edit("Insert Pages", structure=True):
            pos = at
            for src in sources:
                n = src.page_count
                doc.pdf.insert_pdf(src, start_at=pos)
                doc.page_ids[pos:pos] = [new_page_id() for _ in range(n)]
                pos += n
                added += n
    finally:
        for s in sources:
            s.close()
    return added


def insert_blank(doc: Document, at: int, like: int | None = None):
    w, h = doc.page_size(like if like is not None else max(0, at - 1)) if doc.page_count else PAPER["Letter"]
    with doc.edit("Insert Blank Page", structure=True):
        doc.pdf.new_page(pno=at, width=w, height=h)
        doc.page_ids.insert(at, new_page_id())


def extract(doc: Document, pages: list[int]) -> pymupdf.Document:
    out = pymupdf.open()
    for i in pages:
        out.insert_pdf(doc.pdf, from_page=i, to_page=i)
    return out


def merge(paths: list[str], password_cb=None) -> pymupdf.Document:
    out = pymupdf.open()
    for p in paths:
        src = _open_source(p, password_cb)
        try:
            out.insert_pdf(src)
        finally:
            src.close()
    return out


def split(doc: Document, ranges: list[list[int]], folder: str, stem: str) -> list[str]:
    written = []
    for r in ranges:
        part = pymupdf.open()
        for i in r:
            part.insert_pdf(doc.pdf, from_page=i, to_page=i)
        label = f"p{r[0] + 1}" if len(r) == 1 else f"p{r[0] + 1}-{r[-1] + 1}"
        path = os.path.join(folder, f"{stem}_{label}.pdf")
        part.save(path, garbage=3, deflate=True)
        part.close()
        written.append(path)
    return written


def ranges_every(n: int, page_count: int) -> list[list[int]]:
    return [list(range(i, min(i + n, page_count))) for i in range(0, page_count, n)]


def _upright_image_bytes(path: str) -> bytes:
    """Apply the EXIF orientation so phone photos come out upright."""
    with open(path, "rb") as fh:
        data = fh.read()
    try:
        from PIL import Image, ImageOps
    except ImportError:
        return data
    try:
        im = Image.open(io.BytesIO(data))
        orient = im.getexif().get(0x0112, 1)
        if orient in (1, None) and im.format in ("JPEG", "PNG"):
            return data
        im = ImageOps.exif_transpose(im)
        if im.mode not in ("RGB", "RGBA", "L"):
            im = im.convert("RGBA" if "A" in im.mode else "RGB")
        buf = io.BytesIO()
        if im.mode == "RGBA":
            im.save(buf, "PNG")
        else:
            im.save(buf, "JPEG", quality=92)
        return buf.getvalue()
    except Exception:
        return data


def image_to_pdf_doc(paths: list[str], fit: str = "Letter", margin: float = 0) -> pymupdf.Document:
    """One image per page. ``fit`` is a PAPER key (image centred, scaled to fit,
    page turned landscape for wide images) or ``"image"`` for the image's own size."""
    out = pymupdf.open()
    for p in paths:
        data = _upright_image_bytes(p)
        img = pymupdf.open(stream=data, filetype=os.path.splitext(p)[1].lstrip(".") or "png")
        try:
            rect = img[0].rect
        finally:
            img.close()
        if fit == "image" or fit not in PAPER:
            page = out.new_page(width=rect.width, height=rect.height)
            page.insert_image(page.rect, stream=data)
        else:
            w, h = PAPER[fit]
            if rect.width > rect.height:
                w, h = h, w
            page = out.new_page(width=w, height=h)
            box = pymupdf.Rect(margin, margin, w - margin, h - margin)
            page.insert_image(box, stream=data, keep_proportion=True)
    return out


def export_images(doc: Document, pages: list[int], folder: str, stem: str, fmt: str = "png",
                  dpi: int = 150, progress=None) -> list[str]:
    fmt = fmt.lower()
    written = []
    for n, i in enumerate(pages):
        pix = doc.pdf[i].get_pixmap(dpi=dpi, alpha=False)
        path = os.path.join(folder, f"{stem}_p{i + 1}.{ 'jpg' if fmt in ('jpg', 'jpeg') else 'png'}")
        if fmt in ("jpg", "jpeg"):
            pix.save(path, jpg_quality=90)
        else:
            pix.save(path)
        written.append(path)
        if progress and not progress(n + 1, len(pages)):
            break
    return written


def new_blank(paper: str = "Letter", landscape: bool = False, count: int = 1) -> Document:
    w, h = PAPER[paper]
    if landscape:
        w, h = h, w
    return Document.blank(w, h, count)


def render_page(doc: Document, index: int, dpi: int) -> tuple[int, int, int, bytes]:
    """RGB render for printing: (width, height, stride, samples)."""
    pix = doc.pdf[index].get_pixmap(dpi=dpi, alpha=False, annots=True)
    return pix.width, pix.height, pix.stride, bytes(pix.samples)


def to_pdf_document(path: str) -> pymupdf.Document:
    """Images (or other formats MuPDF reads) as a new PDF."""
    return _open_source(path)
