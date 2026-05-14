//go:build !pdf

package pdf

// TryRenderHTMLFileToPDF is implemented when built with -tags pdf (chromedp).
func TryRenderHTMLFileToPDF(_, _ string) error {
	return ErrBuiltWithoutPDFTag
}
