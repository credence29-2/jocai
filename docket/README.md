# Docket

A Windows PDF viewer and editor by 804 Industries, laid out like Adobe Acrobat. Built with Python, PySide6 (Qt) and PyMuPDF.

## What's in it

| Area | What it does |
| --- | --- |
| View | Tabs, smooth continuous scroll, zoom, page thumbnails, bookmarks, clickable links, find (with match case), select and copy text, print, full screen, light and dark theme, recent files, remembered window and zoom |
| Comment | Highlight, underline, strikethrough, sticky notes, text boxes, plain text, freehand drawing, rectangles, ovals, arrows, lines, stamps. Move, resize, restyle, edit or delete any of them. A Comments panel lists them all |
| Fill & Sign | Fill any AcroForm (text, checkboxes, radio buttons, dropdowns, dates). Type anywhere on flat forms. Draw or import a signature and place it |
| Organize Pages | Drag to reorder, rotate, delete, insert from files or images, insert blank, extract, split, combine files, images to PDF, export pages as PNG or JPEG, bookmarks |
| Prepare a Form | Add text, checkbox and date fields, detect fields on flat PDFs automatically, rename, flatten |
| Redact | Mark text or areas, find and redact words, emails, phone numbers and SSNs, then apply. Applying really removes the content, it doesn't just cover it |
| Protect | AES-256 open password and permissions password (print, copy, edit, comment), remove password |
| Compress | Saves a smaller copy: lossless, balanced (150 dpi images) or smallest (96 dpi) |

Nothing is written to disk until you save, and every change can be undone (50 steps per document).

## Layout

It follows current Acrobat:

- **All tools** pane on the left (Shift+F4). Pick a group (Comment, Fill & Sign, Organize Pages, Prepare a Form, Redact, Protect, Compress) to see its tools. Comment also has colour, line width and font size controls that apply to the active tool or the selected comment.
- **Quick tools** across the top: open, save, print, undo, redo, select, hand, sticky note, highlight, draw, add text, sign.
- **Right rail** (F4) with page thumbnails, bookmarks and comments.
- **Page and zoom controls** float at the bottom right of the document.
- A hint bar above the page says what the active tool does, with a Done button.

## Shortcuts

Shortcuts match Acrobat where Acrobat has one.

| Action | Keys |
| --- | --- |
| Open / New / Save / Save As | Ctrl+O / Ctrl+N / Ctrl+S / Ctrl+Shift+S |
| Print / Properties / Close / Exit | Ctrl+P / Ctrl+D / Ctrl+W / Ctrl+Q |
| Undo / Redo | Ctrl+Z / Ctrl+Y or Ctrl+Shift+Z |
| Find / Next / Previous | Ctrl+F / F3 or Enter / Shift+F3 or Shift+Enter |
| Zoom in / out | Ctrl+= / Ctrl+- or Ctrl+scroll wheel |
| Fit page / Actual size / Fit width | Ctrl+0 / Ctrl+1 / Ctrl+2 |
| Go to page | Ctrl+Shift+N |
| Next / previous page | Ctrl+Down / Ctrl+Up, PgDn / PgUp, Space / Shift+Space |
| First / last page | Ctrl+Home / Ctrl+End |
| Select tool / Hand tool | V / H (middle-drag pans in any tool) |
| Rotate pages | Ctrl+Shift+- / Ctrl+Shift+= |
| Delete pages / Insert pages | Ctrl+Shift+D / Ctrl+Shift+I |
| Add bookmark | Ctrl+B |
| All tools pane / Right pane | Shift+F4 / F4 |
| Full screen | Ctrl+L (Esc to leave) |
| Preferences | Ctrl+K |
| Next / previous tab | Ctrl+Tab / Ctrl+Shift+Tab |

Text selection: drag, double-click for a word, triple-click for a line, Shift+click to extend. Esc steps back out of whatever you're doing.

## How things work

