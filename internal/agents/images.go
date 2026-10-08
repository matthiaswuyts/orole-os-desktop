package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register the decoders previewArtifact relies on
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Image tasks run a Codex session whose only job is to call Codex's image generation tool. They
// live in the "images" group in a scratch directory that is not a repository.
const (
	imageGroup       = "images"
	imagePromptMax   = 8 << 10
	imageTitleMax    = 80
	imagePixelLimit  = 64 << 20
	artifactMaxBytes = 3 << 20 // IPC frames are 4 MiB; base64 inflates artifacts by 4/3
)

// StartImage starts a read-only Codex session that generates one image from prompt. The finished
// image arrives as an EvImage event and is read with Artifact.
func (m *Manager) StartImage(ctx context.Context, prompt string) (Session, error) {
	prompt = strings.TrimSpace(prompt)
	switch {
	case prompt == "":
		return Session{}, errors.New("describe the image to generate")
	case len(prompt) > imagePromptMax:
		return Session{}, fmt.Errorf("image prompt must be at most %d KiB", imagePromptMax>>10)
	case strings.ContainsRune(prompt, 0):
		return Session{}, errors.New("image prompt cannot contain NUL")
	case m.cfg.Host == nil || m.cfg.Host.DataDir == "":
		return Session{}, errors.New("image generation needs the daemon data directory")
	}
	dir := filepath.Join(m.cfg.Host.DataDir, "images-workdir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Session{}, err
	}
	return m.Start(ctx, StartSpec{
		Model:          m.codexDefaultModel(ctx),
		Kind:           "codex",
		Mode:           ModeStructured,
		Dir:            dir,
		Title:          "Image: " + truncate(oneLine(prompt, imageTitleMax), imageTitleMax),
		Group:          imageGroup,
		PermissionMode: "plan", // read-only sandbox: generating an image needs no file access
		Prompt: "Generate one image with your built-in image generation tool. Do not write code, run commands or edit files; " +
			"call the image generation tool directly and reply with one short sentence when it is done.\n\nImage to generate:\n" + prompt,
	})
}

// codexDefaultModel is the model Codex reports as the signed-in account's default ("" when it
// cannot be listed, which leaves the choice to the user's Codex config). Image sessions use it
// because a stale model in ~/.codex/config.toml would otherwise fail every image request, and the
// chat model has no bearing on the image itself.
func (m *Manager) codexDefaultModel(ctx context.Context) string {
	for _, d := range m.Detect(ctx, false) {
		if d.Kind != "codex" || !d.Installed {
			continue
		}
		models, err := codexModels(ctx, d.Path)
		if err != nil {
			m.cfg.Log.Debug("codex default model unavailable", "err", err)
			return ""
		}
		for _, mo := range models {
			if mo.Default {
				return mo.ID
			}
		}
	}
	return ""
}

// Artifact returns an image a session produced. Only files inside the session's own scratch
// directory are served: the path is resolved (symlinks included) and must stay inside it, must be
// a regular file, and must be a real image by magic bytes. Images over the IPC frame budget are
// returned as a downscaled JPEG preview.
func (m *Manager) Artifact(id, path string) (Artifact, error) {
	if m.cfg.Host == nil || m.cfg.Host.DataDir == "" {
		return Artifact{}, errors.New("artifacts are unavailable")
	}
	if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\:`) || strings.Contains(id, "..") {
		return Artifact{}, ErrNotFound
	}
	if _, err := m.Get(id); err != nil {
		return Artifact{}, err
	}
	if path == "" || strings.ContainsRune(path, 0) {
		return Artifact{}, errors.New("artifact path is required")
	}
	root, err := filepath.EvalSymlinks(filepath.Join(m.cfg.Host.DataDir, "agents", id))
	if err != nil {
		return Artifact{}, errors.New("this session has no artifacts")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return Artifact{}, errors.New("artifact not found")
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return Artifact{}, errors.New("artifact is outside this session's directory")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return Artifact{}, errors.New("artifact not found")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Artifact{}, errors.New("artifact is not a regular file")
	}
	if info.Size() > codexMaxImage {
		return Artifact{}, fmt.Errorf("artifact is larger than %d MiB", codexMaxImage>>20)
	}
	data, err := io.ReadAll(io.LimitReader(f, codexMaxImage+1))
	if err != nil || len(data) > codexMaxImage {
		return Artifact{}, errors.New("artifact could not be read")
	}
	mime, _ := imageKind(data)
	if mime == "" {
		return Artifact{}, errors.New("only image artifacts are served")
	}
	if len(data) > artifactMaxBytes {
		return previewArtifact(data, mime)
	}
	return Artifact{Mime: mime, Data: data}, nil
}

// previewArtifact downscales an image that does not fit one IPC frame to a JPEG that does. Only
// formats the standard library decodes can be previewed.
func previewArtifact(data []byte, mime string) (Artifact, error) {
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
	default:
		return Artifact{}, fmt.Errorf("%s image is over %d MiB and cannot be previewed", mime, artifactMaxBytes>>20)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > imagePixelLimit {
		return Artifact{}, errors.New("image is too large to preview")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Artifact{}, fmt.Errorf("image could not be decoded: %w", err)
	}
	b := src.Bounds()
	// Composite on white: JPEG has no alpha.
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, b, src, b.Min, draw.Over)
	var out image.Image = flat
	for range 6 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); err != nil {
			return Artifact{}, err
		}
		if buf.Len() <= artifactMaxBytes {
			return Artifact{Mime: "image/jpeg", Data: buf.Bytes()}, nil
		}
		out = halve(out)
	}
	return Artifact{}, errors.New("image is too large to preview")
}

// halve box-averages 2x2 pixel blocks.
func halve(src image.Image) image.Image {
	b := src.Bounds()
	w, h := max(b.Dx()/2, 1), max(b.Dy()/2, 1)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			var r, g, bl, n uint32
			for dy := range 2 {
				for dx := range 2 {
					sx, sy := b.Min.X+2*x+dx, b.Min.Y+2*y+dy
					if sx < b.Max.X && sy < b.Max.Y {
						pr, pg, pb, _ := src.At(sx, sy).RGBA()
						r, g, bl, n = r+pr, g+pg, bl+pb, n+1
					}
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 255})
		}
	}
	return dst
}
