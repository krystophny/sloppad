package web

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const presentationRenderTimeout = 90 * time.Second

type presentationRenderFunc func(ctx context.Context, inputPath, outputPath string) error

func isPresentationFilePath(path string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".pptx", ".odp", ".key":
		return true
	default:
		return false
	}
}

func isPDFFilePath(path string) bool {
	return strings.EqualFold(strings.TrimSpace(filepath.Ext(path)), ".pdf")
}

func sanitizePresentationArtifactName(path string) string {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if base == "" {
		return "presentation"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "presentation"
	}
	return name
}

func presentationArtifactOutputPath(projectRoot, inputPath string) (string, string, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(projectRoot))
	if err != nil {
		return "", "", err
	}
	inputAbs, err := filepath.Abs(strings.TrimSpace(inputPath))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(rootAbs, inputAbs)
	if err != nil {
		rel = inputAbs
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	relOutput := filepath.ToSlash(filepath.Join(
		".tabura",
		"artifacts",
		"presentations",
		fmt.Sprintf("%s-%x.pdf", sanitizePresentationArtifactName(inputPath), sum[:6]),
	))
	absOutput, _, err := resolveCanvasFilePath(rootAbs, relOutput)
	if err != nil {
		return "", "", err
	}
	return relOutput, absOutput, nil
}

func renderPresentationToPDF(ctx context.Context, inputPath, outputPath string) error {
	inputAbs, err := filepath.Abs(strings.TrimSpace(inputPath))
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(strings.TrimSpace(outputPath))
	if err != nil {
		return err
	}
	if strings.TrimSpace(inputAbs) == "" {
		return errors.New("presentation input path is required")
	}
	if strings.TrimSpace(outputAbs) == "" {
		return errors.New("presentation output path is required")
	}
	if !isPresentationFilePath(inputAbs) {
		return fmt.Errorf("unsupported presentation file: %s", filepath.Base(inputAbs))
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(filepath.Dir(outputAbs), "presentation-render-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	cmd := exec.CommandContext(ctx, "libreoffice", "--headless", "--convert-to", "pdf", "--outdir", tempDir, inputAbs)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("libreoffice is not installed")
		}
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("libreoffice conversion failed: %s", message)
	}

	convertedPath, err := firstPDFInDir(tempDir)
	if err != nil {
		return err
	}
	return moveConvertedPresentationPDF(convertedPath, outputAbs)
}

func firstPDFInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if isPDFFilePath(name) {
			return filepath.Join(dir, name), nil
		}
	}
	return "", errors.New("libreoffice did not produce a PDF")
}

func moveConvertedPresentationPDF(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

func (a *App) renderPresentationArtifact(projectRoot, inputPath string) (string, error) {
	relOutput, absOutput, err := presentationArtifactOutputPath(projectRoot, inputPath)
	if err != nil {
		return "", err
	}
	renderer := a.presentationRenderer
	if renderer == nil {
		renderer = renderPresentationToPDF
	}
	ctx, cancel := context.WithTimeout(context.Background(), presentationRenderTimeout)
	defer cancel()
	if err := renderer(ctx, inputPath, absOutput); err != nil {
		return "", err
	}
	return relOutput, nil
}
