package pdf

import "errors"

// ErrBuiltWithoutPDFTag is returned by TryRenderHTMLFileToPDF when the binary
// was built without chromedp (-tags pdf).
var ErrBuiltWithoutPDFTag = errors.New("embedded PDF renderer disabled (rebuild with -tags pdf for chromedp)")