**Comments.** Pick a tool from Comment or the quick tools. Highlight, underline and strikethrough work on selected text right away, or drag over text with the tool on. Markup, pen, Add Text and redaction tools stay on until Esc. Shapes, notes and text boxes go back to Select after one use, like Acrobat. Click a comment to select it, drag to move, drag the square handles to resize, double-click to edit its text, Delete to remove, right-click for colour, width and font size. Your name on comments comes from Preferences.

**Forms.** Click a field to fill it. Enter (or clicking elsewhere) saves, Esc cancels, Tab and Shift+Tab move between fields. Date fields take `3/5/26`, `2026-03-05` and so on and store MM/DD/YYYY, or use the calendar button. **Detect Form Fields** looks for blank underlines, `____` runs, empty table cells, "Label:" cells and checkbox squares, and shows green suggestions. Click one to reject it, then **Create N Fields**. Fields are named after the label next to them, and labels with "date" become date fields.

**Signatures.** Fill & Sign > Sign Yourself. Draw one with the mouse or import a photo or scan (the white background is removed). Click to place it at a normal size or drag to size it. Corners keep the proportions when resizing. Saved signatures are encrypted with Windows DPAPI in `%LOCALAPPDATA%\Docket\signatures`, so only your Windows account on that PC can read them. **These are visual signatures, not certificate-based digital signatures.**

**Redaction.** A black box drawn over text is not redaction, because the text underneath can still be copied out. Mark areas (Mark for Redaction or Find and Redact), review the red boxes, then **Apply Redactions**. That deletes the text, the covered parts of images, and any notes, fields and links under the marks, and by default drawings that touch them and the document properties. You can undo until you save. If you save with marks that aren't applied, Docket asks first.

**Passwords.** Protect a PDF sets an open password and/or a permissions password. Both take effect when you save. Files you open with a password keep it when you save. Remove Password asks for the permissions password if the file has one and you opened it without it.

**Signed PDFs.** Saving a PDF that has digital signatures invalidates them. Docket warns you first.

Opening a PDF while Docket is running opens it as a new tab in the existing window. Errors are logged to `%LOCALAPPDATA%\Docket\logs\docket.log` (Help > Open Log Folder). Settings are in `HKCU\Software\804 Industries\Docket`.

## Not in Acrobat Pro parity yet

These are the main gaps, roughly in order of how often people need them:

1. **OCR** for scanned documents (make them searchable and selectable).
2. **Edit existing text and images** in the page content. Acrobat's Edit PDF. This is the hardest one.
3. **Export to Word, Excel or PowerPoint.**
4. **Headers, footers, watermarks, page numbers and Bates numbering.**
5. **Certificate-based digital signatures** and signature validation.
6. **Compare files**, accessibility checks, preflight and PDF/A.

XFA ("dynamic") forms from Adobe LiveCycle aren't supported by the engine. Most forms are standard AcroForms and work. List boxes are single-select.

## Install

Grab the latest build from the **Docket build** workflow's artifacts on GitHub (Actions > Docket build > latest run > Docket).

- `Docket-Setup-<version>.exe` installs for the current user by default (no admin prompt) into `%LOCALAPPDATA%\Programs\Docket`. Choose "Install for all users" in the first dialog for `C:\Program Files`.
- `Docket-portable.exe` is a single file you can run from anywhere. It starts a couple of seconds slower because it unpacks itself first.

Windows shows a SmartScreen warning the first time because the exe isn't code-signed. Click **More info** then **Run anyway**.

### Make it the default PDF app

Windows won't let installers take over file types silently, so do this once after installing (with "Register as an option for opening PDF files" ticked, which it is by default):

- **Windows 11:** Settings > Apps > Default apps, search for **Docket**, click **.pdf**, choose Docket, **Set default**. The installer's last page has a checkbox that opens this.
- **Windows 10:** Settings > Apps > Default apps > Choose default apps by file type, find **.pdf**, choose **Docket**.
- **Either:** right-click a PDF > Open with > Choose another app > Docket, tick **Always use this app**.

