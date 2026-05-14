//go:build pdf

package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// TryRenderHTMLFileToPDF prints a local HTML file to PDF using headless Chrome via chromedp.
func TryRenderHTMLFileToPDF(htmlPath, pdfPath string) error {
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("pdf: html path: %w", err)
	}
	fileURL := "file://" + filepath.ToSlash(absHTML)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 45*time.Second)
	defer cancelTimeout()

	var pdfBuf []byte
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fileURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuf, _, err = page.PrintToPDF().WithPrintBackground(true).Do(ctx)
			return err
		}),
	); err != nil {
		return fmt.Errorf("chromedp: %w", err)
	}
	if err := os.WriteFile(pdfPath, pdfBuf, 0o644); err != nil {
		return err
	}
	return nil
}
