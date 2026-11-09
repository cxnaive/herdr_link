// Validate the system PDF renderer. On Linux, run inside Xvfb: WebKit needs a realized window.
package main

import (
	"bytes"
	"fmt"
	"github.com/cxnaive/herdr-desktop/internal/pdfview"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func sample() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`}
	stream := "BT /F1 28 Tf 72 700 Td (Herdr PDF Preview) Tj 0 -50 Td /F1 16 Tf (Native embedded document reader) Tj ET\n"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}
func main() {
	out := "build/pdf-preview-native.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := pdfview.Register(); err != nil {
		log.Fatal(err)
	}
	var once sync.Once
	var captured atomic.Bool
	var viewer *pdfview.Viewer
	mygo.App.SetName("Herdr PDF Check")
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "PDF check", Hidden: runtime.GOOS != "linux", SkipTaskbar: true, Width: 900, Height: 720, Content: ui.View(func(c *ui.Context) {
			if viewer != nil {
				ui.WebView(c, viewer.Web).Fill()
			}
		})})
		var err error
		viewer, err = pdfview.New(win, sample(), func(loadErr error) {
			if loadErr != nil {
				log.Println(loadErr)
				mygo.App.Quit()
				return
			}
			once.Do(func() {
				go func() {
					time.Sleep(3 * time.Second)
					png, err := viewer.Web.CapturePage()
					if err == nil {
						err = os.WriteFile(out, png, 0600)
					}
					if err != nil {
						log.Println(err)
					} else {
						captured.Store(true)
						log.Println("PDF screenshot:", out)
					}
					mygo.App.Quit()
				}()
			})
		})
		if err != nil {
			log.Println(err)
			mygo.App.Quit()
			return
		}
		win.Invalidate()
		go func() { time.Sleep(25 * time.Second); mygo.App.Quit() }()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	if !captured.Load() {
		log.Fatal("PDF preview did not produce a screenshot")
	}
}