## Building

You need Windows, Python 3.12 and Inno Setup 6:

```powershell
winget install Python.Python.3.12
winget install JRSoftware.InnoSetup
powershell -ExecutionPolicy Bypass -File build.ps1
```

That creates `.venv`, installs the pinned dependencies, runs the tests, builds `dist\Docket\Docket.exe` (folder) and `dist\Docket-portable.exe`, smoke-tests the exe, and builds `dist\Docket-Setup-<version>.exe`. Options: `-SkipTests`, `-NoInstaller`, `-Version x.y.z`. The version lives in `src\docket\__init__.py`.

Run from source on any OS:

```
python -m venv .venv
.venv\Scripts\pip install -r requirements-dev.txt
.venv\Scripts\python run.py [file.pdf]
.venv\Scripts\python -m pytest tests
```

The icon is drawn by `scripts/make_icon.py`.

## Testing

- `tests/test_core.py` covers the engine: undo/redo, atomic saves, every page tool, text search across line wraps, every comment type (including moving, resizing and restyling, and on rotated pages), forms (fill, create, rename, flatten, dates), field detection, redaction actually removing text, passwords round-tripping, compression, bookmarks, links, signature storage, and the render worker.
- `tests/test_ui.py` drives the real window offscreen: opening, rendering, zoom, search, selecting and copying text, highlighting, page tools, saving, drawing and moving shapes, pen, signatures, notes, Add Text, detecting and filling fields, redaction and password protection, thumbnails and reordering, and launching the app end to end.
- The **Docket build** GitHub workflow runs all of it on Windows, builds the exes and installer, and smoke-tests the built exe on every push.

## Design notes

- **Engine boundary.** Every PyMuPDF call lives in `src/docket/core`. The UI never imports PyMuPDF. That keeps the AGPL engine in one place if it ever has to be swapped.
- **Rendering runs in two worker processes**, one for the page view and one for thumbnails and search. MuPDF holds Python's GIL for a whole page render, so a thread would still freeze the window on heavy pages. Only visible tiles (plus the next page) are rendered, cached up to 400 MB. If you scroll past a page that's still rendering slowly, the view worker is restarted rather than making you wait.
- **Page ids.** Each page has an id that survives reordering and changes when its content changes. The tile cache is keyed by it, so reordering pages costs nothing and an edit only re-renders that page.
- **Editing.** Documents are read fully into memory, so the file is never locked and Save can replace it safely (temp file, then an atomic swap). Each edit snapshots the document for undo, up to 50 steps or 500 MB.
- **Encrypted PDFs.** The working copy is decrypted in memory, and protection is re-applied on save. Never read `Document.needs_pass` on a document after unlocking it: in some MuPDF versions that corrupts the crypt state and the next save can't be decrypted. `security.file_info()` checks a fresh copy instead.
- **Coordinates.** Text, comments and fields use unrotated page coordinates. The page view converts through each page's rotation matrix, so every tool works on rotated pages.
- **Compression** runs in a helper process with a cancellable progress dialog.
- **Test hooks:** `DOCKET_SETTINGS_DIR` (ini file instead of the registry), `DOCKET_DATA_DIR`, `DOCKET_EXIT_AFTER_MS` (auto-quit), `DOCKET_SMOKE_SCREENSHOT`, `DOCKET_MULTI_INSTANCE=1` (don't hand off to a running window), `DOCKET_DEBUG` (log to the console).

## Licence

Docket uses **PyMuPDF, which is AGPL-3.0**. That's fine for internal business use. If Docket is ever distributed outside the company, sold, or offered over a network, either its full source code has to be made available to those users under the AGPL, or a commercial PyMuPDF licence has to be bought from Artifex. Plan on the Artifex licence if this goes to clients. See `LICENSE-NOTICE.txt` for all third-party licences.
