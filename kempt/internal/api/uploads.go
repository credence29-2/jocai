package api

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/credence29-2/jocai/kempt/internal/store"
	_ "golang.org/x/image/webp"
)

func (s *Server) uploadDir() string { return filepath.Join(s.st.DataDir, "uploads") }

// safeName keeps a file name readable but harmless on disk and in URLs.
func safeName(n string) string {
	n = path.Base(strings.ReplaceAll(n, `\`, "/"))
	var b strings.Builder
	for _, r := range n {
		switch {
		case r < 32, strings.ContainsRune(`/\:*?"<>|%#&`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ". ")
	if out == "" {
		out = "file"
	}
	if len(out) > 120 {
		ext := filepath.Ext(out)
		if len(ext) > 16 {
			ext = ""
		}
		out = out[:120-len(ext)] + ext
	}
	return out
}

// saveFile writes one upload to disk and describes it.
func (s *Server) saveFile(name string, r io.Reader, limit int64) (store.Attachment, error) {
	dir := store.RandomToken(12)
	name = safeName(name)
	if err := os.MkdirAll(filepath.Join(s.uploadDir(), dir), 0o755); err != nil {
		return store.Attachment{}, err
	}
	p := filepath.Join(s.uploadDir(), dir, name)
	f, err := os.Create(p)
	if err != nil {
		return store.Attachment{}, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	size, err := io.Copy(f, io.MultiReader(bytes.NewReader(head), io.LimitReader(r, limit-int64(n)+1)))
	f.Close()
	if err == nil && size > limit {
		err = errTooBig
	}
	if err != nil {
		os.RemoveAll(filepath.Join(s.uploadDir(), dir))
		return store.Attachment{}, err
	}
	a := store.Attachment{ID: store.NewID(), Filename: name, Size: size, ContentType: http.DetectContentType(head), URL: "/uploads/" + dir + "/" + name}
	// DetectContentType doesn't know some media types by content; trust the
	// extension for the ones browsers can play.
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); a.ContentType == "application/octet-stream" || strings.HasPrefix(a.ContentType, "text/plain") {
		if strings.HasPrefix(t, "video/") || strings.HasPrefix(t, "audio/") {
			a.ContentType = t
		}
	}
	if strings.HasPrefix(a.ContentType, "image/") {
		if f, err := os.Open(p); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				a.Width, a.Height = cfg.Width, cfg.Height
			}
			f.Close()
		}
	}
	return a, nil
}

type uploadErr string

func (e uploadErr) Error() string { return string(e) }

const errTooBig = uploadErr("file too big")

// receiveFiles saves every file in a multipart message post.
func (s *Server) receiveFiles(w http.ResponseWriter, r *http.Request) ([]store.Attachment, bool) {
	limit := int64(s.cfg.MaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit*10+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, "bad upload")
		return nil, false
	}
	var out []store.Attachment
	cleanup := func() { s.removeAttachments(out) }
	r.Form = map[string][]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			badRequest(w, "upload was cut off")
			return nil, false
		}
		if part.FileName() == "" {
			b, _ := io.ReadAll(io.LimitReader(part, 64<<10))
			r.Form[part.FormName()] = []string{string(b)}
			continue
		}
		if len(out) >= 10 {
			cleanup()
			badRequest(w, "up to 10 files per message")
			return nil, false
		}
		a, err := s.saveFile(part.FileName(), part, limit)
		if err != nil {
			cleanup()
			if err == errTooBig {
				httpError(w, http.StatusRequestEntityTooLarge, "files are limited to "+itoa(s.cfg.MaxUploadMB)+" MB each")
			} else {
				fail(w, err)
			}
			return nil, false
		}
		out = append(out, a)
	}
	return out, true
}

// receiveImage takes a single image (avatar or server icon).
func (s *Server) receiveImage(w http.ResponseWriter, r *http.Request) (store.Attachment, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "send an image under 8 MB")
		return store.Attachment{}, false
	}
	defer f.Close()
	a, err := s.saveFile(hdr.Filename, f, 8<<20)
	if err != nil {
		badRequest(w, "send an image under 8 MB")
		return a, false
	}
	if a.Width == 0 {
		s.removeAttachments([]store.Attachment{a})
		badRequest(w, "that isn't a PNG, JPEG, GIF or WebP image")
		return a, false
	}
	return a, true
}

func (s *Server) removeAttachments(atts []store.Attachment) {
	for _, a := range atts {
		parts := strings.Split(strings.TrimPrefix(a.URL, "/uploads/"), "/")
		if len(parts) == 2 && parts[0] != "" && !strings.Contains(parts[0], ".") {
			os.RemoveAll(filepath.Join(s.uploadDir(), parts[0]))
		}
	}
}

// Only types that can't run script are shown inline; everything else
// downloads, so an uploaded HTML or SVG file can't act as the Kempt site.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "audio/webm": true, "audio/mp4": true,
	"application/pdf": true,
}

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request, u *store.User) {
	dir, name := r.PathValue("id"), r.PathValue("name")
	if strings.ContainsAny(dir, `./\`) || name != safeName(name) {
		fail(w, store.ErrNotFound)
		return
	}
	p := filepath.Join(s.uploadDir(), dir, name)
	f, err := os.Open(p)
	if err != nil {
		fail(w, store.ErrNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		fail(w, store.ErrNotFound)
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ct := http.DetectContentType(head[:n])
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); strings.HasPrefix(t, "video/") || strings.HasPrefix(t, "audio/") {
		if ct == "application/octet-stream" || strings.HasPrefix(ct, "text/plain") {
			ct = t
		}
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	base := strings.TrimSpace(strings.Split(ct, ";")[0])
	if inlineTypes[base] {
		h.Set("Content-Type", base)
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	} else {
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	f.Seek(0, io.SeekStart)
	http.ServeContent(w, r, "", st.ModTime(), f)
}
